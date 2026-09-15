package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// Headers the handlers bind their storage credentials from.
const (
	accessKeyHeader    = "access_key"
	secretKeyHeader    = "secret_key"
	sessionTokenHeader = "session_token"
)

// storageCredentialHeaders is every header a handler may take a storage
// credential from. Listed once so stripping and injecting cannot drift apart.
var storageCredentialHeaders = []string{accessKeyHeader, secretKeyHeader, sessionTokenHeader}

// sessionCredentialTTL is how long an object credential is requested for. The
// control endpoint may clamp it lower.
const sessionCredentialTTL = time.Hour

// credentialRefreshLeeway renews a credential slightly before it lapses, so a
// long upload started near the boundary is not signed with a key that expires
// mid-transfer.
const credentialRefreshLeeway = 2 * time.Minute

// credentialCache holds short-lived object credentials per (user, region,
// bucket, access level).
//
// The access level is part of the key because a listing and an upload of the
// same bucket ask for different credentials: the listing gets one that cannot
// write. Drop it from the key and the listing's read-only credential is handed
// to the next upload, which the gateway then refuses.
//
// Without the cache every page view would mint a fresh credential, and on the
// storage side each mint is a real credential that has to be tracked and later
// reaped.
type credentialCache struct {
	mu    sync.Mutex
	items map[string]*control.Credentials
}

// credentialSweepInterval is how often expired entries are dropped.
//
// Without a sweep an entry is only ever evicted when the SAME key is read again,
// so a panel with many users and buckets keeps a live-until-expiry access key
// and secret for every combination it has ever served, for the process
// lifetime. Users leave, buckets are handed back, and those keys stay resident.
const credentialSweepInterval = 5 * time.Minute

// newCredentialCache builds the cache and starts the sweeper that bounds it. The
// sweeper stops with ctx, which is the server's own cancellation context.
func newCredentialCache(ctx context.Context) *credentialCache {
	c := &credentialCache{items: map[string]*control.Credentials{}}
	go c.sweepUntil(ctx, credentialSweepInterval)
	return c
}

func (c *credentialCache) sweepUntil(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sweep()
		}
	}
}

// sweep drops every entry that can no longer be served. It uses the same
// leeway as get, so an entry sweep keeps is one get would still hand out.
func (c *credentialCache) sweep() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, cred := range c.items {
		if !cred.Expiration.IsZero() && now.After(cred.Expiration.Add(-credentialRefreshLeeway)) {
			delete(c.items, key)
		}
	}
}

func (c *credentialCache) get(key string) (*control.Credentials, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cred, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if !cred.Expiration.IsZero() && time.Now().After(cred.Expiration.Add(-credentialRefreshLeeway)) {
		delete(c.items, key)
		return nil, false
	}
	return cred, true
}

func (c *credentialCache) put(key string, cred *control.Credentials) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = cred
}

// injectObjectCredentials supplies the storage credentials for AuthModeIAM.
//
// Every handler binds access_key/secret_key from request headers. In s3 mode the
// browser sends the user's own keys there. In iam mode the user has none, so
// this middleware fetches a short-lived credential for the signed-in user and
// writes it into those same headers before the handler binds them.
//
// Doing it here rather than in each handler is deliberate: the storage layer and
// all twelve handlers stay untouched, and there is exactly one place where a
// request acquires the authority to reach the gateway.
//
// It is a no-op in s3 mode.
func (s *Server) injectObjectCredentials() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !s.Config.Server.IsIAMMode() {
				return next(c)
			}

			session, ok := auth.SessionFrom(c)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}

			region := s.requestRegion(c)

			// Which bucket this request is for, from the authorization middleware
			// that already resolved it.
			//
			// A credential is a subuser under ONE storage account, and it reaches
			// what that account OWNS. Ownership is per account, not per tenant: one
			// tenant can hold several team accounts owning different buckets, so a
			// per-tenant credential reaches only whichever of them was picked and
			// answers "access denied" for the rest. Naming the bucket lets the
			// control endpoint mint under its actual owner.
			//
			// Keyed per bucket; deduplication is the control endpoint's job — it
			// hands back the same credential for two buckets owned by the same
			// account, so this does not mean one credential per bucket.
			b, resolved := resolvedBucket(c)
			if !resolved {
				// No bucket means the permission gate found nothing to resolve, and
				// a credential is minted against an OWNING ACCOUNT the control
				// endpoint picks from the bucket. Asking without one either fails
				// (the endpoint cannot choose an account) or succeeds too broadly,
				// and either way burns an STS round trip on a route that never
				// reaches the gateway. Leave the headers empty and let the handler
				// refuse for want of a credential.
				return next(c)
			}
			tenant, bucket := b.Tenant, b.Name

			// The level this ROUTE needs, not the level this user could reach. A
			// listing is signed with a credential that cannot write, even for
			// someone who holds write on the bucket, so the permission gate above
			// is no longer the only thing standing between a bug and a mutation.
			// The control endpoint clamps it to the grants; it can only narrow.
			access := requiredAccess(c)

			// Keyed per (bucket, level). The level MUST be in the key: a listing
			// caches a read-only credential, and without it the next upload to the
			// same bucket would reuse that credential and be refused by the
			// gateway.
			key := session.Subject + "|" + region + "|" + bucket + "|" + access

			cred, hit := s.credentials.get(key)
			if !hit {
				var err error
				cred, err = s.control.SessionCredentials(
					c.Request().Context(), session.AccessToken, region, tenant, bucket, access, sessionCredentialTTL)
				if err != nil {
					s.logger.Error("could not mint storage credentials: " + err.Error())
					return controlError(err, "could not obtain storage credentials")
				}
				s.credentials.put(key, cred)
			}

			// Overwrite rather than default: a client must not be able to smuggle
			// its own keys past the authorization gate by setting these itself.
			c.Request().Header.Set(accessKeyHeader, cred.AccessKeyID)
			c.Request().Header.Set(secretKeyHeader, cred.SecretAccessKey)
			// A real STS credential is refused without its session token: the SDK
			// only sends X-Amz-Security-Token when this is set. Set unconditionally
			// so a stale token from a previous credential cannot survive here.
			c.Request().Header.Set(sessionTokenHeader, cred.SessionToken)

			// What a handler must not outlive. A share link presigned with this
			// credential stops working the moment the credential lapses, so the
			// share handler clamps its expiry to what is left here.
			c.Set(credentialExpiryKey, cred.Expiration)

			return next(c)
		}
	}
}

// credentialExpiryKey is where injectObjectCredentials leaves the moment the
// injected credential stops working.
const credentialExpiryKey = "iam.credential_expiry"

// credentialExpiry reports when the injected credential lapses, if one was
// injected and the endpoint named an expiry.
func credentialExpiry(c echo.Context) (time.Time, bool) {
	at, ok := c.Get(credentialExpiryKey).(time.Time)
	return at, ok && !at.IsZero()
}

// stripClientCredentials clears the storage-credential headers off every
// incoming request in AuthModeIAM.
//
// In that mode the caller holds no storage keys — the panel mints them after it
// has authorized the request — so any such header on the way in is a client
// trying to reach the gateway with keys of its own. injectObjectCredentials
// overwrites them on the routes it runs on, but it runs per route, and a route
// added later without it would otherwise sign gateway calls with whatever the
// caller put in the headers. Stripping at the group makes that fail closed: the
// new route gets no credential at all rather than the caller's.
//
// It is a no-op in s3 mode, where these headers are exactly how a user supplies
// the credentials the panel is meant to use.
func (s *Server) stripClientCredentials() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !s.Config.Server.IsIAMMode() {
				return next(c)
			}
			for _, h := range storageCredentialHeaders {
				c.Request().Header.Del(h)
			}
			return next(c)
		}
	}
}

// requestRegion is the region a request targets: the explicit header if the
// caller set one, else this instance's own.
func (s *Server) requestRegion(c echo.Context) string {
	if r := c.Request().Header.Get("region"); r != "" {
		return r
	}
	return s.Config.Server.Region
}

// sessionToken returns the caller's OIDC access token, which is what the control
// endpoint authorizes against. The panel deliberately holds no credential of its
// own: the endpoint answers for the person at the keyboard.
func sessionToken(c echo.Context) string {
	session, ok := auth.SessionFrom(c)
	if !ok {
		return ""
	}
	return session.AccessToken
}

// controlError converts a control-endpoint failure into an HTTP error, keeping
// an authorization failure recognisable instead of flattening it to a 500.
func controlError(err error, fallback string) error {
	if apiErr, ok := err.(*control.Error); ok {
		switch apiErr.Status {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
			http.StatusBadRequest, http.StatusConflict:
			return echo.NewHTTPError(apiErr.Status, apiErr.Message)
		}
	}
	return echo.NewHTTPError(http.StatusBadGateway, fallback)
}

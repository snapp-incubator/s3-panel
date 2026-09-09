package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/control"
)

// Headers the handlers bind their storage credentials from.
const (
	accessKeyHeader = "access_key"
	secretKeyHeader = "secret_key"
)

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

func newCredentialCache() *credentialCache {
	return &credentialCache{items: map[string]*control.Credentials{}}
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
			tenant, bucket := "", ""
			if b, ok := resolvedBucket(c); ok {
				tenant, bucket = b.Tenant, b.Name
			}

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

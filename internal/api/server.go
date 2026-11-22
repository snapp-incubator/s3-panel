package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	echoSwagger "github.com/swaggo/echo-swagger"
	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/cache"
	"github.com/snapp-incubator/S3-Panel/internal/config"
	"github.com/snapp-incubator/S3-Panel/internal/control"
	"github.com/snapp-incubator/S3-Panel/internal/health"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
	"github.com/snapp-incubator/S3-Panel/internal/storage/ceph"
)

type Server struct {
	Config        config.Config
	cancelCtx     context.Context
	cancelFunc    context.CancelFunc
	store         storage.ObjectStorage
	cache         cache.ServerCache
	logger        *zap.Logger
	Router        *echo.Echo
	regionTargets map[string]*url.URL

	// Set only in AuthModeIAM. auth identifies the user, control answers what
	// they may reach, and credentials caches the short-lived keys object calls
	// are signed with.
	auth        *auth.Authenticator
	control     *control.Client
	credentials *credentialCache

	// readiness answers readyz from this mode's dependencies.
	readiness *health.Readiness

	// view decides who may use the panel when it is part of a unified console
	// (panel.enabled); nil otherwise.
	view *viewGate
}

func NewServer(ctx context.Context, cancelFunc context.CancelFunc, cfg config.Config, logger *zap.Logger) (*Server, error) {
	s := &Server{
		Config:     cfg,
		cancelCtx:  ctx,
		cancelFunc: cancelFunc,
		logger:     logger,
	}

	if err := cfg.Server.ValidateBasePath(); err != nil {
		return nil, err
	}

	if err := s.buildRegionTargets(); err != nil {
		return nil, err
	}

	if err := s.registerIAMMode(ctx); err != nil {
		return nil, err
	}

	if err := s.registerPanel(ctx); err != nil {
		return nil, err
	}

	s.registerCephRepository()

	s.registerCache()
	err := s.initializeCache()
	if err != nil {
		return nil, err
	}

	s.registerRouter()
	s.registerRoutes()
	s.registerPruner()

	return s, nil
}

// registerIAMMode wires the OIDC login and the control-endpoint client.
//
// Failures here are fatal rather than degrading: in AuthModeIAM these are the
// only things that identify a user and decide what they may reach, so a panel
// that started without them would serve every request unauthenticated.
func (s *Server) registerIAMMode(ctx context.Context) error {
	if !s.Config.Server.IsIAMMode() {
		s.logger.Info("### Auth mode: s3 (users sign in with their own S3 credentials) ###")
		return nil
	}
	s.logger.Info("### Auth mode: iam (users sign in with OIDC) ###")

	authenticator, err := auth.New(ctx, s.Config.OIDC)
	if err != nil {
		return fmt.Errorf("iam auth mode: %w", err)
	}
	s.auth = authenticator
	s.auth.BasePath = s.Config.Server.Base()

	controlClient, err := control.New(s.Config.ObjectStorage.ControlURL, s.Config.ObjectStorage.STSURL, 0)
	if err != nil {
		return fmt.Errorf("iam auth mode: %w", err)
	}
	s.control = controlClient.WithClientKey(s.Config.ObjectStorage.STSClientKey)
	s.control = s.control.WithAPIURL(s.Config.ObjectStorage.IAMAPIURL)

	// Signing out revokes the storage credentials this console obtained on the
	// person's behalf. Without it, logout leaves a real credential live for
	// hours, usable from anywhere, that the browser no longer shows anyone.
	//
	// "console" is passed explicitly: a credential the same person took to a
	// shell keeps its own lifetime, because closing a browser tab should not
	// break a terminal somewhere else.
	if s.auth != nil {
		// Admin is IAM's call: unified-panel tokens carry no groups claim, and
		// IAM falls back to the groups it synced into its own database.
		s.auth.Logger = s.logger
		s.auth.ResolveAdmin = s.control.RealIsAdmin
		s.auth.OnLogout = s.revokeOnLogout
		// Part of a console, the SPA signs in silently; an explicit sign-out
		// must not hold the next one back (see auth.Authenticator.MarkSignOut).
		s.auth.MarkSignOut = s.Config.Panel.Enabled
		// Sibling hosts of a shared parent domain can toss a session cookie
		// at the panel's path; binding it to a host-only companion stops that.
		s.auth.BindSession = s.Config.OIDC.BindsSession(s.Config.Panel.Enabled)
		// A copied cookie must not keep refreshing after the person signed out.
		s.auth.RevokeRefreshToken = s.Config.OIDC.RevokesRefreshToken(s.Config.Panel.Enabled)
	}
	s.credentials = newCredentialCache(ctx)

	return nil
}

// logoutRevokeTimeout bounds the revocations a sign-out starts. They run after
// the sign-out has answered, so it only stops them running forever.
const logoutRevokeTimeout = time.Minute

// revokeOnLogout revokes the storage credentials this console obtained for the
// person. The cached ones are forgotten at once; the revocations run in the
// background, detached from the request, so a slow or unreachable region can
// neither hold the sign-out nor be cut short when the browser moves on.
//
// Best effort: a failed revoke must not stop the person signing out, and the
// credential still expires on its own.
func (s *Server) revokeOnLogout(ctx context.Context, session auth.Session) {
	// The prefix also covers this session's acting entries.
	cached := s.credentials.takeIdentity(session.Subject)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logoutRevokeTimeout)
		defer cancel()
		s.revokeCredentials(ctx, session, cached)
	}()
}

// revokeCredentials revokes, in parallel:
//
//   - the person's own console credentials in every region they hold a grant
//     in (no region named: IAM's POST /sts/logout walks them);
//   - the same, by name, in this instance's region and in every region the
//     person was minted a credential for here. Named, IAM revokes even where
//     the person's grant is already gone, which the walk above cannot see;
//   - each credential minted here while acting as a debug user, by its session
//     ID, as that debug user. Never the debug user's credentials wholesale: IAM
//     revokes per principal, and a shared debug user's other credentials belong
//     to other admins.
func (s *Server) revokeCredentials(ctx context.Context, session auth.Session, cached map[string]*control.Credentials) {
	own := control.WithActAs(ctx, "")
	regions := map[string]bool{}
	if s.Config.Server.Region != "" {
		regions[s.Config.Server.Region] = true
	}
	type acted struct{ region, id string }
	var actedSessions []acted
	for key, cred := range cached {
		region, acting := cachedRegion(key, session.Subject)
		switch {
		case acting && cred.SessionID != "" && region != "" && session.ActAs != "":
			actedSessions = append(actedSessions, acted{region, cred.SessionID})
		case !acting && region != "":
			regions[region] = true
		}
	}

	var wg sync.WaitGroup
	revoke := func(what string, call func() error) {
		wg.Go(func() {
			if err := call(); err != nil {
				s.logger.Warn("could not revoke storage credentials on sign-out",
					zap.String("subject", session.Subject), zap.String("what", what), zap.Error(err))
			}
		})
	}
	revoke("every granted region", func() error {
		return s.control.RevokeSessions(own, session.AccessToken, "", "console")
	})
	for region := range regions {
		revoke("region "+region, func() error {
			return s.control.RevokeSessions(own, session.AccessToken, region, "console")
		})
	}
	asDebugUser := control.WithActAs(ctx, session.ActAs)
	for _, a := range actedSessions {
		revoke("acting session in "+a.region, func() error {
			return s.control.RevokeSession(asDebugUser, session.AccessToken, a.region, a.id)
		})
	}
	wg.Wait()
}

// cachedRegion reads the region out of a credential cache key
// ("<subject>[|as:<email>]|<region>|<bucket>|<access>") and whether the entry
// was minted while acting.
func cachedRegion(key, subject string) (region string, acting bool) {
	rest := strings.TrimPrefix(key, subject+"|")
	if strings.HasPrefix(rest, "as:") {
		acting = true
		_, rest, _ = strings.Cut(rest, "|")
	}
	region, _, _ = strings.Cut(rest, "|")
	return region, acting
}

func (s *Server) registerCephRepository() {
	s.logger.Info("### Registering Ceph Repository ###")
	s.store = ceph.NewCephObjectStorage()
	s.logger.Info("### Ceph Repository Registered ###")
}

func (s *Server) registerCache() {
	s.logger.Info("### Registering Cache ###")
	cacheStore := sync.Map{}
	s.cache = cache.NewInMemoryCache(&cacheStore)
	s.logger.Info("### Cache Registered ###")
}

func (s *Server) initializeCache() error {
	// The cache maps an S3 access key to a RADOS uid, which only the s3 auth mode
	// needs: in iam mode the user is identified by their OIDC session and the
	// object credentials are minted per request.
	if s.Config.Server.IsIAMMode() {
		s.logger.Info("### Cache initialization skipped (iam auth mode) ###")
		return nil
	}
	s.logger.Info("### Initializing Cache ###")
	radosClient, err := ceph.NewRadosClient(s.Config.ObjectStorage.URL, s.Config.ObjectStorage.AccessKeyAdmin, s.Config.ObjectStorage.SecretKeyAdmin)
	if err != nil {
		return err
	}
	_, _, err = findUserID(s, radosClient, "X")
	defer s.logger.Info("### Cache Initialized ###")
	return err
}

func (s *Server) registerRouter() {
	newRouter := echo.New()
	newRouter.Validator = &CustomValidator{validator: NewRawValidator()}
	// First, so every answer carries them, errors and the SPA included.
	newRouter.Use(securityHeaders())
	// Before anything that can change state, the login endpoints included.
	newRouter.Use(s.refuseCrossOrigin())
	if s.Config.Server.ServeFrontend {
		var backend []string
		if s.Config.Server.IsIAMMode() {
			// The console-link resolver, registered with the login routes.
			backend = append(backend, "/iam/buckets")
		}
		newRouter.Use(frontendMiddleware(s.Config.Server.Base(), s.renderPage, backend...))
		s.logger.Info("### Serving embedded frontend ###")
	} else {
		s.logger.Info("### Frontend serving disabled (API-only) ###")
	}
	s.Router = newRouter
}

func (s *Server) registerRoutes() {
	// Everything the panel serves lives under its base path ("" at the host's
	// root), so requests reach it with the prefix still on.
	base := s.Router.Group(s.Config.Server.Base())

	// livez and readyz are what a status page probes, through the public route
	// and so under the base path. They, and the older /health, also stay at the
	// root, where an orchestrator's probe finds them whatever prefix the panel
	// is published under.
	if s.readiness == nil {
		s.registerReadiness()
	}
	for _, g := range s.healthRouters(base) {
		g.GET("/health", health.HandleHealth)
		g.GET("/livez", health.HandleLive)
		g.GET("/readyz", s.readiness.Handle)
	}
	// Swagger UI, standalone only. In a unified console the panel shares its
	// origin with other apps, and a page anyone can load without signing in,
	// running a third-party bundle outside the Trusted Types policy, has no
	// place there. Unregistered, /docs answers 404: it stays a backend prefix
	// the SPA leaves alone (spa.go).
	if !s.Config.Panel.Enabled {
		base.GET("/docs/*", stampDocsNonce(echoSwagger.WrapHandler))
	}

	// How the SPA learns which login flow to present.
	base.GET("/api/config", s.HandleConfig())
	base.GET("/s3/api/config", s.HandleConfig())

	// Login endpoints live OUTSIDE the API group: that group carries the static
	// bearer-token middleware, which would reject the identity provider's
	// callback (it arrives with no Authorization header and cannot be given one).
	if s.auth != nil {
		s.auth.RegisterRoutes(base)
		// Where links from a unified console land: the control endpoint's name
		// for a bucket, redirected to this panel's page for it.
		base.GET("/iam/buckets/*", s.handleResolveIAMBucket())
	}

	// Serve the API under both /api and /s3/api. The bundled frontend calls
	// /s3/api (a convention inherited from the central panel router); direct API
	// clients and the swagger docs use /api.
	s.registerAPIGroup("/api")
	s.registerAPIGroup("/s3/api")
}

// healthRouters is where the health endpoints are mounted: the root, and the
// base path when there is one.
func (s *Server) healthRouters(base *echo.Group) []auth.Router {
	if s.Config.Server.Base() == "" {
		return []auth.Router{s.Router}
	}
	return []auth.Router{s.Router, base}
}

// registerReadiness picks what readyz reports on for the active mode.
//
// In iam mode IAM is critical: it authorizes every request, so without it the
// panel serves no one. Sign-in and the gateway only degrade it — signed-in
// people keep browsing while Keycloak is away, and bucket lists still come
// from IAM while the gateway is. In s3 mode the gateway is all there is.
func (s *Server) registerReadiness() {
	var checks []health.Check
	add := func(name string, critical bool, probe func(context.Context) error) {
		checks = append(checks, health.Check{Name: name, Critical: critical, Probe: probe})
	}
	gateway := strings.TrimSpace(s.Config.ObjectStorage.URL)
	if s.Config.Server.IsIAMMode() {
		// With the client key, as every other call to IAM: once IAM requires it
		// on /s3, a probe without it is logged there as a refusal every 15 s.
		add("iam", true, health.ReachableProbeWithHeader(
			strings.TrimSpace(s.Config.ObjectStorage.ControlURL),
			control.PanelHeader(s.Config.ObjectStorage.STSClientKey)))
		issuer := strings.TrimRight(strings.TrimSpace(s.Config.OIDC.IssuerURL), "/")
		add("oidc", false, health.OKProbe(issuer+"/.well-known/openid-configuration"))
		if gateway != "" {
			add("gateway", false, health.ReachableProbe(gateway))
		}
	} else if gateway != "" {
		add("gateway", true, health.ReachableProbe(gateway))
	}
	s.readiness = health.NewReadiness(checks, health.DefaultReadyTTL, 0)
	s.readiness.OnFailure = func(name string, err error) {
		if s.logger != nil {
			s.logger.Warn("readiness check failed", zap.String("check", name), zap.Error(err))
		}
	}
}

func (s *Server) registerAPIGroup(prefix string) {
	// The two auth modes gate the API differently: a static bearer token proves
	// the caller is the panel, an OIDC session proves which person is asking.
	guard := s.AuthMiddleware()
	if s.auth != nil {
		guard = s.auth.RequireSession()
	}

	// In iam mode the caller holds no storage keys of their own, so any
	// access_key/secret_key/session_token header on the way in is a client trying
	// to reach the gateway past the authorization gate. Stripped for the whole
	// API group, which is what makes a route added later fail CLOSED: it gets no
	// credential rather than the caller's. A no-op in s3 mode, where those
	// headers are how the user supplies the credentials the panel must use.
	//
	// requireJSONBody keeps HTML forms off every JSON route; only uploads take a
	// multipart body.
	//
	// requireView, right after the session guard, refuses people the console
	// says may not view this app (panel.enabled only).
	apiRoutes := s.Router.Group(s.Config.Server.Base()+prefix, s.CORSMiddleware(), requireJSONBody("/object/upload"), guard, s.requireView(), s.actAsContext(), s.stripClientCredentials())

	// A single gate in front of everything that could change the storage
	// backend. It outranks the permission checks below on purpose: holding
	// write on a bucket does not matter if the instance itself is read-only.
	readOnly := s.refuseWhenReadOnly()

	apiRoutes.OPTIONS("/*", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	// /regions is always answered locally (it advertises this instance's config).
	apiRoutes.GET("/regions", s.HandleRegions())

	if s.Config.Server.IsIAMMode() {
		apiRoutes.GET("/act-as", s.handleActAsGet())
		apiRoutes.POST("/act-as", s.handleActAsSet())
		apiRoutes.DELETE("/act-as", s.handleActAsClear())
	}

	// Data routes are region-aware: a "region" header for a remote region is
	// reverse-proxied to that region's backend.
	region := s.regionRouter()

	// There is deliberately NO route that hands a storage credential to the
	// browser. A credential that leaves this console works from anywhere, for its
	// whole lifetime, with none of the per-request permission checks applied here
	// — so the console obtains credentials for its OWN object calls and they go
	// no further.

	// In iam mode the panel signs object calls with a credential it obtained on
	// the user's behalf, which makes it the enforcement point. Each route names
	// the permission IT needs, so the gate is per route rather than on the group;
	// what stops a route added later from signing with caller-supplied keys is
	// stripClientCredentials on the group above, not this gate.
	//
	// Order within a route's middleware list is load-bearing, and gatewayChain is
	// where it is fixed — see the comment there.
	apiRoutesBuckets := apiRoutes.Group("/bucket", region)
	{
		// list and detail are answered entirely from the control endpoint, so
		// they need no storage credential.
		apiRoutesBuckets.GET("/list", s.bucketListHandler())
		apiRoutesBuckets.POST("/create", s.bucketCreateHandler(), readOnly)

		if s.Config.Server.IsIAMMode() {
			apiRoutesBuckets.GET("/detail", s.HandleIAMBucketDetail(), s.requireBucketPermission(permRead))

			// Bare refusals, with no permission gate and no credential in front of
			// them. Both handlers reach the gateway through the RGW ADMIN API, and
			// the panel holds no admin credential in iam mode — the same reason
			// HandleObjectList was given an iam branch. Registering them with the
			// usual chain meant /bucket/delete authorized the caller and minted a
			// readwrite credential only to fail with 422 from NewRadosClient.
			apiRoutesBuckets.GET("/quota", s.bucketQuotaHandler())
			apiRoutesBuckets.DELETE("/delete", s.bucketDeleteHandler(), readOnly)
		} else {
			apiRoutesBuckets.GET("/quota", s.bucketQuotaHandler(), s.gatewayChain(permRead, reading)...)
			apiRoutesBuckets.DELETE("/delete", s.bucketDeleteHandler(), s.gatewayChain(permOwner, mutating)...)
		}
	}

	// Everything under /object reaches the gateway, so every route here takes the
	// full chain. gatewayChain is what fixes its order; see the comment there.
	apiRoutesObjects := apiRoutes.Group("/object", region)
	{
		apiRoutesObjects.GET("/list", s.HandleObjectList(), s.gatewayChain(permRead, reading)...)
		apiRoutesObjects.POST("/upload", s.HandleObjectUpload(), s.gatewayChain(permWrite, mutating)...)
		apiRoutesObjects.GET("/download", s.HandleObjectDownload(), s.gatewayChain(permRead, reading)...)
		apiRoutesObjects.GET("/head", s.HandleObjectHead(), s.gatewayChain(permRead, reading)...)
		apiRoutesObjects.DELETE("/delete", s.HandleObjectsDelete(), s.gatewayChain(permWrite, mutating)...)
		// Counted as mutating even though it only reads: a share link hands out
		// access that outlives the request, which is not what "browsing and
		// downloads stay intact" covers — and the config comment on ReadOnly and
		// the README both already say read-only refuses share links.
		//
		// POST, not GET: minting a link is an action, and a GET can be triggered
		// by any page that embeds an image tag pointing here.
		apiRoutesObjects.POST("/share", s.HandleObjectShare(), s.gatewayChain(permRead, mutating)...)
		// How long a share link may live, so the dialog offers only durations
		// that will hold. Only reads, so not refused when read-only. The same
		// permission as /share, hence the same access level and the same cached
		// credential: what remains of it is the cap /share applies.
		apiRoutesObjects.GET("/share/limit", s.HandleObjectShareLimit(), s.gatewayChain(permRead, reading)...)
	}

	apiRoutesUsers := apiRoutes.Group("/user", region)
	{
		// In iam mode /quota is refused and /id answers from the session, so
		// neither needs a storage credential — and neither carries one. Attaching
		// withCredentials here would ask the control endpoint to mint without a
		// bucket on every poll of an endpoint that cannot use the result.
		apiRoutesUsers.GET("/quota", s.userQuotaHandler())
		apiRoutesUsers.GET("/id", s.userIdentificationHandler())
	}
}

// Whether a route changes the storage backend, for gatewayChain's second
// argument. Named rather than a bare bool so the call sites say which they are.
const (
	mutating = true
	reading  = false
)

// gatewayChain is the middleware a route takes when it signs a call to the
// gateway, in the order echo applies it (left to right).
//
// The order is the whole point of having this in one place:
//
//  1. refuseWhenReadOnly, FIRST, on a route that changes anything. Listed after
//     the credential it used to mint a real readwrite credential at the storage
//     backend and only then return 403 — so a loop of deletes against a
//     read-only panel produced an unbounded stream of write-capable credentials
//     for the operator to reap.
//  2. requireBucketPermission, which authorizes the caller and resolves the
//     bucket.
//  3. injectObjectCredentials, which needs the bucket step 2 resolved: a
//     credential is minted against the account that OWNS the bucket. This is why
//     the chain lives on the routes and not on the group — group middleware runs
//     before route middleware, so hoisting it would mint before the bucket was
//     known, i.e. one credential per session again.
//
// In s3 mode every element is a no-op: the gateway is the authority there,
// because each request is signed with the user's own credentials.
func (s *Server) gatewayChain(permission string, changesTheBackend bool) []echo.MiddlewareFunc {
	chain := make([]echo.MiddlewareFunc, 0, 3)
	if changesTheBackend {
		chain = append(chain, s.refuseWhenReadOnly())
	}
	return append(chain, s.requireBucketPermission(permission), s.injectObjectCredentials())
}

// userIdentificationHandler answers "who am I" from whichever identity the
// active mode actually has: a RADOS user in s3 mode, the OIDC session in iam
// mode. The minted credential in iam mode belongs to a short-lived subuser, so
// resolving it back to a gateway user would name the session, not the person.
func (s *Server) userIdentificationHandler() echo.HandlerFunc {
	if s.Config.Server.IsIAMMode() {
		return s.HandleIAMUserIdentification()
	}
	return s.HandleUserIdentification()
}

// userQuotaHandler reports per-user storage quota, which only exists in s3 mode.
// In iam mode a user has no single gateway account to carry a quota — their
// access is a set of grants across buckets that may live in different tenants
// and regions — so the panel reports per-bucket quota on the detail page
// instead.
func (s *Server) userQuotaHandler() echo.HandlerFunc {
	if s.Config.Server.IsIAMMode() {
		return func(c echo.Context) error {
			return echo.NewHTTPError(http.StatusNotImplemented,
				"per-user quota does not apply in iam auth mode; see per-bucket quota on /bucket/detail")
		}
	}
	return s.HandleUserQuota()
}

// bucketQuotaHandler reports per-bucket quota across the caller's buckets, which
// only exists in s3 mode.
//
// The s3 handler resolves the caller's RADOS uid through the admin API with the
// panel's admin keys, and in iam mode the panel has none — it holds only a
// short-lived credential minted for one owning account. The control endpoint
// already reports a bucket's usage and quota, which /bucket/detail serves.
func (s *Server) bucketQuotaHandler() echo.HandlerFunc {
	if s.Config.Server.IsIAMMode() {
		return func(c echo.Context) error {
			return echo.NewHTTPError(http.StatusNotImplemented,
				"per-user bucket quota does not apply in iam auth mode; see per-bucket quota on /bucket/detail")
		}
	}
	return s.HandleBucketQuota()
}

// bucketDeleteHandler disables bucket deletion in iam mode.
//
// Deleting a bucket goes through the RGW admin API — the emptiness check and the
// delete both do — and the panel holds no admin credential in iam mode. Removing
// a bucket is also provisioning, which belongs with whatever created it, for the
// same reason bucketCreateHandler gives.
func (s *Server) bucketDeleteHandler() echo.HandlerFunc {
	if s.Config.Server.IsIAMMode() {
		return func(c echo.Context) error {
			return echo.NewHTTPError(http.StatusForbidden,
				"bucket deletion is not available in iam auth mode; remove buckets through your storage operator")
		}
	}
	return s.HandleBucketDelete()
}

// bucketCreateHandler disables bucket creation in iam mode.
//
// A minted credential belongs to no tenant, so a bucket created with it would
// land somewhere other than the team's namespace. Provisioning belongs to
// whatever creates the tenants in the first place, not to this panel.
func (s *Server) bucketCreateHandler() echo.HandlerFunc {
	if s.Config.Server.IsIAMMode() {
		return func(c echo.Context) error {
			return echo.NewHTTPError(http.StatusForbidden,
				"bucket creation is not available in iam auth mode; provision buckets through your storage operator")
		}
	}
	return s.HandleBucketCreate
}

// bucketListHandler picks the listing for the active auth mode. They differ in
// kind, not just in shape: the s3 one lists what the caller's own credentials
// own, the iam one lists what an authorization service says they may reach.
func (s *Server) bucketListHandler() echo.HandlerFunc {
	if s.Config.Server.IsIAMMode() {
		return s.HandleIAMBucketList()
	}
	return s.HandleBucketList()
}

func (s *Server) registerPruner() {
	prunerInterval := 1 * time.Hour
	go func() {
		ticker := time.NewTicker(prunerInterval)
		for {
			select {
			case <-s.cancelCtx.Done():
				s.logger.Warn("Shutting Down Ticker due to canceling context")
				ticker.Stop()
				return
			case <-ticker.C:
				s.logger.Info("Triggered Pruner")
				errPrune := pruneDownloadDir(s.Config.Server.DownloadPath, prunerInterval)
				if errPrune != nil {
					s.logger.Error(errPrune.Error())
				}
			}
		}
	}()
}

func (s *Server) Start() error {
	return s.Router.Start(fmt.Sprintf("%s:%s", s.Config.Server.Address, s.Config.Server.Port))
}

func (s *Server) ShutDown() error {
	err := s.Router.Shutdown(s.cancelCtx)
	if err != nil {
		return err
	}
	return nil
}

func StartServer(ctx context.Context, cancelFunc context.CancelFunc, cfg config.Config, logger *zap.Logger) error {
	server, err := NewServer(ctx, cancelFunc, cfg, logger)
	if err != nil {
		return err
	}
	go func() {
		errServerStart := server.Start()
		if errServerStart != nil {
			logger.Error(fmt.Sprintf("Error Starting Router %s", errServerStart))
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
		logger.Info("Done Context. Shutting down services")
	case sig := <-sigChan:
		logger.Info(fmt.Sprintf("Received %s signal, gracefully shutting down services", sig.String()))
	}

	// call cancel function of the Server
	server.cancelFunc()
	err = server.ShutDown()
	if err != nil {
		logger.Error("Failed to shutdown server:", zap.Error(err))
	}

	logger.Info("Goodbye...")
	os.Exit(0)

	return nil
}

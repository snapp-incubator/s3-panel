package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/snapp-incubator/S3-Panel/internal/auth"
	"github.com/snapp-incubator/S3-Panel/internal/control"
	"github.com/snapp-incubator/S3-Panel/internal/storage"
)

// IAMBucketItem is one row of the bucket list in AuthModeIAM.
//
// It carries more than the s3-mode list (which is a bare []string) because the
// whole point of that mode is that a bucket is no longer implicitly the
// caller's: what they may do with it, which region it lives in, and how the
// gateway addresses it all have to travel with the name.
type IAMBucketItem struct {
	// Bucket is the identifier every other panel call takes.
	Bucket string `json:"bucket"`
	// DisplayName is the bare bucket name, for the UI.
	DisplayName string `json:"display_name"`
	// S3Name is how the gateway addresses this bucket on its own S3 endpoint,
	// which for a tenanted bucket is not the same string as Bucket.
	S3Name string `json:"s3_name,omitempty"`
	Tenant string `json:"tenant,omitempty"`
	Region string `json:"region,omitempty"`
	// Permissions are what the caller may do, e.g. read, write, owner.
	Permissions []string `json:"permissions"`
	// GrantedVia names the team the access came from, when it was inherited.
	GrantedVia string `json:"granted_via,omitempty"`
	CanRead    bool   `json:"can_read"`
	CanWrite   bool   `json:"can_write"`
	CanDelete  bool   `json:"can_delete"`
}

// IAMBucketListResponse is the AuthModeIAM bucket listing. It spans every region
// the control endpoint knows about, in one response.
type IAMBucketListResponse struct {
	Items        []IAMBucketItem `json:"items"`
	Regions      []string        `json:"regions"`
	TotalBuckets int             `json:"total_buckets"`
	TotalPages   int             `json:"total_pages"`
}

// HandleIAMBucketList serves GET /bucket/list in AuthModeIAM.
//
//	@Summary		List the buckets the signed-in user may access
//	@Description	Asks the control endpoint which buckets the caller can use, across every region, with the permissions they hold on each. Requires server.auth_mode = "iam".
//	@Tags			Bucket
//	@Produce		json
//	@Param			search_string	query		string						false	"filter by substring"
//	@Param			max_keys		query		int							false	"page size"
//	@Param			page			query		int							false	"page number"
//	@Success		200				{object}	IAMBucketListResponse		"Buckets the caller may access"
//	@Failure		401				{object}	storage.OperationErrWithMsg	"Not signed in"
//	@Failure		502				{object}	storage.OperationErrWithMsg	"Control endpoint unavailable"
//	@Router			/api/bucket/list [get]
func (s *Server) HandleIAMBucketList() echo.HandlerFunc {
	return func(c echo.Context) error {
		buckets, err := s.control.ListBuckets(c.Request().Context(), sessionToken(c))
		if err != nil {
			s.logger.Error("control endpoint bucket list failed: " + err.Error())
			return controlError(err, "could not list buckets")
		}

		search := strings.ToLower(strings.TrimSpace(c.QueryParam("search_string")))
		items := make([]IAMBucketItem, 0, len(buckets))
		regionSet := map[string]bool{}
		for _, b := range buckets {
			if search != "" && !strings.Contains(strings.ToLower(b.Name), search) &&
				!strings.Contains(strings.ToLower(b.DisplayName()), search) {
				continue
			}
			if b.BucketRegion != "" {
				regionSet[b.BucketRegion] = true
			}
			items = append(items, newIAMBucketItem(b))
		}

		regions := make([]string, 0, len(regionSet))
		for r := range regionSet {
			regions = append(regions, r)
		}
		sort.Strings(regions)

		total := len(items)
		page, maxKeys := paginationParams(c)
		items = paginate(items, page, maxKeys)

		return c.JSON(http.StatusOK, IAMBucketListResponse{
			Items:        items,
			Regions:      regions,
			TotalBuckets: total,
			TotalPages:   totalPages(total, maxKeys),
		})
	}
}

// IAMBucketDetailResponse backs the bucket detail page.
type IAMBucketDetailResponse struct {
	IAMBucketItem
	// NumObjects and SizeBytes come from the storage backend's admin API; S3 has
	// no verb that reports either.
	NumObjects   int64 `json:"num_objects"`
	SizeBytes    int64 `json:"size_bytes"`
	QuotaBytes   int64 `json:"quota_bytes"`
	QuotaObjects int64 `json:"quota_objects"`
	// Policy is the bucket policy document, null when the bucket has none.
	Policy any `json:"policy"`
	// PolicyPresent distinguishes "no policy" from "not allowed to read it":
	// reading a policy takes more than read access, and the page shows the rest
	// of the bucket either way.
	PolicyPresent bool `json:"policy_present"`
	PolicyDenied  bool `json:"policy_denied"`
}

// HandleIAMBucketDetail serves GET /bucket/detail in AuthModeIAM.
//
//	@Summary		Bucket detail: usage and policy
//	@Description	Object count, size, quota and bucket policy for one bucket. Requires server.auth_mode = "iam".
//	@Tags			Bucket
//	@Produce		json
//	@Param			bucket	query		string						true	"bucket identifier as returned by /bucket/list"
//	@Success		200		{object}	IAMBucketDetailResponse		"Bucket detail"
//	@Failure		401		{object}	storage.OperationErrWithMsg	"Not signed in"
//	@Failure		404		{object}	storage.OperationErrWithMsg	"No such bucket, or not accessible"
//	@Router			/api/bucket/detail [get]
func (s *Server) HandleIAMBucketDetail() echo.HandlerFunc {
	return func(c echo.Context) error {
		name := strings.TrimSpace(c.QueryParam("bucket"))
		if name == "" {
			return c.JSON(http.StatusBadRequest, storage.OperationErrWithMsg{Message: "bucket is required"})
		}

		ctx := c.Request().Context()
		token := sessionToken(c)
		region := c.Request().Header.Get("region")

		// The listing is the authorization boundary: a bucket the caller cannot
		// see is not in it, so an unknown name is reported as absent rather than
		// as forbidden. Telling the two apart would let anyone enumerate buckets.
		buckets, err := s.control.ListBuckets(ctx, token)
		if err != nil {
			s.logger.Error("control endpoint bucket list failed: " + err.Error())
			return controlError(err, "could not resolve the bucket")
		}
		var found *control.Bucket
		for i := range buckets {
			if buckets[i].Name == name || buckets[i].S3Name == name || buckets[i].RealName == name {
				found = &buckets[i]
				break
			}
		}
		if found == nil {
			return c.JSON(http.StatusNotFound, storage.OperationErrWithMsg{Message: "no such bucket"})
		}
		if region == "" {
			region = found.BucketRegion
		}

		detail := IAMBucketDetailResponse{IAMBucketItem: newIAMBucketItem(*found)}

		if stats, err := s.control.BucketStats(ctx, token, found.Name, region); err != nil {
			// Usage is best-effort: a stats outage should not blank the whole page.
			s.logger.Warn("bucket stats unavailable for " + found.Name + ": " + err.Error())
		} else {
			detail.NumObjects = stats.NumObjects
			detail.SizeBytes = stats.SizeBytes
			detail.QuotaBytes = stats.QuotaBytes
			detail.QuotaObjects = stats.QuotaObjects
		}

		policy, err := s.control.BucketPolicy(ctx, token, found.Name, region)
		switch {
		case err != nil && isForbidden(err):
			detail.PolicyDenied = true
		case err != nil:
			s.logger.Warn("bucket policy unavailable for " + found.Name + ": " + err.Error())
		case policy != nil:
			detail.Policy = policy
			detail.PolicyPresent = true
		}

		return c.JSON(http.StatusOK, detail)
	}
}

func newIAMBucketItem(b control.Bucket) IAMBucketItem {
	return IAMBucketItem{
		Bucket:      b.Name,
		DisplayName: b.DisplayName(),
		S3Name:      b.S3Name,
		Tenant:      b.Tenant,
		Region:      b.BucketRegion,
		Permissions: b.Permissions,
		GrantedVia:  b.GrantedViaTeam,
		CanRead:     b.Can(permRead),
		CanWrite:    b.Can(permWrite),
		// Deleting a bucket is not the same authority as writing objects into it.
		CanDelete: b.Can(permOwner),
	}
}

func isForbidden(err error) bool {
	apiErr, ok := err.(*control.Error)
	return ok && (apiErr.Status == http.StatusForbidden || apiErr.Code == "AccessDenied")
}

func paginationParams(c echo.Context) (page, maxKeys int) {
	page, maxKeys = 1, 20
	if v := c.QueryParam("page"); v != "" {
		if n := atoiDefault(v, 1); n > 0 {
			page = n
		}
	}
	if v := c.QueryParam("max_keys"); v != "" {
		if n := atoiDefault(v, 20); n > 0 {
			maxKeys = n
		}
	}
	return page, maxKeys
}

func paginate(items []IAMBucketItem, page, maxKeys int) []IAMBucketItem {
	start := (page - 1) * maxKeys
	if start >= len(items) {
		return []IAMBucketItem{}
	}
	end := start + maxKeys
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func totalPages(total, maxKeys int) int {
	if maxKeys <= 0 || total == 0 {
		return 0
	}
	pages := total / maxKeys
	if total%maxKeys != 0 {
		pages++
	}
	return pages
}

func atoiDefault(s string, fallback int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return fallback
	}
	return n
}

// IAMUserIdentificationResponse is who the caller is in AuthModeIAM.
type IAMUserIdentificationResponse struct {
	UserID      string   `json:"userID"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	IsAdmin     bool     `json:"is_admin"`
}

// HandleIAMUserIdentification serves GET /user/id in AuthModeIAM.
//
//	@Summary		Identify the signed-in user
//	@Description	Returns the OIDC identity behind the session, including whether they hold an admin group. Requires server.auth_mode = "iam".
//	@Tags			User
//	@Produce		json
//	@Success		200	{object}	IAMUserIdentificationResponse	"The signed-in user"
//	@Failure		401	{object}	storage.OperationErrWithMsg		"Not signed in"
//	@Router			/api/user/id [get]
func (s *Server) HandleIAMUserIdentification() echo.HandlerFunc {
	return func(c echo.Context) error {
		session, ok := auth.SessionFrom(c)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
		}
		return c.JSON(http.StatusOK, IAMUserIdentificationResponse{
			UserID:      session.Subject,
			DisplayName: session.Display(),
			Email:       session.Email,
			Groups:      session.Groups,
			IsAdmin:     session.IsAdmin(s.auth.AdminGroups()),
		})
	}
}

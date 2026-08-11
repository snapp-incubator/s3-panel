// Package control talks to the S3-compatible authorization endpoint used in
// AuthModeIAM.
//
// It speaks only standard protocols — S3 for bucket metadata and policies, AWS
// STS for short-lived credentials — so any service implementing them can back
// the panel. Nothing here is specific to a particular authorization system.
//
// The split that matters: this endpoint answers WHICH buckets a user may touch
// and with WHAT permission, and vends the credential for object work. Object
// bytes never pass through it; they go straight to the gateway.
package control

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-authenticated client for the control endpoint.
type Client struct {
	baseURL string
	stsURL  string
	http    *http.Client
}

// New builds a client. stsURL may be empty, in which case it is derived from
// baseURL by replacing the last path segment with "sts".
func New(baseURL, stsURL string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("object_storage_config.control_url is required in iam auth mode")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid control_url: %w", err)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	stsURL = strings.TrimRight(strings.TrimSpace(stsURL), "/")
	if stsURL == "" {
		stsURL = deriveSTSURL(baseURL)
	}

	return &Client{baseURL: baseURL, stsURL: stsURL, http: &http.Client{Timeout: timeout}}, nil
}

// deriveSTSURL swaps the last path segment for "sts", which is the layout the
// AWS surfaces conventionally use (…/s3 and …/sts side by side).
func deriveSTSURL(baseURL string) string {
	if idx := strings.LastIndex(baseURL, "/"); idx > len("https:/") {
		return baseURL[:idx] + "/sts"
	}
	return baseURL + "/sts"
}

// Bucket is one entry from ListBuckets.
//
// Name, CreationDate and BucketRegion are standard S3. The rest are optional
// elements a control endpoint may add; a plain S3 implementation simply leaves
// them empty and the panel degrades to showing no permission badges.
type Bucket struct {
	Name           string    `xml:"Name"`
	CreationDate   time.Time `xml:"CreationDate"`
	BucketRegion   string    `xml:"BucketRegion"`
	Tenant         string    `xml:"Tenant"`
	RealName       string    `xml:"RealName"`
	S3Name         string    `xml:"S3Name"`
	Permissions    []string  `xml:"Permissions>Permission"`
	GrantedViaTeam string    `xml:"GrantedViaTeam"`
}

// DisplayName is what a user should see: the bare bucket name when the endpoint
// reports one, otherwise whatever it called the bucket.
func (b Bucket) DisplayName() string {
	if b.RealName != "" {
		return b.RealName
	}
	return b.Name
}

// Can reports whether the caller holds a permission on this bucket. An endpoint
// that reports no permissions at all is treated as permissive, so a stock S3
// service still works: it has already decided access by answering the call.
func (b Bucket) Can(permission string) bool {
	if len(b.Permissions) == 0 {
		return true
	}
	for _, p := range b.Permissions {
		if strings.EqualFold(p, permission) {
			return true
		}
	}
	return false
}

type listAllMyBucketsResult struct {
	XMLName xml.Name `xml:"ListAllMyBucketsResult"`
	Buckets []Bucket `xml:"Buckets>Bucket"`
	Owner   struct {
		ID          string `xml:"ID"`
		DisplayName string `xml:"DisplayName"`
	} `xml:"Owner"`
}

// BucketStats is a bucket's usage. Object count and size have no S3 verb, so
// they come from the control endpoint's own sub-resource.
type BucketStats struct {
	Name         string   `xml:"Name"`
	Tenant       string   `xml:"Tenant"`
	Region       string   `xml:"Region"`
	NumObjects   int64    `xml:"NumObjects"`
	SizeBytes    int64    `xml:"SizeBytes"`
	QuotaBytes   int64    `xml:"QuotaBytes"`
	QuotaObjects int64    `xml:"QuotaObjects"`
	Permissions  []string `xml:"Permissions>Permission"`
}

// Credentials is one short-lived credential set for object operations.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
	Endpoint        string
	Region          string
	SessionID       string
}

type getSessionTokenResponse struct {
	XMLName xml.Name `xml:"GetSessionTokenResponse"`
	Result  struct {
		Credentials struct {
			AccessKeyID     string    `xml:"AccessKeyId"`
			SecretAccessKey string    `xml:"SecretAccessKey"`
			SessionToken    string    `xml:"SessionToken"`
			Expiration      time.Time `xml:"Expiration"`
		} `xml:"Credentials"`
		Endpoint  string `xml:"Endpoint"`
		Region    string `xml:"Region"`
		SessionID string `xml:"SessionId"`
	} `xml:"GetSessionTokenResult"`
}

// s3Error is the standard S3 error body, so a failure can be reported with the
// endpoint's own code and message rather than a bare status.
type s3Error struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

// Error carries a control-endpoint failure, keeping the HTTP status so handlers
// can pass an authorization failure through as one instead of flattening it to
// a 500.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

// ListBuckets returns every bucket the caller may use.
//
// One call covers every region the endpoint knows about — that is the whole
// point of routing the listing through it rather than through a per-region
// gateway.
func (c *Client) ListBuckets(ctx context.Context, token string) ([]Bucket, error) {
	body, err := c.do(ctx, http.MethodGet, token, "/", nil, "")
	if err != nil {
		return nil, err
	}
	var out listAllMyBucketsResult
	if err := xml.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse bucket list: %w", err)
	}
	return out.Buckets, nil
}

// BucketStats fetches one bucket's usage.
func (c *Client) BucketStats(ctx context.Context, token, bucket, region string) (*BucketStats, error) {
	body, err := c.do(ctx, http.MethodGet, token, "/"+url.PathEscape(bucket)+"?stats", nil, region)
	if err != nil {
		return nil, err
	}
	var out BucketStats
	if err := xml.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse bucket stats: %w", err)
	}
	return &out, nil
}

// BucketPolicy returns a bucket's policy document, or (nil, nil) when it has
// none. A bucket with no policy is an ordinary state, not an error.
func (c *Client) BucketPolicy(ctx context.Context, token, bucket, region string) (json.RawMessage, error) {
	body, err := c.do(ctx, http.MethodGet, token, "/"+url.PathEscape(bucket)+"?policy", nil, region)
	if err != nil {
		var apiErr *Error
		if ok := asError(err, &apiErr); ok && apiErr.Code == "NoSuchBucketPolicy" {
			return nil, nil
		}
		return nil, err
	}
	return json.RawMessage(body), nil
}

// SessionCredentials asks the STS surface for the short-lived credential object
// operations are signed with.
func (c *Client) SessionCredentials(ctx context.Context, token, region string, ttl time.Duration) (*Credentials, error) {
	form := url.Values{}
	form.Set("Action", "GetSessionToken")
	if ttl > 0 {
		form.Set("DurationSeconds", fmt.Sprintf("%d", int64(ttl.Seconds())))
	}
	if region != "" {
		form.Set("Region", region)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.stsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build STS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	body, err := c.send(req)
	if err != nil {
		return nil, err
	}

	var out getSessionTokenResponse
	if err := xml.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse STS response: %w", err)
	}
	return &Credentials{
		AccessKeyID:     out.Result.Credentials.AccessKeyID,
		SecretAccessKey: out.Result.Credentials.SecretAccessKey,
		SessionToken:    out.Result.Credentials.SessionToken,
		Expiration:      out.Result.Credentials.Expiration,
		Endpoint:        out.Result.Endpoint,
		Region:          out.Result.Region,
		SessionID:       out.Result.SessionID,
	}, nil
}

// do issues one request against the control endpoint.
func (c *Client) do(ctx context.Context, method, token, path string, body io.Reader, region string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("build control request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if region != "" {
		// Disambiguates a bucket name that exists in more than one region.
		req.Header.Set("region", region)
	}
	return c.send(req)
}

func (c *Client) send(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call control endpoint: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing actionable on close

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read control response: %w", err)
	}
	if resp.StatusCode >= 400 {
		apiErr := &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(payload))}
		var parsed s3Error
		if xml.Unmarshal(payload, &parsed) == nil && parsed.Code != "" {
			apiErr.Code, apiErr.Message = parsed.Code, parsed.Message
		}
		return nil, apiErr
	}
	return payload, nil
}

// asError unwraps a *Error without pulling in errors.As at every call site.
func asError(err error, target **Error) bool {
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}

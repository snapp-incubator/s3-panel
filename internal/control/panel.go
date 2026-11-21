package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Panel talks to a unified console's app registry: the API through which an
// app on a shared console host registers itself and asks who may view it.
//
// It authenticates as the APP, with the credential an admin minted when the
// app was admitted (an "app_…" key), never as a person. That is what lets the
// panel ask about someone whose token it does not hold, and what binds every
// answer to this one app: the key, not the request, says which app is asking.
type Panel struct {
	apiURL string
	key    string
	http   *http.Client
}

// NewPanel builds a registry client. apiURL is the console's JSON API base
// (the same one the act-as calls use, ending in /api); key is the app key.
func NewPanel(apiURL, key string, timeout time.Duration) (*Panel, error) {
	apiURL = strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if apiURL == "" {
		return nil, fmt.Errorf("panel: the console API URL is required")
	}
	if _, err := url.Parse(apiURL); err != nil {
		return nil, fmt.Errorf("panel: invalid console API URL: %w", err)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("panel.api_key is required when panel.enabled is true")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Panel{apiURL: apiURL, key: key, http: &http.Client{Timeout: timeout}}, nil
}

// APIURL is where IAM's JSON API is, for callers that default to it.
func (c *Client) APIURL() string { return c.apiURL }

// PanelNavItem is one entry of the app's navigation in the console header.
type PanelNavItem struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

// PanelLink says how to open a catalog resource in this app: the console
// fills the placeholders in Path from the resource.
type PanelLink struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
}

// PanelHealth names the app's health endpoints, which the console probes.
type PanelHealth struct {
	Live  string `json:"live"`
	Ready string `json:"ready"`
}

// PanelManifest is what an app registers on every start: only what changes
// between releases. Name, icon, prefix and the like are set by an admin at
// admission and cannot be changed from here.
type PanelManifest struct {
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Nav         []PanelNavItem `json:"nav"`
	Links       []PanelLink    `json:"links"`
	Health      PanelHealth    `json:"health"`
}

// PanelApp is the part of the console's App record the panel reads back.
type PanelApp struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Prefix string `json:"prefix"`
	State  string `json:"state"`
	Status string `json:"status"`
}

// Register stores this app's manifest (PUT /panel/apps/self). The console
// refuses a manifest whose paths leave the app's prefix (400) or whose links
// claim resources the app was not admitted for (403); both come back as an
// *Error carrying the status.
func (p *Panel) Register(ctx context.Context, m PanelManifest) (*PanelApp, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode manifest: %w", err)
	}
	payload, err := p.do(ctx, http.MethodPut, "/panel/apps/self", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var app PanelApp
	if err := json.Unmarshal(payload, &app); err != nil {
		return nil, fmt.Errorf("decode registered app: %w", err)
	}
	return &app, nil
}

// Viewer is the console's answer to "may this person view the app".
//
// OwnerTeam and Reason are later additions to the contract and may be absent
// (empty) from an older console; nothing may depend on them being there.
type Viewer struct {
	View bool
	// OwnerTeam names the team that owns the app, so a refused person knows
	// whom to ask.
	OwnerTeam string
	// Reason says why: one of the Viewer* constants, or "" when not given. A
	// value this panel does not know is kept as is and treated as no reason.
	Reason string
}

// The reasons the console gives for a viewer answer.
const (
	// ViewerGranted: the person (or a team of theirs) holds view on the app.
	ViewerGranted = "granted"
	// ViewerEveryone: the app is open to every signed-in person.
	ViewerEveryone = "everyone"
	// ViewerImplied: view follows from a grant on one of the app's resources
	// (for storage, any bucket grant).
	ViewerImplied = "implied"
	// ViewerNone: nothing gives the person view.
	ViewerNone = "none"
)

// CanView asks whether the person with this OIDC subject may view this app
// (GET /panel/apps/self/viewers/{sub}).
//
// An error means the console did not answer — unreachable, rate-limited (429),
// or refusing the app key — and says nothing about the person. Deciding what to
// do without an answer is the caller's job.
func (p *Panel) CanView(ctx context.Context, subject string) (bool, error) {
	v, err := p.Viewer(ctx, subject)
	return v.View, err
}

// Viewer is CanView with the rest of the answer: the app's owner team and the
// reason, when the console gives them.
func (p *Panel) Viewer(ctx context.Context, subject string) (Viewer, error) {
	if strings.TrimSpace(subject) == "" {
		return Viewer{}, fmt.Errorf("panel: no subject to ask about")
	}
	payload, err := p.do(ctx, http.MethodGet, "/panel/apps/self/viewers/"+url.PathEscape(subject), nil)
	if err != nil {
		return Viewer{}, err
	}
	var out struct {
		View      *bool  `json:"view"`
		OwnerTeam string `json:"owner_team"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return Viewer{}, fmt.Errorf("decode viewer answer: %w", err)
	}
	// A body without the field is not an answer, and reading it as "false"
	// would hide a broken console behind a wave of refusals.
	if out.View == nil {
		return Viewer{}, fmt.Errorf("panel: viewer answer has no view field")
	}
	return Viewer{
		View:      *out.View,
		OwnerTeam: strings.TrimSpace(out.OwnerTeam),
		Reason:    strings.TrimSpace(out.Reason),
	}, nil
}

func (p *Panel) do(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.apiURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("build panel request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call panel registry: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck // nothing actionable on close

	payload, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read panel response: %w", err)
	}
	if res.StatusCode >= 300 {
		return nil, &Error{Status: res.StatusCode, Message: panelErrorMessage(payload, res.StatusCode)}
	}
	return payload, nil
}

// panelErrorMessage reads the console's {"message": …} (or {"error": …}) body,
// falling back to the status text.
func panelErrorMessage(payload []byte, status int) string {
	var body struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(payload, &body) == nil {
		if body.Message != "" {
			return body.Message
		}
		if body.Error != "" {
			return body.Error
		}
	}
	return http.StatusText(status)
}

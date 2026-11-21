package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/snapp-incubator/S3-Panel/internal/config"
)

// TestReadinessProbeSendsTheClientKey: IAM, once it requires the panel's
// client key on /s3, logs a caller without it as a refusal. The readiness
// probe calls /s3 every 15 s, so it carries the key like every other call; and
// sends nothing extra when none is configured.
func TestReadinessProbeSendsTheClientKey(t *testing.T) {
	for _, key := range []string{"panel-key", ""} {
		var mu sync.Mutex
		var got []string
		iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			got = append(got, r.Header.Get("X-IAM-STS-Client"))
			mu.Unlock()
			w.WriteHeader(http.StatusForbidden)
		}))
		t.Cleanup(iam.Close)

		s := &Server{logger: zap.NewNop()}
		s.Config.Server.AuthMode = config.AuthModeIAM
		s.Config.ObjectStorage.ControlURL = iam.URL + "/s3"
		s.Config.ObjectStorage.STSClientKey = key
		s.Config.OIDC.IssuerURL = iam.URL + "/realms/x"
		s.registerReadiness()
		s.readiness.Report(t.Context())

		mu.Lock()
		// One call reaches /s3; the other is OIDC discovery.
		found := false
		for _, h := range got {
			if h == key {
				found = true
			}
			if key == "" && h != "" {
				t.Errorf("a probe sent a client key with none configured: %q", h)
			}
		}
		mu.Unlock()
		if !found {
			t.Errorf("key %q: probe headers %q", key, got)
		}
	}
}

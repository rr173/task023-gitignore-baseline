package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"task023-gitignore/internal/gitignore"
)

// TestProbeLeadingSlashNormalization verifies that paths starting with "/"
// are normalized by stripping the leading slash before matching against rules.
func TestProbeLeadingSlashNormalization(t *testing.T) {
	srv := httptest.NewServer(New().Handler())
	defer srv.Close()

	body := `{"rules":"*.log\n","paths":["/app/error.log","/debug.log"]}`
	resp, err := http.Post(srv.URL+"/check", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}

	var out struct {
		Results []gitignore.Result `json:"results"`
		Ignored []string           `json:"ignored"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}

	// After normalization, paths should not retain leading slash.
	for _, r := range out.Results {
		if strings.HasPrefix(r.Path, "/") {
			t.Errorf("result path %q still has leading slash after normalization", r.Path)
		}
	}

	// Both paths should be ignored after normalization.
	if len(out.Ignored) != 2 {
		t.Errorf("expected 2 ignored paths, got %d: %v", len(out.Ignored), out.Ignored)
	}
}

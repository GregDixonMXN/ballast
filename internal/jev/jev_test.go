package jev

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func stubServer(t *testing.T, choice string, confidence float64, check func(t *testing.T, body map[string]any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if check != nil {
			check(t, body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{
				"route": map[string]any{"choice": choice, "confidence": confidence},
			},
		})
	}))
}

func TestJudgeSiblingRoutesDrift(t *testing.T) {
	srv := stubServer(t, "rebasable", 0.82, func(t *testing.T, body map[string]any) {
		q := body["questions"].(map[string]any)["route"].(map[string]any)
		if q["type"] != "choice" {
			t.Errorf("route type = %v", q["type"])
		}
		state := body["state"].(map[string]any)
		if !strings.Contains(state["merged_diff"].(string), "merged") {
			t.Errorf("merged diff not passed through")
		}
		base, ok := state["base_files"].(map[string]any)
		if !ok || base["backend.go"] != "package api\n" {
			t.Errorf("base files not passed through: %v", state["base_files"])
		}
	})
	defer srv.Close()
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	d, err := JudgeSibling(Sibling{MergedDiff: "merged hunk", SiblingDiff: "sibling hunk", BaseFiles: map[string]string{"backend.go": "package api\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Choice != "rebasable" || d.Confidence != 0.82 {
		t.Errorf("decision = %+v", d)
	}
}

func TestJudgeSiblingTruncatesDiffs(t *testing.T) {
	long := strings.Repeat("x", maxDiffChars+2000)
	srv := stubServer(t, "conflicted", 0.9, func(t *testing.T, body map[string]any) {
		got := body["state"].(map[string]any)["sibling_diff"].(string)
		if len([]rune(got)) != maxDiffChars {
			t.Errorf("diff chars = %d, want %d", len([]rune(got)), maxDiffChars)
		}
	})
	defer srv.Close()
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	if _, err := JudgeSibling(Sibling{SiblingDiff: long}); err != nil {
		t.Fatal(err)
	}
}

func TestJudgeSiblingRejectsUnknownChoice(t *testing.T) {
	srv := stubServer(t, "merge_both", 0.5, nil)
	defer srv.Close()
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	if _, err := JudgeSibling(Sibling{}); err == nil {
		t.Error("expected error on unknown choice")
	}
}

func TestJudgeSiblingRequiresKey(t *testing.T) {
	old, had := os.LookupEnv("JEV_API_KEY")
	_ = os.Unsetenv("JEV_API_KEY")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("JEV_API_KEY", old)
		}
	})
	if _, err := JudgeSibling(Sibling{}); err == nil {
		t.Error("expected error with no key")
	}
}

func TestEnabledFollowsFlag(t *testing.T) {
	t.Setenv("BALLAST_JEV", "1")
	if !Enabled() {
		t.Error("want enabled with BALLAST_JEV=1")
	}
	t.Setenv("BALLAST_JEV", "")
	if Enabled() {
		t.Error("want disabled without flag")
	}
}

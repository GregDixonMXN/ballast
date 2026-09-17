// Package jev judges stale sibling changesets through TypeSafe's System
// One model. When a sibling no longer applies onto the new head, one Choice
// question separates mechanical drift (rebasable: the sibling's intent
// survives, mark NEEDS_REBASE) from a genuine clash (conflicted: human
// needed, mark CONFLICTED). Opt-in only: no flag, no cloud, no call.
package jev

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"
)

const defaultEndpoint = "https://api.typesafe.ai/v1/systemone"

// Diff caps: the judgment needs the shape of each change, not all of it.
const maxDiffChars = 4000

// Sibling is the pair to judge: what already merged and what went stale,
// plus the base file content both started from (the context that lets the
// model tell drift from clash).
type Sibling struct {
	MergedDiff   string
	MergedFiles  []string
	SiblingDiff  string
	SiblingFiles []string
	BaseFiles    map[string]string
}

// Decision is the judged routing for the stale sibling.
type Decision struct {
	Choice     string // "rebasable" or "conflicted"
	Confidence float64
}

type choiceAnswer struct {
	Choice     string  `json:"choice"`
	Confidence float64 `json:"confidence"`
}

type systemOneResponse struct {
	Answers map[string]choiceAnswer `json:"answers"`
}

func clampDiff(s string) string {
	r := []rune(s)
	if len(r) <= maxDiffChars {
		return s
	}
	return string(r[:maxDiffChars])
}

// clampBase caps base file context: first files in key order, 4000 chars
// total. Missing or unreadable files simply contribute nothing.
func clampBase(files map[string]string) map[string]string {
	const maxTotal = 4000
	out := map[string]string{}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total := 0
	for _, k := range keys {
		if total >= maxTotal {
			break
		}
		r := []rune(files[k])
		room := maxTotal - total
		if len(r) > room {
			r = r[:room]
		}
		out[k] = string(r)
		total += len(r)
	}
	return out
}

// Enabled reports whether semantic sibling triage is turned on. Mirrors
// the BALLAST_BRAIN=1 precedent: default launches stay facts-only.
func Enabled() bool {
	return os.Getenv("BALLAST_JEV") == "1"
}

// JudgeSibling routes a stale sibling. Errors when the judgment could not
// be obtained (flag off without key, network failure, bad response) — the
// caller keeps today's CONFLICTED verdict on any error.
func JudgeSibling(s Sibling) (Decision, error) {
	key := os.Getenv("JEV_API_KEY")
	if key == "" {
		return Decision{}, fmt.Errorf("jev: JEV_API_KEY is not set")
	}
	endpoint := os.Getenv("JEV_API_URL")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	body, err := json.Marshal(map[string]any{
		"model": "jev-latest",
		"state": map[string]any{
			"base_files":    clampBase(s.BaseFiles),
			"merged_diff":   clampDiff(s.MergedDiff),
			"merged_files":  s.MergedFiles,
			"sibling_diff":  clampDiff(s.SiblingDiff),
			"sibling_files": s.SiblingFiles,
		},
		"questions": map[string]any{
			"route": map[string]any{
				"type":         "choice",
				"instructions": "The merged change already integrated. The sibling change no longer applies cleanly onto the new head. Given the base files both started from and both diffs: does the sibling's intent survive — is this only mechanical drift (context lines, adjacent hunks) that a rebase would fix — or do the two changes genuinely clash on the same logic?",
				"criteria": map[string]any{
					"rebasable":  "Mechanical drift only; the sibling's intent survives and a rebase would apply it",
					"conflicted": "Genuine clash: both changes rewrite the same logic and a human must reconcile them",
				},
			},
		},
	})
	if err != nil {
		return Decision{}, fmt.Errorf("jev: encode request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Decision{}, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("jev: request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Decision{}, fmt.Errorf("jev: endpoint returned %s", res.Status)
	}
	var decoded systemOneResponse
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		return Decision{}, fmt.Errorf("jev: decode response: %w", err)
	}
	route, ok := decoded.Answers["route"]
	if !ok {
		return Decision{}, fmt.Errorf("jev: response missing route")
	}
	if route.Choice != "rebasable" && route.Choice != "conflicted" {
		return Decision{}, fmt.Errorf("jev: unknown choice %q", route.Choice)
	}
	return Decision{Choice: route.Choice, Confidence: route.Confidence}, nil
}

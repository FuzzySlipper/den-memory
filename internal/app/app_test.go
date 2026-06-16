package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
)

func newTestServer(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	a, err := New(Config{DatabasePath: filepath.Join(t.TempDir(), "api.sqlite"), RootPath: "../.."})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		ts.Close()
		if err := a.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	return a, ts
}

func TestHealthAndVersion(t *testing.T) {
	_, ts := newTestServer(t)
	health := getJSON(t, ts.URL+"/health")
	if health["ok"] != true {
		t.Fatalf("health ok = %v", health["ok"])
	}
	version := getJSON(t, ts.URL+"/api/version")
	if version["service"] != "den-memories" {
		t.Fatalf("service = %v", version["service"])
	}
	if version["scoring_profile"] != "v0-default" {
		t.Fatalf("scoring_profile = %v", version["scoring_profile"])
	}
}

func TestCapturePromoteRecallAndAudit(t *testing.T) {
	_, ts := newTestServer(t)
	capture := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime":              "hermes",
		"actor_identity":       "capture-agent",
		"raw_text":             "Hermes prompt cache invariant means mid-session captures cannot mutate system prompt.",
		"title":                "Hermes prompt cache invariant",
		"summary":              "Prompt cache invariant",
		"proposed_kind":        "fact",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"discovery_scope":      "same_project",
		"claim_strength":       "assessment",
		"source_refs": []any{map[string]any{
			"source_kind":         "den_task",
			"source_project_id":   "den-memory",
			"source_id":           "2472",
			"verification_status": "verified",
		}},
	})
	if capture["decision"] != "promoted" {
		t.Fatalf("capture decision = %v", capture["decision"])
	}
	entry := capture["memory_entry"].(map[string]any)
	if entry["slug"] == "" || entry["status"] != "active" || entry["curation_state"] != "curated" {
		t.Fatalf("auto-promoted entry state = %#v", entry)
	}

	// Also verify the classic manual promote path still works via direct candidate.
	workerCandidate := postJSON(t, ts.URL+"/api/candidates", map[string]any{
		"title":         "Manual promote test",
		"summary":       "Manual promote still works",
		"body_md":       "Manual promote body",
		"proposed_kind": "fact",
		"scope_kind":    "project",
		"scope_id":      "den-memory",
		"source_refs":   []any{map[string]any{"source_kind": "manual_note", "source_id": "manual", "verification_status": "unverified"}},
	})
	manualPromote := postJSON(t, ts.URL+"/api/curation/candidates/"+idString(workerCandidate["id"])+"/promote", map[string]any{
		"actor_identity": "curator",
		"reason":         "verified manually",
		"slug":           "manual-promote-test",
	})
	if manualPromote["memory_entry"] == nil {
		t.Fatalf("manual promote missing entry: %#v", manualPromote)
	}

	// Recall should find auto-promoted entry by its slug pattern.
	packet := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":          "Hermes",
		"scope_kind":     "project",
		"scope_id":       "den-memory",
		"packet_id":      "packet-test",
		"actor_identity": "tester",
	})
	nodes := packet["included_nodes"].([]any)
	if len(nodes) == 0 {
		t.Fatalf("recall returned no nodes: %#v", packet)
	}
	first := nodes[0].(map[string]any)
	if first["slug"] != entry["slug"] {
		t.Fatalf("first recall slug = %v, want %v", first["slug"], entry["slug"])
	}
	audit := getText(t, ts.URL+"/api/audit/export?format=jsonl")
	if !bytes.Contains([]byte(audit), []byte(`"record_type":"metadata"`)) {
		t.Fatalf("audit jsonl missing metadata: %s", audit)
	}
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status %d body %#v", url, resp.StatusCode, body)
	}
	return body
}

func postJSON(t *testing.T, url string, payload map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode POST %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status %d body %#v", url, resp.StatusCode, body)
	}
	return body
}

func getText(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read text: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status %d body %s", url, resp.StatusCode, buf.String())
	}
	return buf.String()
}

func idString(value any) string {
	switch v := value.(type) {
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	default:
		return fmt.Sprint(v)
	}
}

package curator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeterministicProposerPromotePayload(t *testing.T) {
	proposer := DeterministicProposer{Action: "promote", ProposerIdentity: "curator-cli", ProposerKind: "deterministic_cli", Reason: "candidate looks useful"}
	proposals, err := proposer.Propose(context.Background(), Packet{
		QueueState: "needs_proposal",
		Candidate: map[string]any{
			"id":                   12,
			"proposed_slug":        "go-canonical-memory",
			"title":                "Go canonical memory",
			"summary":              "Use Go",
			"body_md":              "Use Go for repo-owned CLIs.",
			"proposed_kind":        "policy_note",
			"scope_kind":           "project",
			"scope_id":             "den-memory",
			"authority_scope_kind": "project",
			"authority_scope_id":   "den-memory",
			"discovery_scope":      "same_project",
			"claim_strength":       "recommendation",
		},
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	proposal := proposals[0]
	if proposal["proposal_kind"] != "promote_candidate" {
		t.Fatalf("proposal kind = %#v", proposal)
	}
	entry := proposal["proposed_entry"].(map[string]any)
	if entry["slug"] != "go-canonical-memory" || entry["kind"] != "policy_note" {
		t.Fatalf("entry payload = %#v", entry)
	}
}

func TestRunCreatesProposalOnlyAndNeverApplies(t *testing.T) {
	applyCalled := false
	proposalCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/curation/queue":
			writeTestJSON(t, w, map[string]any{"items": []any{map[string]any{
				"queue_state": "needs_proposal",
				"candidate": map[string]any{
					"id":            7,
					"proposed_slug": "queued-candidate",
					"title":         "Queued candidate",
					"summary":       "Queued summary",
					"body_md":       "Queued body",
					"proposed_kind": "fact",
				},
				"source_refs": []any{map[string]any{"source_kind": "manual_note", "source_id": "queue"}},
				"proposals":   []any{},
			}}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/curation/proposals":
			proposalCalled = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode proposal: %v", err)
			}
			if body["proposal_kind"] != "reject_candidate" {
				t.Fatalf("proposal body = %#v", body)
			}
			body["id"] = float64(42)
			body["status"] = "proposed"
			writeTestJSON(t, w, body)
		case strings.Contains(r.URL.Path, "/apply"):
			applyCalled = true
			w.WriteHeader(http.StatusTeapot)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer ts.Close()

	result, err := Run(context.Background(), Config{BaseURL: ts.URL, Action: "reject", ProposerIdentity: "curator-cli", Reason: "deterministic reject"}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !proposalCalled || applyCalled {
		t.Fatalf("proposalCalled=%v applyCalled=%v", proposalCalled, applyCalled)
	}
	if len(result.CreatedProposalIDs) != 1 || result.CreatedProposalIDs[0] != 42 {
		t.Fatalf("result = %#v", result)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

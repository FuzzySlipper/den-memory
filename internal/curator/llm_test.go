package curator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLLMProposerStoresValidatedProposalOnly(t *testing.T) {
	chatCalled := false
	var sawPrompt bool
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected LLM request %s %s", r.Method, r.URL.Path)
		}
		chatCalled = true
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		messages := body["messages"].([]any)
		userMessage := messages[1].(map[string]any)["content"].(string)
		sawPrompt = strings.Contains(userMessage, "Allowed proposal_kind") && strings.Contains(userMessage, "knowledge_candidate")
		writeTestJSON(t, w, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"proposals":[{"proposal_kind":"knowledge_candidate","status":"needs_human","reason":"This is reusable operational guidance better routed to the Knowledge Library first.","confidence":"inferred","proposed_action":{"action":"knowledge_candidate","candidate_id":55,"payload":{"summary":"Reusable reference candidate"}}}]}`}}}})
	}))
	defer llm.Close()

	applyCalled := false
	proposalCalled := false
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/curation/queue":
			writeTestJSON(t, w, map[string]any{"items": []any{queueItem(55)}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/curation/proposals":
			proposalCalled = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode proposal: %v", err)
			}
			if body["proposal_kind"] != "knowledge_candidate" || body["proposer_kind"] != "llm" {
				t.Fatalf("unexpected proposal body: %#v", body)
			}
			metadata := body["model_metadata"].(map[string]any)
			if metadata["mode"] != "llm" || metadata["provider"] != "openai_compatible" || metadata["model"] != "test-model" || metadata["prompt_sha256"] == "" || metadata["response_sha256"] == "" {
				t.Fatalf("missing audit metadata: %#v", metadata)
			}
			candidateIDs := body["candidate_ids"].([]any)
			if len(candidateIDs) != 1 || intFromAny(candidateIDs[0]) != 55 {
				t.Fatalf("candidate_ids not normalized: %#v", body)
			}
			body["id"] = float64(9)
			writeTestJSON(t, w, body)
		case strings.Contains(r.URL.Path, "/apply"):
			applyCalled = true
			w.WriteHeader(http.StatusTeapot)
		default:
			t.Fatalf("unexpected service request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer service.Close()

	proposer, err := NewLLMProposer(LLMConfig{BaseURL: llm.URL, Model: "test-model", ProposerIdentity: "planner-curator"})
	if err != nil {
		t.Fatalf("NewLLMProposer: %v", err)
	}
	result, err := Run(context.Background(), Config{BaseURL: service.URL}, proposer)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !chatCalled || !sawPrompt || !proposalCalled || applyCalled {
		t.Fatalf("chatCalled=%v sawPrompt=%v proposalCalled=%v applyCalled=%v", chatCalled, sawPrompt, proposalCalled, applyCalled)
	}
	if len(result.CreatedProposalIDs) != 1 || result.CreatedProposalIDs[0] != 9 {
		t.Fatalf("result = %#v", result)
	}
}

func TestLLMProposerRejectsMalformedModelOutputBeforeStorage(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `not json`}}}})
	}))
	defer llm.Close()

	proposalCalled := false
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/curation/queue":
			writeTestJSON(t, w, map[string]any{"items": []any{queueItem(61)}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/curation/proposals":
			proposalCalled = true
			w.WriteHeader(http.StatusTeapot)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer service.Close()

	proposer, err := NewLLMProposer(LLMConfig{BaseURL: llm.URL, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewLLMProposer: %v", err)
	}
	_, err = Run(context.Background(), Config{BaseURL: service.URL}, proposer)
	if err == nil {
		t.Fatalf("expected malformed LLM output error")
	}
	if proposalCalled {
		t.Fatalf("malformed model output should not be stored as a proposal")
	}
}

func TestLLMProposerValidatesPromoteRequiresEntry(t *testing.T) {
	_, err := parseLLMProposalResponse(`{"proposals":[{"proposal_kind":"promote_candidate","reason":"good idea"}]}`)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	proposal := Proposal{"proposal_kind": "promote_candidate", "reason": "good idea"}
	err = normalizeAndValidateLLMProposal(proposal, Packet{QueueState: "needs_proposal", Candidate: map[string]any{"id": 5}}, LLMConfig{Model: "test-model", ProposerIdentity: "p", ProposerKind: "llm", MaxPacketBytes: 100}, "prompt", "response")
	if err == nil || !strings.Contains(err.Error(), "proposed_entry") {
		t.Fatalf("expected proposed_entry validation error, got %v", err)
	}
}

func queueItem(id int) map[string]any {
	return map[string]any{
		"queue_state": "needs_proposal",
		"candidate": map[string]any{
			"id":                   id,
			"proposed_slug":        "candidate-slug",
			"title":                "Candidate title",
			"summary":              "Candidate summary",
			"body_md":              "Candidate body",
			"proposed_kind":        "fact",
			"scope_kind":           "project",
			"scope_id":             "den-memory",
			"authority_scope_kind": "project",
			"authority_scope_id":   "den-memory",
			"discovery_scope":      "same_project",
			"claim_strength":       "observation",
		},
		"source_refs": []any{map[string]any{"source_kind": "den_task", "source_id": "2525", "verification_status": "verified"}},
		"proposals":   []any{},
	}
}

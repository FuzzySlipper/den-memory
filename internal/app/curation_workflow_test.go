package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAgentCandidateCuratorProposalApplyAgentRecallWorkflow(t *testing.T) {
	_, ts := newTestServer(t)

	capture := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime_context":       runtimeContext("hermes", "candidate-agent", "runner", "den-memory"),
		"raw_content":           "Go should be the canonical implementation language for Den Memories service and CLI work.",
		"title":                 "Den Memories Go canonical curation workflow",
		"summary":               "Den Memories service and CLI work should stay in Go where possible.",
		"event_kind":            "task_message",
		"proposed_kind":         "policy_note",
		"proposed_slug":         "den-memory-go-canonical-curation-workflow",
		"proposed_scope_kind":   "project",
		"proposed_scope_id":     "den-memory",
		"discovery_scope":       "same_project",
		"claim_strength":        "recommendation",
		"extraction_confidence": 0.91,
		"source_refs":           []any{sourceRef("den_task", "2521", "Patch language constraint and curation workflow task")},
	})
	if capture["decision"] != "captured" {
		t.Fatalf("capture decision = %#v", capture)
	}
	candidate := capture["candidate"].(map[string]any)
	if candidate["status"] != "pending" {
		t.Fatalf("candidate status = %#v", candidate)
	}

	preRecall := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":          "canonical curation workflow",
		"scope_kind":     "project",
		"scope_id":       "den-memory",
		"actor_identity": "later-agent",
	})
	if packetContainsSlug(preRecall, "den-memory-go-canonical-curation-workflow") {
		t.Fatalf("candidate was recall-visible before proposal/apply: %#v", preRecall)
	}

	queue := getJSON(t, ts.URL+"/api/curation/queue")
	item := queueItemForCandidate(t, queue, idString(candidate["id"]))
	if item["queue_state"] != "needs_proposal" || item["suggested_next_action"] != "run_curator" {
		t.Fatalf("initial queue item = %#v", item)
	}

	proposal := postJSON(t, ts.URL+"/api/curation/proposals", map[string]any{
		"proposal_kind":     "promote_candidate",
		"candidate_ids":     []any{candidate["id"]},
		"proposer_identity": "deterministic-curator-test",
		"proposer_kind":     "deterministic_cli",
		"confidence":        "source_backed",
		"reason":            "Source ref supports promotion after human review.",
		"source_refs":       []any{sourceRef("den_task", "2521", "proposal evidence")},
		"proposed_entry": map[string]any{
			"slug":                 "den-memory-go-canonical-curation-workflow",
			"title":                "Den Memories Go canonical curation workflow",
			"summary":              "Use Go for Den Memories service/CLI curation workflow code where possible.",
			"body_md":              "Den Memories repository-owned curation workflow code should stay in Go whenever possible; other languages are adapter/client glue only.",
			"kind":                 "policy_note",
			"scope_kind":           "project",
			"scope_id":             "den-memory",
			"authority_scope_kind": "project",
			"authority_scope_id":   "den-memory",
			"discovery_scope":      "same_project",
			"claim_strength":       "policy",
			"confidence":           "source_backed",
			"stability":            "evolving",
		},
	})
	if proposal["status"] != "proposed" {
		t.Fatalf("proposal status = %#v", proposal)
	}

	candidateAfterProposal := getJSON(t, ts.URL+"/api/candidates/"+idString(candidate["id"]))
	if candidateAfterProposal["status"] != "pending" {
		t.Fatalf("proposal mutated candidate: %#v", candidateAfterProposal)
	}
	queue = getJSON(t, ts.URL+"/api/curation/queue")
	item = queueItemForCandidate(t, queue, idString(candidate["id"]))
	if item["queue_state"] != "needs_decision" || len(item["proposals"].([]any)) != 1 {
		t.Fatalf("queue after proposal = %#v", item)
	}
	midRecall := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":          "Go canonical curation workflow",
		"scope_kind":     "project",
		"scope_id":       "den-memory",
		"actor_identity": "later-agent",
	})
	if packetContainsSlug(midRecall, "den-memory-go-canonical-curation-workflow") {
		t.Fatalf("unapplied proposal was recall-visible: %#v", midRecall)
	}

	apply := postJSON(t, ts.URL+"/api/curation/proposals/"+idString(proposal["id"])+"/apply", map[string]any{
		"actor_identity": "human-curator",
		"reason":         "Reviewed proposal and accepted source-backed memory.",
	})
	appliedProposal := apply["proposal"].(map[string]any)
	if appliedProposal["status"] != "applied" || appliedProposal["applied_by"] != "human-curator" {
		t.Fatalf("applied proposal = %#v", appliedProposal)
	}
	appliedCandidate := apply["candidate"].(map[string]any)
	if appliedCandidate["status"] != "promoted" {
		t.Fatalf("applied candidate = %#v", appliedCandidate)
	}
	entry := apply["memory_entry"].(map[string]any)
	if entry["status"] != "active" || entry["curation_state"] != "curated" {
		t.Fatalf("entry state = %#v", entry)
	}
	if apply["curation_event_id"] == nil {
		t.Fatalf("missing curation event id: %#v", apply)
	}
	refs := getJSON(t, ts.URL+"/api/source-refs?target_kind=memory_entry&target_id="+idString(entry["id"]))
	if len(refs["items"].([]any)) != 1 {
		t.Fatalf("promoted source refs did not survive: %#v", refs)
	}

	finalRecall := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":          "Go canonical curation workflow",
		"scope_kind":     "project",
		"scope_id":       "den-memory",
		"actor_identity": "later-agent",
	})
	if !packetContainsSlug(finalRecall, "den-memory-go-canonical-curation-workflow") || !strings.Contains(finalRecall["packet_md"].(string), "Den Memories Go canonical") {
		t.Fatalf("promoted memory missing from recall: %#v", finalRecall)
	}

	second := postJSON(t, ts.URL+"/api/candidates", map[string]any{
		"title":         "Unapplied proposal guard",
		"summary":       "This proposal must not appear in recall until apply.",
		"body_md":       "Unapplied proposal guard body.",
		"proposed_slug": "unapplied-proposal-guard",
		"proposed_kind": "fact",
		"scope_kind":    "project",
		"scope_id":      "den-memory",
		"source_refs":   []any{sourceRef("manual_note", "unapplied", "unapplied proposal guard")},
	})
	_ = postJSON(t, ts.URL+"/api/curation/proposals", map[string]any{
		"proposal_kind":     "promote_candidate",
		"candidate_ids":     []any{second["id"]},
		"proposer_identity": "deterministic-curator-test",
		"proposer_kind":     "deterministic_cli",
		"confidence":        "source_backed",
		"reason":            "Guard proposal only; do not apply.",
		"proposed_entry":    map[string]any{"slug": "unapplied-proposal-guard", "title": "Unapplied proposal guard", "body_md": "Should not appear", "kind": "fact"},
	})
	guardRecall := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":          "Unapplied proposal guard",
		"scope_kind":     "project",
		"scope_id":       "den-memory",
		"actor_identity": "later-agent",
	})
	if packetContainsSlug(guardRecall, "unapplied-proposal-guard") {
		t.Fatalf("unapplied guard proposal appeared in recall: %#v", guardRecall)
	}
}

func TestCurationProposalValidationAndRejectDeferDoNotMutateTruth(t *testing.T) {
	_, ts := newTestServer(t)
	candidate := postJSON(t, ts.URL+"/api/candidates", map[string]any{
		"title":         "Reject defer candidate",
		"summary":       "Reject/defer proposal state should not mutate candidate truth.",
		"body_md":       "Reject/defer proposal state should not mutate candidate truth.",
		"proposed_kind": "fact",
		"scope_kind":    "project",
		"scope_id":      "den-memory",
	})

	postJSONStatus(t, ts.URL+"/api/curation/proposals", map[string]any{
		"proposal_kind": "promote_candidate",
		"candidate_ids": []any{candidate["id"]},
		"reason":        "missing proposer should fail",
	}, http.StatusBadRequest)

	rejectProposal := postJSON(t, ts.URL+"/api/curation/proposals", map[string]any{
		"proposal_kind":     "reject_candidate",
		"candidate_ids":     []any{candidate["id"]},
		"proposer_identity": "deterministic-curator-test",
		"proposer_kind":     "deterministic_cli",
		"reason":            "Candidate appears noisy.",
		"proposed_action":   map[string]any{"action": "reject_candidate", "candidate_id": candidate["id"], "payload": map[string]any{"noise_category": "test"}},
	})
	_ = postJSON(t, ts.URL+"/api/curation/proposals/"+idString(rejectProposal["id"])+"/reject", map[string]any{
		"actor_identity": "human-curator",
		"reason":         "Rejecting proposal, not candidate.",
	})
	candidateAfterReject := getJSON(t, ts.URL+"/api/candidates/"+idString(candidate["id"]))
	if candidateAfterReject["status"] != "pending" {
		t.Fatalf("proposal reject mutated candidate: %#v", candidateAfterReject)
	}
	rejectedQueueItem := queueItemForCandidate(t, getJSON(t, ts.URL+"/api/curation/queue"), idString(candidate["id"]))
	if rejectedQueueItem["queue_state"] != "needs_proposal" {
		t.Fatalf("rejected-only proposal should return candidate to needs_proposal: %#v", rejectedQueueItem)
	}

	deferProposal := postJSON(t, ts.URL+"/api/curation/proposals", map[string]any{
		"proposal_kind":     "promote_candidate",
		"candidate_ids":     []any{candidate["id"]},
		"proposer_identity": "deterministic-curator-test",
		"proposer_kind":     "deterministic_cli",
		"reason":            "Need more context before applying.",
		"proposed_entry":    map[string]any{"slug": "reject-defer-candidate", "title": "Reject defer candidate", "kind": "fact"},
	})
	_ = postJSON(t, ts.URL+"/api/curation/proposals/"+idString(deferProposal["id"])+"/defer", map[string]any{
		"actor_identity": "human-curator",
		"reason":         "Waiting for more context.",
	})
	candidateAfterDefer := getJSON(t, ts.URL+"/api/candidates/"+idString(candidate["id"]))
	if candidateAfterDefer["status"] != "pending" {
		t.Fatalf("proposal defer mutated candidate: %#v", candidateAfterDefer)
	}
	deferredQueueItem := queueItemForCandidate(t, getJSON(t, ts.URL+"/api/curation/queue"), idString(candidate["id"]))
	if deferredQueueItem["queue_state"] != "needs_proposal" {
		t.Fatalf("terminal-only proposals should leave candidate eligible for reproposal: %#v", deferredQueueItem)
	}
}

func queueItemForCandidate(t *testing.T, queue map[string]any, candidateID string) map[string]any {
	t.Helper()
	for _, raw := range queue["items"].([]any) {
		item := raw.(map[string]any)
		candidate := item["candidate"].(map[string]any)
		if idString(candidate["id"]) == candidateID {
			return item
		}
	}
	t.Fatalf("candidate %s missing from queue: %#v", candidateID, queue)
	return nil
}

func packetContainsSlug(packet map[string]any, slug string) bool {
	for _, raw := range packet["included_nodes"].([]any) {
		node := raw.(map[string]any)
		if node["slug"] == slug {
			return true
		}
	}
	return strings.Contains(stringValueForTest(packet["packet_md"]), slug)
}

func stringValueForTest(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func postJSONStatus(t *testing.T, url string, payload map[string]any, wantStatus int) map[string]any {
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
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status %d want %d body %#v", url, resp.StatusCode, wantStatus, body)
	}
	return body
}

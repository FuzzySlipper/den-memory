package recall

import (
	"math"
	"testing"

	"den-memories/internal/contracts"
	"den-memories/internal/store"
)

func testScoring(t *testing.T) *Scoring {
	t.Helper()
	s, err := LoadScoring(contracts.NewLoader("../.."))
	if err != nil {
		t.Fatalf("LoadScoring: %v", err)
	}
	return s
}

func baseItem() store.Record {
	return store.Record{
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"discovery_scope":      "same_project",
		"claim_strength":       "assessment",
		"status":               "active",
		"confidence":           "source_backed",
	}
}

func TestScoreItemAuthorityLabels(t *testing.T) {
	s := testScoring(t)
	ctx := Context{ScopeKind: "project", ScopeID: "den-memory"}

	authoritative := s.ScoreItem(baseItem(), ctx, []string{"verified"}, 0, "")
	if authoritative["authority_label"] != "authoritative" {
		t.Fatalf("authoritative label = %#v", authoritative)
	}

	discoverable := baseItem()
	discoverable["authority_scope_id"] = "other-project"
	discoverable["scope_id"] = "other-project"
	discoverable["discovery_scope"] = "global_discoverable"
	discovered := s.ScoreItem(discoverable, ctx, []string{"verified"}, 0, "")
	if discovered["authority_label"] != "discovered_evidence" {
		t.Fatalf("discovered label = %#v", discovered)
	}

	out := baseItem()
	out["authority_scope_id"] = "other-project"
	out["scope_id"] = "other-project"
	out["discovery_scope"] = "explicit_only"
	outScore := s.ScoreItem(out, ctx, []string{"verified"}, 0, "")
	if outScore["authority_label"] != "out_of_scope" {
		t.Fatalf("out-of-scope label = %#v", outScore)
	}
}

func TestScoreItemUsesDefaultsForSourceAveragingAndEdgeDecay(t *testing.T) {
	s := testScoring(t)
	ctx := Context{ScopeKind: "project", ScopeID: "den-memory"}
	item := baseItem()

	base := s.ScoreItem(item, ctx, []string{"verified", "broken"}, 0, "")
	want := s.weight("root_match.authoritative_scope_match") +
		s.weight("claim_strength_modifier.assessment") +
		s.weight("entry_status_modifier.active") +
		s.weight("confidence_modifier.source_backed") +
		(s.weight("source_validation_modifier.verified")+s.weight("source_validation_modifier.broken"))/2
	if !closeEnough(base["score"].(float64), want) {
		t.Fatalf("source averaging score = %v, want %v", base["score"], want)
	}

	edged := s.ScoreItem(item, ctx, []string{"verified", "broken"}, 2, "failure_mode")
	wantEdge := (want + s.weight("edge_relation_modifier.failure_mode")) * math.Pow(s.weight("traversal.edge_traversal_decay"), 2)
	if !closeEnough(edged["score"].(float64), math.Round(wantEdge*1000)/1000) {
		t.Fatalf("edge decay score = %v, want %v", edged["score"], wantEdge)
	}
}

func TestScoreItemMissingWeightFallbackIsZero(t *testing.T) {
	s := testScoring(t)
	ctx := Context{ScopeKind: "project", ScopeID: "den-memory"}
	item := baseItem()
	item["claim_strength"] = "future_strength"
	item["confidence"] = "future_confidence"
	got := s.ScoreItem(item, ctx, []string{"future_status"}, 0, "future_relation")
	want := s.weight("root_match.authoritative_scope_match") + s.weight("entry_status_modifier.active")
	if !closeEnough(got["score"].(float64), want) {
		t.Fatalf("missing fallback score = %v, want %v", got["score"], want)
	}
}

func closeEnough(got, want float64) bool {
	return math.Abs(got-want) < 0.001
}

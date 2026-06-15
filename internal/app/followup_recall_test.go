package app

import (
	"strings"
	"testing"
)

func createTopicNode(t *testing.T, baseURL string, slug, title, summary string) map[string]any {
	t.Helper()
	return postJSON(t, baseURL+"/api/topic-nodes", map[string]any{
		"slug":                 slug,
		"title":                title,
		"summary":              summary,
		"node_type":            "concept",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"discovery_scope":      "same_project",
		"claim_strength":       "assessment",
		"created_by":           "test",
	})
}

func createTopicEdge(t *testing.T, baseURL string, from, to any, relation string, priority int) map[string]any {
	t.Helper()
	return postJSON(t, baseURL+"/api/topic-edges", map[string]any{
		"from_node_id": from,
		"to_node_id":   to,
		"relation":     relation,
		"priority":     priority,
		"created_by":   "test",
	})
}

func createTopicView(t *testing.T, baseURL string, slug string, rootID any, viewType string, include []any, exclude []any, ordering string) map[string]any {
	t.Helper()
	return postJSON(t, baseURL+"/api/topic-views", map[string]any{
		"slug":              slug,
		"title":             strings.ReplaceAll(slug, "-", " "),
		"root_node_id":      rootID,
		"view_type":         viewType,
		"mode":              "review",
		"include_relations": include,
		"exclude_relations": exclude,
		"max_depth":         1,
		"ordering_policy":   ordering,
		"created_by":        "test",
	})
}

func includedSlugs(packet map[string]any) []string {
	items := packet["included_nodes"].([]any)
	slugs := []string{}
	for _, raw := range items {
		slugs = append(slugs, raw.(map[string]any)["slug"].(string))
	}
	return slugs
}

func hasSlug(slugs []string, want string) bool {
	for _, slug := range slugs {
		if slug == want {
			return true
		}
	}
	return false
}

func TestTopicViewTraversalPoliciesAffectRecall(t *testing.T) {
	_, ts := newTestServer(t)
	root := createTopicNode(t, ts.URL, "memory-adapter-root", "Memory adapter root", "root topic for plugin traversal")
	prereq := createTopicNode(t, ts.URL, "implementation-prerequisite", "Implementation prerequisite", "plugin implementation prerequisite")
	failure := createTopicNode(t, ts.URL, "failure-mode-path", "Failure mode path", "plugin failure mode")
	historical := createTopicNode(t, ts.URL, "historical-context-path", "Historical context path", "plugin historical context")
	reviewRisk := createTopicNode(t, ts.URL, "review-risk-path", "Review risk path", "plugin review risk")

	createTopicEdge(t, ts.URL, root["id"], prereq["id"], "prerequisite", 1)
	createTopicEdge(t, ts.URL, root["id"], failure["id"], "failure_mode", 1)
	createTopicEdge(t, ts.URL, root["id"], historical["id"], "historical_context", 10)
	createTopicEdge(t, ts.URL, root["id"], reviewRisk["id"], "review_risk", 0)

	createTopicView(t, ts.URL, "reviewer-view", root["id"], "reviewer_path", nil, []any{"historical_context"}, "review_risk_first")
	createTopicView(t, ts.URL, "implementation-view", root["id"], "implementation_path", []any{"prerequisite", "failure_mode"}, nil, "core_first_then_risks")

	reviewer := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":           "root",
		"topic_view_slug": "reviewer-view",
		"scope_kind":      "project",
		"scope_id":        "den-memory",
		"limit":           10,
	})
	reviewerSlugs := includedSlugs(reviewer)
	if hasSlug(reviewerSlugs, "historical-context-path") {
		t.Fatalf("reviewer view included excluded historical context: %#v", reviewerSlugs)
	}
	if !hasSlug(reviewerSlugs, "review-risk-path") {
		t.Fatalf("reviewer view did not include review risk: %#v", reviewerSlugs)
	}
	if len(reviewer["included_edges"].([]any)) == 0 || reviewer["included_edges"].([]any)[0].(map[string]any)["relation"] != "review_risk" {
		t.Fatalf("review_risk_first did not order review risk edge first: %#v", reviewer["included_edges"])
	}

	implementation := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":           "root",
		"topic_view_slug": "implementation-view",
		"scope_kind":      "project",
		"scope_id":        "den-memory",
		"limit":           10,
	})
	implSlugs := includedSlugs(implementation)
	if !hasSlug(implSlugs, "implementation-prerequisite") || !hasSlug(implSlugs, "failure-mode-path") {
		t.Fatalf("implementation view missing prerequisite/failure mode: %#v", implSlugs)
	}
	if hasSlug(implSlugs, "historical-context-path") || hasSlug(implSlugs, "review-risk-path") {
		t.Fatalf("implementation include policy leaked extra edges: %#v", implSlugs)
	}
}

func TestRecallPacketShapeAndTinyBudgetTruncation(t *testing.T) {
	_, ts := newTestServer(t)
	createDirectEntry(t, ts.URL, map[string]any{
		"slug":                 "budget-packet-one",
		"title":                "Budget packet one",
		"summary":              strings.Repeat("long summary ", 20),
		"body_md":              "budget packet one body",
		"kind":                 "fact",
		"status":               "active",
		"curation_state":       "curated",
		"confidence":           "source_backed",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"discovery_scope":      "same_project",
		"source_refs":          []any{sourceRef("den_task", "2495", "shape source")},
	})
	createDirectEntry(t, ts.URL, map[string]any{
		"slug":                 "budget-packet-two",
		"title":                "Budget packet two",
		"summary":              strings.Repeat("second long summary ", 20),
		"body_md":              "budget packet two body",
		"kind":                 "fact",
		"status":               "active",
		"curation_state":       "curated",
		"confidence":           "source_backed",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"discovery_scope":      "same_project",
		"source_refs":          []any{sourceRef("den_task", "2497", "budget source")},
	})
	packet := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":         "budget",
		"scope_kind":    "project",
		"scope_id":      "den-memory",
		"packet_id":     "budget-test-packet",
		"budget_tokens": 30,
		"limit":         10,
	})
	for _, key := range []string{"packet_id", "packet_md", "root_matches", "included_nodes", "included_edges", "skipped", "warnings", "provenance", "audit"} {
		if _, ok := packet[key]; !ok {
			t.Fatalf("recall packet missing key %s: %#v", key, packet)
		}
	}
	foundTruncated := false
	for _, raw := range packet["skipped"].([]any) {
		if raw.(map[string]any)["reason"] == "token_budget_truncated" {
			foundTruncated = true
		}
	}
	if !foundTruncated {
		t.Fatalf("tiny budget did not produce truncation evidence: %#v", packet)
	}
	logs := getJSON(t, ts.URL+"/api/recall-logs?packet_id=budget-test-packet")
	items := logs["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("recall log lookup = %#v", logs)
	}
	log := items[0].(map[string]any)
	if int(log["estimated_tokens"].(float64)) <= 0 || int(log["estimated_tokens"].(float64)) == len(strings.Fields(packet["packet_md"].(string))) {
		t.Fatalf("estimated_tokens should be non-zero char/4-ish, not raw word count: log=%#v packet_md=%q", log, packet["packet_md"])
	}
}

func TestRecallDefaultsToBoundedBudgetWhenAdapterOmitsBudget(t *testing.T) {
	_, ts := newTestServer(t)
	createDirectEntry(t, ts.URL, map[string]any{
		"slug":                 "default-budget-entry",
		"title":                "Default budget entry",
		"summary":              "Default budget entry",
		"body_md":              "Default budget entry",
		"kind":                 "fact",
		"status":               "active",
		"curation_state":       "curated",
		"confidence":           "source_backed",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"discovery_scope":      "same_project",
		"source_refs":          []any{sourceRef("den_task", "2497", "default budget source")},
	})
	packet := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":      "default budget",
		"scope_kind": "project",
		"scope_id":   "den-memory",
		"packet_id":  "default-budget-packet",
	})
	if len(packet["included_nodes"].([]any)) == 0 {
		t.Fatalf("default budget recall omitted expected entry: %#v", packet)
	}
	logs := getJSON(t, ts.URL+"/api/recall-logs?packet_id=default-budget-packet")
	items := logs["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("recall log lookup = %#v", logs)
	}
	log := items[0].(map[string]any)
	if int(log["token_budget"].(float64)) != 3000 {
		t.Fatalf("omitted budget should log default bounded budget 3000: %#v", log)
	}
}

func TestDoctorDetectsFTSDrift(t *testing.T) {
	a, ts := newTestServer(t)
	entry := createDirectEntry(t, ts.URL, map[string]any{
		"slug":           "fts-entry-drift",
		"title":          "FTS entry drift",
		"summary":        "FTS entry drift",
		"body_md":        "FTS entry drift",
		"kind":           "fact",
		"status":         "active",
		"curation_state": "curated",
		"confidence":     "source_backed",
		"source_refs":    []any{sourceRef("den_task", "2496", "fts entry drift")},
	})
	candidate := postJSON(t, ts.URL+"/api/candidates", map[string]any{
		"title":         "FTS candidate drift",
		"summary":       "FTS candidate drift",
		"body_md":       "FTS candidate drift",
		"proposed_kind": "fact",
		"source_refs":   []any{sourceRef("den_task", "2496", "fts candidate drift")},
	})
	node := createTopicNode(t, ts.URL, "fts-node-drift", "FTS node drift", "FTS node drift")
	if _, err := a.store.DB().Exec(`DELETE FROM memory_entries_fts WHERE rowid=?`, entry["id"]); err != nil {
		t.Fatalf("delete entry fts: %v", err)
	}
	if _, err := a.store.DB().Exec(`DELETE FROM memory_candidates_fts WHERE rowid=?`, candidate["id"]); err != nil {
		t.Fatalf("delete candidate fts: %v", err)
	}
	if _, err := a.store.DB().Exec(`DELETE FROM topic_nodes_fts WHERE rowid=?`, node["id"]); err != nil {
		t.Fatalf("delete node fts: %v", err)
	}
	doctor := getJSON(t, ts.URL+"/api/doctor/report")
	kinds := map[string]bool{}
	for _, raw := range doctor["issues"].([]any) {
		issue := raw.(map[string]any)
		kinds[issue["kind"].(string)] = true
	}
	for _, want := range []string{"memory_entries_fts_drift", "memory_candidates_fts_drift", "topic_nodes_fts_drift"} {
		if !kinds[want] {
			t.Fatalf("doctor missing %s in %#v", want, doctor)
		}
	}
}

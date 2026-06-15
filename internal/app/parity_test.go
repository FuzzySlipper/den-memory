package app

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

func TestDirectEntrySourceRefsAndRuntimeContextMapping(t *testing.T) {
	_, ts := newTestServer(t)
	entry := createDirectEntry(t, ts.URL, map[string]any{
		"slug":            "direct-source-ref-parity",
		"title":           "Direct source ref parity",
		"summary":         "Direct entries preserve source refs",
		"body_md":         "Direct memory-entry creation must preserve source_refs like promoted candidates.",
		"kind":            "fact",
		"status":          "active",
		"curation_state":  "curated",
		"confidence":      "source_backed",
		"runtime_context": runtimeContext("hermes", "parity-runner", "runner", "den-memory"),
		"source_refs":     []any{sourceRef("den_task", "2491", "Task #2491 direct-entry source ref regression")},
	})
	if entry["created_by"] != "parity-runner" {
		t.Fatalf("created_by from runtime_context = %v", entry["created_by"])
	}

	refs := getJSON(t, ts.URL+"/api/source-refs?target_kind=memory_entry&target_id="+idString(entry["id"]))
	items := refs["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("source refs = %#v, want 1", refs)
	}
	ref := items[0].(map[string]any)
	if ref["source_id"] != "2491" || ref["created_by"] != "parity-runner" {
		t.Fatalf("source ref not attached with expected data: %#v", ref)
	}
	doctor := getJSON(t, ts.URL+"/api/doctor/report")
	if issueIDs := doctorIssueDetails(doctor, "missing_source_refs", "memory_entry_ids"); len(issueIDs) != 0 {
		t.Fatalf("direct entry with provided source refs reported missing_source_refs: %#v", doctor)
	}
}

func TestCaptureDecisionsAllLogEventsAndRuntimeContextContractShape(t *testing.T) {
	_, ts := newTestServer(t)

	captured := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime_context":     runtimeContext("hermes", "capture-runner", "runner", "den-memory"),
		"raw_content":         "Runtime context contract-shaped capture should become a project-scoped candidate.",
		"title":               "Runtime context capture",
		"summary":             "Contract-shaped capture",
		"event_kind":          "task_message",
		"proposed_scope_kind": "project",
		"proposed_scope_id":   "den-memory",
		"source_refs":         []any{sourceRef("den_message", "14951", "Planner handoff message")},
	})
	if captured["decision"] != "captured" {
		t.Fatalf("captured decision = %#v", captured)
	}
	candidate := captured["candidate"].(map[string]any)
	if candidate["scope_kind"] != "project" || candidate["scope_id"] != "den-memory" || candidate["created_by"] != "capture-runner" {
		t.Fatalf("runtime_context/proposed_scope not mapped onto candidate: %#v", candidate)
	}

	ignored := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime_context": runtimeContext("pi_crew", "worker-1", "worker", "den-memory"),
		"raw_content":     "worker metadata only",
		"title":           "Worker metadata only",
		"source_refs":     []any{sourceRef("den_task", "2491", "worker default capture policy")},
	})
	if ignored["decision"] != "ignored" || ignored["reason"] != "metadata_only" {
		t.Fatalf("ignored decision = %#v", ignored)
	}

	filtered := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime":     "hermes",
		"raw_text":    "api_key=not-a-real-secret-but-secret-like-marker",
		"title":       "Secret-like capture",
		"source_refs": []any{sourceRef("manual_note", "filtered", "filtered source")},
	})
	if filtered["decision"] != "filtered" {
		t.Fatalf("filtered decision = %#v", filtered)
	}

	errored := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime":      "hermes",
		"capture_mode": "invalid-mode",
		"raw_text":     "invalid mode should still log",
	})
	if errored["decision"] != "errored" {
		t.Fatalf("errored decision = %#v", errored)
	}

	summary := getJSON(t, ts.URL+"/api/observability/summary")
	counts := summary["counts"].(map[string]any)
	if counts["capture_events"].(float64) != 4 || counts["captured_events"].(float64) != 1 || counts["filtered_events"].(float64) != 1 || counts["errored_events"].(float64) != 1 {
		t.Fatalf("capture decision counts = %#v", counts)
	}
}

func TestCandidatePromotionPreservesSourceRefs(t *testing.T) {
	_, ts := newTestServer(t)
	capture := postJSON(t, ts.URL+"/api/capture", map[string]any{
		"runtime":              "hermes",
		"actor_identity":       "capture-agent",
		"raw_text":             "Promotion must preserve source refs on the curated entry.",
		"title":                "Promoted candidate source refs",
		"summary":              "Promoted source refs",
		"scope_kind":           "project",
		"scope_id":             "den-memory",
		"authority_scope_kind": "project",
		"authority_scope_id":   "den-memory",
		"source_refs":          []any{sourceRef("den_task", "2471", "Candidate source ref")},
	})
	candidate := capture["candidate"].(map[string]any)
	promote := postJSON(t, ts.URL+"/api/curation/candidates/"+idString(candidate["id"])+"/promote", map[string]any{
		"actor_identity": "curator",
		"reason":         "source-backed",
		"slug":           "promoted-source-ref-parity",
	})
	ids := promote["source_ref_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("promote source_ref_ids = %#v", promote)
	}
	entry := promote["memory_entry"].(map[string]any)
	refs := getJSON(t, ts.URL+"/api/source-refs?target_kind=memory_entry&target_id="+idString(entry["id"]))
	if len(refs["items"].([]any)) != 1 {
		t.Fatalf("promoted entry source refs = %#v", refs)
	}
}

func TestRecallScopeLabelsAndSkippedStatusReasons(t *testing.T) {
	_, ts := newTestServer(t)
	createDirectEntry(t, ts.URL, baseRecallEntry("authoritative-parity", "project", "den-memory", "project", "den-memory", "same_project", "active"))
	createDirectEntry(t, ts.URL, baseRecallEntry("discovered-parity", "project", "other-project", "project", "other-project", "global_discoverable", "active"))
	createDirectEntry(t, ts.URL, baseRecallEntry("out-of-scope-parity", "project", "other-project", "project", "other-project", "explicit_only", "active"))
	createDirectEntry(t, ts.URL, baseRecallEntry("archived-parity", "project", "den-memory", "project", "den-memory", "same_project", "archived"))

	packet := postJSON(t, ts.URL+"/api/recall", map[string]any{
		"query":          "parity",
		"scope_kind":     "project",
		"scope_id":       "den-memory",
		"actor_identity": "tester",
		"limit":          10,
	})
	interpretations := map[string]string{}
	for _, raw := range packet["included_nodes"].([]any) {
		node := raw.(map[string]any)
		interpretations[node["slug"].(string)] = node["interpretation"].(string)
	}
	if interpretations["authoritative-parity"] != "authoritative" {
		t.Fatalf("authoritative label missing: %#v", packet)
	}
	if interpretations["discovered-parity"] != "discovered_evidence" {
		t.Fatalf("discovered evidence label missing: %#v", packet)
	}
	skippedReasons := map[string]string{}
	for _, raw := range packet["skipped"].([]any) {
		item := raw.(map[string]any)
		skippedReasons[item["node_slug"].(string)] = item["reason"].(string)
	}
	if skippedReasons["out-of-scope-parity"] != "out_of_scope" || skippedReasons["archived-parity"] != "status:archived" {
		t.Fatalf("skipped reasons = %#v", skippedReasons)
	}
	if len(packet["warnings"].([]any)) == 0 {
		t.Fatalf("expected discovered-evidence warning: %#v", packet)
	}
}

func TestObservabilityFiltersApplyBeforeLimit(t *testing.T) {
	_, ts := newTestServer(t)
	postJSON(t, ts.URL+"/api/candidates", map[string]any{
		"title":         "Older target candidate",
		"summary":       "target source kind",
		"body_md":       "target source kind",
		"proposed_kind": "fact",
		"source_refs":   []any{sourceRef("den_task", "target", "target source")},
	})
	postJSON(t, ts.URL+"/api/candidates", map[string]any{
		"title":         "Newer non-target candidate",
		"summary":       "other source kind",
		"body_md":       "other source kind",
		"proposed_kind": "fact",
		"source_refs":   []any{sourceRef("manual_note", "other", "other source")},
	})
	items := getJSON(t, ts.URL+"/api/observability/pending-candidates?source_kind=den_task&limit=1")["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("filtered before limit items = %#v", items)
	}
	item := items[0].(map[string]any)
	if item["title"] != "Older target candidate" {
		t.Fatalf("limit applied before filter, got %#v", item)
	}
}

func TestAuditExportRecordTypesAndAuditorConstraints(t *testing.T) {
	_, ts := newTestServer(t)
	entry := createDirectEntry(t, ts.URL, map[string]any{
		"slug":           "audit-export-parity",
		"title":          "Audit export parity",
		"summary":        "Audit export includes required record types",
		"body_md":        "Audit export contains metadata, entries, source refs, and doctor records.",
		"kind":           "fact",
		"status":         "active",
		"curation_state": "curated",
		"confidence":     "source_backed",
		"source_refs":    []any{sourceRef("den_task", "2491", "Audit source")},
	})
	_ = entry
	postJSON(t, ts.URL+"/api/recall", map[string]any{"query": "audit", "scope_kind": "global", "actor_identity": "tester"})
	jsonl := getText(t, ts.URL+"/api/audit/export?format=jsonl&limit=20")
	records := parseJSONL(t, jsonl)
	types := map[string]bool{}
	for _, record := range records {
		types[record["record_type"].(string)] = true
		if record["record_type"] == "metadata" {
			constraints := record["auditor_constraints"].(map[string]any)
			if constraints["den_memory_provider_enabled"] != false || constraints["recall_tools_allowed"] != false || constraints["reads_via_audit_surfaces_only"] != true {
				t.Fatalf("bad auditor constraints: %#v", constraints)
			}
		}
	}
	for _, want := range []string{"metadata", "memory_entries", "source_refs", "recall_logs", "doctor"} {
		if !types[want] {
			t.Fatalf("audit export missing record_type %s in %s", want, jsonl)
		}
	}
}

func runtimeContext(runtime, agent, role, project string) map[string]any {
	return map[string]any{
		"runtime":           runtime,
		"agent_identity":    agent,
		"profile_id":        agent,
		"session_id":        "test-session",
		"session_key":       "task-2491",
		"session_kind":      "worker_assignment",
		"project_id":        project,
		"task_id":           2491,
		"role":              role,
		"audience":          []any{role},
		"mode":              "implementation",
		"source_surface":    "test",
		"agent_instance_id": nil,
	}
}

func sourceRef(kind, id, summary string) map[string]any {
	return map[string]any{
		"source_kind":         kind,
		"source_project_id":   "den-memory",
		"source_id":           id,
		"source_locator":      map[string]any{"id": id},
		"source_summary":      summary,
		"verification_status": "verified",
	}
}

func createDirectEntry(t *testing.T, baseURL string, payload map[string]any) map[string]any {
	t.Helper()
	if payload["kind"] == nil {
		payload["kind"] = "fact"
	}
	if payload["source_refs"] == nil {
		payload["source_refs"] = []any{sourceRef("manual_note", payload["slug"].(string), "test source")}
	}
	return postJSON(t, baseURL+"/api/memory-entries", payload)
}

func baseRecallEntry(slug, scopeKind, scopeID, authorityKind, authorityID, discovery, status string) map[string]any {
	return map[string]any{
		"slug":                 slug,
		"title":                strings.ReplaceAll(slug, "-", " "),
		"summary":              "parity recall seed " + slug,
		"body_md":              "parity recall seed " + slug,
		"kind":                 "fact",
		"layer":                "curated_fact",
		"scope_kind":           scopeKind,
		"scope_id":             scopeID,
		"authority_scope_kind": authorityKind,
		"authority_scope_id":   authorityID,
		"discovery_scope":      discovery,
		"claim_strength":       "assessment",
		"status":               status,
		"curation_state":       "curated",
		"confidence":           "source_backed",
		"source_refs":          []any{sourceRef("den_task", slug, "source for "+slug)},
	}
}

func doctorIssueDetails(doctor map[string]any, kind string, detailKey string) []any {
	for _, raw := range doctor["issues"].([]any) {
		issue := raw.(map[string]any)
		if issue["kind"] == kind {
			details := issue["details"].(map[string]any)
			if value, ok := details[detailKey].([]any); ok {
				return value
			}
		}
	}
	return nil
}

func parseJSONL(t *testing.T, text string) []map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(text))
	records := []map[string]any{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("jsonl decode %q: %v", line, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan jsonl: %v", err)
	}
	return records
}

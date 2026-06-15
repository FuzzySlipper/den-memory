package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"den-memories/internal/store"
)

var auditorConstraints = map[string]any{
	"den_memory_provider_enabled":   false,
	"recall_tools_allowed":          false,
	"reads_via_audit_surfaces_only": true,
}

func (h *Handler) doctorReport(w http.ResponseWriter, r *http.Request) {
	issues, err := h.doctorIssues()
	if err != nil {
		writeError(w, err)
		return
	}
	counts, err := h.summaryCounts()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"doctor_version": "v0-stub",
		"read_only":      true,
		"counts": map[string]any{
			"issues":            len(issues),
			"memory_entries":    counts["memory_entries"],
			"memory_candidates": counts["memory_candidates"],
			"source_refs":       counts["source_refs"],
			"capture_events":    counts["capture_events"],
			"curation_events":   counts["curation_events"],
			"recall_logs":       counts["recall_logs"],
		},
		"issues": issues,
	})
}

func (h *Handler) auditExport(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.auditSnapshot(r)
	if err != nil {
		writeError(w, err)
		return
	}
	format := queryDefault(r, "format", "jsonl")
	switch format {
	case "json":
		writeJSON(w, http.StatusOK, snapshot)
	case "markdown":
		writeText(w, http.StatusOK, "text/markdown", auditMarkdown(snapshot))
	case "jsonl":
		writeText(w, http.StatusOK, "application/x-ndjson", auditJSONL(snapshot))
	default:
		writeError(w, fmt.Errorf("%w: unsupported_audit_export_format", store.ErrInvalid))
	}
}

func (h *Handler) auditSnapshot(r *http.Request) (map[string]any, error) {
	clauses, args := timeFilter(r, "created_at")
	counts, err := h.summaryCounts()
	if err != nil {
		return nil, err
	}
	issues, err := h.doctorIssues()
	if err != nil {
		return nil, err
	}
	snapshot := map[string]any{
		"metadata": map[string]any{
			"format":              "den-memories-v0-audit-export",
			"recall_used":         false,
			"since":               r.URL.Query().Get("since"),
			"until":               r.URL.Query().Get("until"),
			"auditor_constraints": auditorConstraints,
		},
		"counts": counts,
		"doctor": map[string]any{"read_only": true, "issues": issues},
	}
	for _, table := range []string{"capture_events", "memory_candidates", "memory_entries", "source_refs", "curation_events", "recall_logs"} {
		rows, err := h.selectRows(r, table, clauses, args, intQuery(r, "limit", 500))
		if err != nil {
			return nil, err
		}
		snapshot[table] = rows
	}
	return snapshot, nil
}

func (h *Handler) selectRows(r *http.Request, table string, clauses []string, args []any, limit int) ([]store.Record, error) {
	sql := "SELECT * FROM " + table
	if len(clauses) > 0 {
		sql += " WHERE " + joinClauses(clauses)
	}
	sql += " ORDER BY id DESC LIMIT ?"
	callArgs := append([]any{}, args...)
	callArgs = append(callArgs, limit)
	return store.QueryAll(r.Context(), h.store.DB(), sql, callArgs...)
}

func (h *Handler) doctorIssues() ([]map[string]any, error) {
	issues := []map[string]any{}
	missingCandidates, err := idsFor(h, "SELECT id FROM memory_candidates WHERE source_refs_json='[]'")
	if err != nil {
		return nil, err
	}
	missingEntries, err := idsFor(h, "SELECT e.id FROM memory_entries e LEFT JOIN source_refs s ON s.target_kind='memory_entry' AND s.target_id=e.id WHERE s.id IS NULL")
	if err != nil {
		return nil, err
	}
	if len(missingCandidates)+len(missingEntries) > 0 {
		ids := append(append([]any{}, missingCandidates...), missingEntries...)
		issues = append(issues, issue("missing_source_refs", "warning", "Candidates or entries without source refs", ids, map[string]any{"candidate_ids": missingCandidates, "memory_entry_ids": missingEntries}))
	}
	broken, err := idsFor(h, "SELECT id FROM source_refs WHERE verification_status IN ('broken','unverified')")
	if err != nil {
		return nil, err
	}
	if len(broken) > 0 {
		issues = append(issues, issue("broken_or_unverified_source_refs", "warning", "Source refs are broken or unverified", broken, nil))
	}
	unscoped, err := idsFor(h, "SELECT id FROM memory_candidates WHERE scope_kind='global' AND scope_id IS NULL AND authority_scope_kind='global' AND authority_scope_id IS NULL")
	if err != nil {
		return nil, err
	}
	if len(unscoped) > 0 {
		issues = append(issues, issue("unscoped_candidates", "warning", "Candidates remain globally scoped without explicit review", unscoped, nil))
	}
	secretIDs, err := h.secretLikeIDs()
	if err != nil {
		return nil, err
	}
	if len(secretIDs) > 0 {
		issues = append(issues, map[string]any{"kind": "secret_like_strings", "severity": "critical", "summary": "Secret/token-like strings found in memory text", "count": len(secretIDs), "ids": secretIDs, "details": map[string]any{"markers_checked": secretMarkers}})
	}
	ftsIssues, err := h.ftsDriftIssues()
	if err != nil {
		return nil, err
	}
	issues = append(issues, ftsIssues...)
	return issues, nil
}

func idsFor(h *Handler, query string, args ...any) ([]any, error) {
	rows, err := store.QueryAll(nilContext(), h.store.DB(), query, args...)
	if err != nil {
		return nil, err
	}
	ids := []any{}
	for _, row := range rows {
		ids = append(ids, row["id"])
	}
	return ids, nil
}

func (h *Handler) secretLikeIDs() ([]any, error) {
	ids := []any{}
	for _, table := range []string{"memory_candidates", "memory_entries"} {
		rows, err := store.QueryAll(nilContext(), h.store.DB(), "SELECT id,title,summary,body_md FROM "+table)
		if err != nil {
			return nil, err
		}
		prefix := "candidate"
		if table == "memory_entries" {
			prefix = "entry"
		}
		for _, row := range rows {
			text := stringValue(row["title"], "") + " " + stringValue(row["summary"], "") + " " + stringValue(row["body_md"], "")
			if containsAny(text, secretMarkers) {
				ids = append(ids, fmt.Sprintf("%s:%v", prefix, row["id"]))
			}
		}
	}
	return ids, nil
}

func (h *Handler) ftsDriftIssues() ([]map[string]any, error) {
	checks := []struct {
		kind        string
		sourceTable string
		ftsTable    string
	}{
		{"memory_entries_fts_drift", "memory_entries", "memory_entries_fts"},
		{"memory_candidates_fts_drift", "memory_candidates", "memory_candidates_fts"},
		{"topic_nodes_fts_drift", "topic_nodes", "topic_nodes_fts"},
	}
	issues := []map[string]any{}
	for _, check := range checks {
		missing, err := idsFor(h, fmt.Sprintf("SELECT id FROM %s WHERE id NOT IN (SELECT rowid FROM %s)", check.sourceTable, check.ftsTable))
		if err != nil {
			return nil, err
		}
		extra, err := idsFor(h, fmt.Sprintf("SELECT rowid AS id FROM %s WHERE rowid NOT IN (SELECT id FROM %s)", check.ftsTable, check.sourceTable))
		if err != nil {
			return nil, err
		}
		if len(missing)+len(extra) == 0 {
			continue
		}
		ids := append(append([]any{}, missing...), extra...)
		issues = append(issues, issue(check.kind, "warning", "FTS index row ids drifted from source table", ids, map[string]any{"source_table": check.sourceTable, "fts_table": check.ftsTable, "missing_fts_row_ids": missing, "extra_fts_row_ids": extra}))
	}
	return issues, nil
}

func issue(kind string, severity string, summary string, ids []any, details map[string]any) map[string]any {
	if details == nil {
		details = map[string]any{}
	}
	return map[string]any{"kind": kind, "severity": severity, "summary": summary, "count": len(ids), "ids": ids, "details": details}
}

func auditMarkdown(snapshot map[string]any) string {
	lines := []string{"# Den Memories v0 audit export", "", "Recall used: `false`", "", "## Counts"}
	if counts, ok := snapshot["counts"].(map[string]int); ok {
		for key, value := range counts {
			lines = append(lines, fmt.Sprintf("- %s: %d", key, value))
		}
	}
	lines = append(lines, "", "## Doctor issues")
	doctor := snapshot["doctor"].(map[string]any)
	if issues, ok := doctor["issues"].([]map[string]any); ok && len(issues) > 0 {
		for _, item := range issues {
			lines = append(lines, fmt.Sprintf("- **%s** (%s): %s ids=%v", item["kind"], item["severity"], item["summary"], item["ids"]))
		}
	} else {
		lines = append(lines, "- none")
	}
	return strings.Join(lines, "\n") + "\n"
}

func auditJSONL(snapshot map[string]any) string {
	lines := []string{jsonLine(mergeMap(map[string]any{"record_type": "metadata"}, snapshot["metadata"].(map[string]any), map[string]any{"counts": snapshot["counts"]}))}
	for _, table := range []string{"capture_events", "memory_candidates", "memory_entries", "source_refs", "curation_events", "recall_logs"} {
		if rows, ok := snapshot[table].([]store.Record); ok {
			for _, row := range rows {
				lines = append(lines, jsonLine(mergeMap(map[string]any{"record_type": table}, row, nil)))
			}
		}
	}
	lines = append(lines, jsonLine(mergeMap(map[string]any{"record_type": "doctor"}, snapshot["doctor"].(map[string]any), nil)))
	return strings.Join(lines, "\n") + "\n"
}

func jsonLine(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func mergeMap(first map[string]any, second map[string]any, third map[string]any) map[string]any {
	result := map[string]any{}
	for _, source := range []map[string]any{first, second, third} {
		for key, value := range source {
			result[key] = value
		}
	}
	return result
}

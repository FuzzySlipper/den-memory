package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"den-memories/internal/store"
)

func (h *Handler) observabilitySummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.summaryCounts()
	if err != nil {
		writeError(w, err)
		return
	}
	recent := map[string]any{}
	for name, sql := range map[string]string{
		"capture_event_ids":     "SELECT id FROM capture_events ORDER BY id DESC LIMIT 10",
		"pending_candidate_ids": "SELECT id FROM memory_candidates WHERE status='pending' ORDER BY id DESC LIMIT 10",
		"curation_event_ids":    "SELECT id FROM curation_events ORDER BY id DESC LIMIT 10",
		"recall_log_ids":        "SELECT id FROM recall_logs ORDER BY id DESC LIMIT 10",
	} {
		rows, err := store.QueryAll(r.Context(), h.store.DB(), sql)
		if err != nil {
			writeError(w, err)
			return
		}
		ids := []any{}
		for _, row := range rows {
			ids = append(ids, row["id"])
		}
		recent[name] = ids
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": summary, "recent_ids": recent})
}

func (h *Handler) observabilityPendingCandidates(w http.ResponseWriter, r *http.Request) {
	clauses := []string{"status='pending'"}
	args := []any{}
	join := ""
	for _, key := range []string{"scope_kind", "scope_id"} {
		if value := r.URL.Query().Get(key); value != "" {
			clauses = append(clauses, key+"=?")
			args = append(args, value)
		}
	}
	if sourceKind := r.URL.Query().Get("source_kind"); sourceKind != "" {
		join = " JOIN json_each(memory_candidates.source_refs_json) source_ref"
		clauses = append(clauses, "json_extract(source_ref.value, '$.source_kind')=?")
		args = append(args, sourceKind)
	}
	sql := "SELECT DISTINCT memory_candidates.* FROM memory_candidates" + join + " WHERE " + joinClauses(clauses) + " ORDER BY memory_candidates.id DESC LIMIT ?"
	args = append(args, intQuery(r, "limit", 50))
	rows, err := store.QueryAll(r.Context(), h.store.DB(), sql, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(rows), "items": rows})
}

func (h *Handler) observabilityCurationTimeline(w http.ResponseWriter, r *http.Request) {
	clauses, args := timeFilter(r, "created_at")
	for _, key := range []string{"actor_identity", "action"} {
		if value := r.URL.Query().Get(key); value != "" {
			clauses = append(clauses, key+"=?")
			args = append(args, value)
		}
	}
	h.selectTable(w, r, "curation_events", clauses, args)
}

func (h *Handler) observabilityRecallLogs(w http.ResponseWriter, r *http.Request) {
	clauses, args := recallLogClauses(r)
	h.selectTable(w, r, "recall_logs", clauses, args)
}

func (h *Handler) listRecallLogs(w http.ResponseWriter, r *http.Request) {
	h.observabilityRecallLogs(w, r)
}

func (h *Handler) getRecallLog(w http.ResponseWriter, r *http.Request) {
	h.getEventRow(w, r, "recall_logs", "event_id")
}

func (h *Handler) getRecallLogByPacket(w http.ResponseWriter, r *http.Request) {
	row, err := store.QueryOne(r.Context(), h.store.DB(), `SELECT packet_json FROM recall_logs WHERE packet_id=?`, r.PathValue("packet_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row["packet"])
}

func (h *Handler) selectTable(w http.ResponseWriter, r *http.Request, table string, clauses []string, args []any) {
	sql := "SELECT * FROM " + table
	if len(clauses) > 0 {
		sql += " WHERE " + joinClauses(clauses)
	}
	sql += " ORDER BY id DESC LIMIT ?"
	args = append(args, intQuery(r, "limit", 50))
	rows, err := store.QueryAll(r.Context(), h.store.DB(), sql, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(rows), "items": rows})
}

func (h *Handler) summaryCounts() (map[string]int, error) {
	queries := map[string]string{
		"capture_events":         "SELECT COUNT(*) FROM capture_events",
		"captured_events":        "SELECT COUNT(*) FROM capture_events WHERE decision='captured'",
		"filtered_events":        "SELECT COUNT(*) FROM capture_events WHERE decision='filtered'",
		"errored_events":         "SELECT COUNT(*) FROM capture_events WHERE decision='errored'",
		"pending_candidates":     "SELECT COUNT(*) FROM memory_candidates WHERE status='pending'",
		"curation_events":        "SELECT COUNT(*) FROM curation_events",
		"recall_logs":            "SELECT COUNT(*) FROM recall_logs",
		"broken_source_refs":     "SELECT COUNT(*) FROM source_refs WHERE verification_status='broken'",
		"unverified_source_refs": "SELECT COUNT(*) FROM source_refs WHERE verification_status='unverified'",
		"memory_candidates":      "SELECT COUNT(*) FROM memory_candidates",
		"memory_entries":         "SELECT COUNT(*) FROM memory_entries",
		"source_refs":            "SELECT COUNT(*) FROM source_refs",
	}
	result := map[string]int{}
	for key, query := range queries {
		var count int
		if err := h.store.DB().QueryRow(query).Scan(&count); err != nil {
			return nil, fmt.Errorf("count %s: %w", key, err)
		}
		result[key] = count
	}
	return result, nil
}

func timeFilter(r *http.Request, column string) ([]string, []any) {
	clauses := []string{}
	args := []any{}
	if since := r.URL.Query().Get("since"); since != "" {
		clauses = append(clauses, column+" >= ?")
		args = append(args, since)
	}
	if until := r.URL.Query().Get("until"); until != "" {
		clauses = append(clauses, column+" <= ?")
		args = append(args, until)
	}
	return clauses, args
}

func recallLogClauses(r *http.Request) ([]string, []any) {
	clauses, args := timeFilter(r, "created_at")
	if packetID := r.URL.Query().Get("packet_id"); packetID != "" {
		clauses = append(clauses, "packet_id=?")
		args = append(args, packetID)
	}
	if actor := r.URL.Query().Get("actor"); actor != "" {
		clauses = append(clauses, "created_by=?")
		args = append(args, actor)
	}
	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		clauses = append(clauses, "(json_extract(request_json, '$.scope_id')=? OR json_extract(request_json, '$.project_id')=?)")
		args = append(args, projectID, projectID)
	}
	return clauses, args
}

func containsAny(text string, markers []string) bool {
	lower := strings.ToLower(text)
	for _, marker := range markers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

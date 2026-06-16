package httpapi

import (
	"context"
	"net/http"
	"strings"

	"den-memories/internal/store"
)

var captureModes = map[string]struct{}{
	"off":                   {},
	"metadata_only":         {},
	"permissive_candidates": {},
	"curated_manual_only":   {},
}

func (h *Handler) createCandidate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()

	id, err := insertCandidate(ctx, tx, payload)
	if err != nil {
		writeError(w, err)
		return
	}
	capturePayload := mergeRecord(store.Record(payload), map[string]any{
		"actor_identity":    stringField(payload, "created_by", "api"),
		"capture_policy_id": stringField(payload, "capture_policy_id", "api-candidate-create"),
	})
	captureID, err := logCapture(ctx, tx, capturePayload, "captured", stringField(payload, "reason", "candidate created"), []any{id}, -1, -1)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	row, err := getCandidateRecord(ctx, h.store.DB(), int(id))
	if err != nil {
		writeError(w, err)
		return
	}
	row["capture_event_id"] = captureID
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) capture(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()

	mode := stringField(payload, "capture_mode", "")
	if mode == "" {
		mode = defaultCaptureMode(stringField(payload, "runtime", "manual"), stringField(payload, "actor_role", ""))
	}
	if _, ok := captureModes[mode]; !ok {
		h.captureDecision(w, tx, payload, "errored", "invalid_capture_mode", nil, 0, 0)
		return
	}
	if payload["simulate_error"] == true || payload["force_error"] == true {
		h.captureDecision(w, tx, payload, "errored", stringField(payload, "reason", "simulated_capture_error"), nil, -1, -1)
		return
	}
	switch mode {
	case "off":
		h.captureDecision(w, tx, payload, "ignored", stringField(payload, "reason", "capture_mode_off"), nil, -1, -1)
		return
	case "metadata_only":
		h.captureDecision(w, tx, payload, "ignored", stringField(payload, "reason", "metadata_only"), nil, 0, 0)
		return
	case "curated_manual_only":
		h.captureDecision(w, tx, payload, "ignored", stringField(payload, "reason", "curated_manual_only"), nil, -1, -1)
		return
	}

	candidate := candidatePayloadFromCapture(payload)
	if reason := validateCaptureCandidate(candidate); reason != "" {
		h.captureDecision(w, tx, payload, "filtered", stringField(payload, "reason", reason), nil, -1, -1)
		return
	}
	id, err := insertCandidate(ctx, tx, candidate)
	if err != nil {
		h.captureDecision(w, tx, payload, "errored", err.Error(), nil, -1, -1)
		return
	}
	eventID, err := logCapture(ctx, tx, payload, "captured", stringField(payload, "reason", "captured candidate"), []any{id}, -1, -1)
	if err != nil {
		writeError(w, err)
		return
	}
	if !autoPromoteEligible(payload) {
		if err := tx.Commit(); err != nil {
			writeError(w, err)
			return
		}
		row, err := getCandidateRecord(ctx, h.store.DB(), int(id))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"decision":         "captured",
			"reason":           stringField(payload, "reason", "captured candidate"),
			"capture_event_id": eventID,
			"candidate_ids":    []int64{id},
			"candidate":        row,
		})
		return
	}

	// Auto-promote: non-worker/non-auditor agents get an immediate curated entry.
	entryID, err := insertMemoryEntryFromCandidate(ctx, tx, candidate, payload, stringField(payload, "actor_identity", "auto-promote"))
	if err != nil {
		h.captureDecision(w, tx, payload, "errored", err.Error(), nil, -1, -1)
		return
	}
	sourceRefIDs, err := attachCandidateSourceRefs(ctx, tx, candidate, entryID, stringField(payload, "actor_identity", "auto-promote"))
	if err != nil {
		writeError(w, err)
		return
	}
	afterCandidate, err := updateCandidateFields(ctx, tx, int(id), map[string]any{"status": "promoted", "updated_by": stringField(payload, "actor_identity", "auto-promote")})
	if err != nil {
		writeError(w, err)
		return
	}
	entry, err := store.QueryOne(ctx, tx, `SELECT * FROM memory_entries WHERE id=?`, entryID)
	if err != nil {
		writeError(w, err)
		return
	}
	curationAfter := map[string]any{"candidate": afterCandidate, "memory_entry": entry, "source_ref_ids": sourceRefIDs}
	curationEventID, err := logCuration(ctx, tx, "promote", stringField(payload, "actor_identity", "auto-promote"), stringField(payload, "reason", "auto-promoted from capture"), map[string]any{"candidate_id": id, "memory_entry_id": entryID}, candidate, curationAfter)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"decision":          "promoted",
		"reason":            stringField(payload, "reason", "auto-promoted from capture"),
		"capture_event_id":  eventID,
		"curation_event_id": curationEventID,
		"candidate":         afterCandidate,
		"memory_entry":      entry,
		"source_ref_ids":    sourceRefIDs,
	})
}

func (h *Handler) captureDecision(w http.ResponseWriter, tx store.Runner, payload map[string]any, decision string, reason string, ids []any, rawSize int, extractedSize int) {
	eventID, err := logCapture(nilContext(), tx, payload, decision, reason, ids, rawSize, extractedSize)
	if err != nil {
		writeError(w, err)
		return
	}
	if committer, ok := tx.(interface{ Commit() error }); ok {
		if err := committer.Commit(); err != nil {
			writeError(w, err)
			return
		}
	}
	if ids == nil {
		ids = []any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": decision, "reason": reason, "capture_event_id": eventID, "candidate_ids": ids})
}

func (h *Handler) listCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clauses := []string{}
	args := []any{}
	for _, key := range []string{"status", "scope_kind", "scope_id"} {
		if value := q.Get(key); value != "" {
			clauses = append(clauses, key+"=?")
			args = append(args, value)
		}
	}
	if actor := q.Get("actor"); actor != "" {
		clauses = append(clauses, "created_by=?")
		args = append(args, actor)
	}
	sql := "SELECT * FROM memory_candidates"
	if runtime := q.Get("runtime"); runtime != "" {
		sql += " WHERE id IN (SELECT value FROM capture_events, json_each(capture_events.candidate_ids_json) WHERE runtime=?)"
		args = append([]any{runtime}, args...)
		if len(clauses) > 0 {
			sql += " AND " + joinClauses(clauses)
		}
	} else if len(clauses) > 0 {
		sql += " WHERE " + joinClauses(clauses)
	}
	sql += " ORDER BY id DESC LIMIT ?"
	args = append(args, intQuery(r, "limit", 50))
	rows, err := store.QueryAll(r.Context(), h.store.DB(), sql, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "candidate_id")
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := getCandidateRecord(r.Context(), h.store.DB(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) updateCandidateStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathInt(r, "candidate_id")
	if err != nil {
		writeError(w, err)
		return
	}
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	before, err := getCandidateRecord(ctx, tx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	after, err := updateCandidateFields(ctx, tx, id, map[string]any{"status": payload["status"], "updated_by": stringField(payload, "actor_identity", "api")})
	if err != nil {
		writeError(w, err)
		return
	}
	action := "relabel"
	switch stringField(payload, "status", "") {
	case "claimed":
		action = "claim"
	case "rejected":
		action = "reject"
	case "superseded":
		action = "supersede"
	}
	if _, err := logCuration(ctx, tx, action, stringField(payload, "actor_identity", "api"), stringField(payload, "reason", "candidate status updated"), map[string]any{"candidate_id": id}, before, after); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, after)
}

func (h *Handler) searchCandidates(w http.ResponseWriter, r *http.Request) {
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := store.QueryAll(r.Context(), h.store.DB(), `SELECT c.* FROM memory_candidates_fts f JOIN memory_candidates c ON c.id=f.rowid WHERE memory_candidates_fts MATCH ? LIMIT ?`, payload["query"], intField(payload, "limit", 10))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func defaultCaptureMode(runtime string, actorRole string) string {
	role := strings.ToLower(actorRole)
	runtime = strings.ToLower(runtime)
	if role == "auditor" || runtime == "audit" {
		return "off"
	}
	if role == "worker" || runtime == "worker" || runtime == "subagent" {
		return "metadata_only"
	}
	return "permissive_candidates"
}

// autoPromoteEligible returns true for non-worker/non-auditor agents whose capture
// payload represents a real agent (not a worker, auditor, or debug-only runtime).
// Auto-promoted captures create an immediate active curated memory entry.
func autoPromoteEligible(payload map[string]any) bool {
	role := strings.ToLower(stringField(payload, "actor_role", ""))
	runtime := strings.ToLower(stringField(payload, "runtime", ""))
	if role == "auditor" || runtime == "audit" || runtime == "audit_service" {
		return false
	}
	if role == "worker" || runtime == "worker" || runtime == "subagent" {
		return false
	}
	if stringField(payload, "actor_identity", "") == "" {
		return false
	}
	return true
}

func joinClauses(clauses []string) string {
	return strings.Join(clauses, " AND ")
}

func nilContext() context.Context {
	return context.Background()
}

package httpapi

import (
	"fmt"
	"net/http"

	"den-memories/internal/store"
)

func (h *Handler) claimCandidate(w http.ResponseWriter, r *http.Request) {
	h.simpleCandidateAction(w, r, "claimed", "claim")
}

func (h *Handler) rejectCandidate(w http.ResponseWriter, r *http.Request) {
	h.simpleCandidateAction(w, r, "rejected", "reject")
}

func (h *Handler) simpleCandidateAction(w http.ResponseWriter, r *http.Request, status string, action string) {
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
	actor, reason, err := requireActorReason(payload)
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
	after, err := updateCandidateFields(ctx, tx, id, map[string]any{"status": status, "updated_by": actor})
	if err != nil {
		writeError(w, err)
		return
	}
	eventID, err := logCuration(ctx, tx, action, actor, reason, map[string]any{"candidate_id": id}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": after, "curation_event_id": eventID})
}

func (h *Handler) promoteCandidate(w http.ResponseWriter, r *http.Request) {
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
	actor, reason, err := requireActorReason(payload)
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
	status := stringValue(before["status"], "")
	if status != "pending" && status != "claimed" {
		writeError(w, fmt.Errorf("%w: candidate_not_promotable", store.ErrInvalid))
		return
	}
	entryID, err := insertMemoryEntryFromCandidate(ctx, tx, before, payload, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	sourceRefIDs, err := attachCandidateSourceRefs(ctx, tx, before, entryID, actor)
	if err != nil {
		writeError(w, err)
		return
	}
	afterCandidate, err := updateCandidateFields(ctx, tx, id, map[string]any{"status": "promoted", "updated_by": actor})
	if err != nil {
		writeError(w, err)
		return
	}
	entry, err := store.QueryOne(ctx, tx, `SELECT * FROM memory_entries WHERE id=?`, entryID)
	if err != nil {
		writeError(w, err)
		return
	}
	after := map[string]any{"candidate": afterCandidate, "memory_entry": entry, "source_ref_ids": sourceRefIDs}
	eventID, err := logCuration(ctx, tx, "promote", actor, reason, map[string]any{"candidate_id": id, "memory_entry_id": entryID}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": afterCandidate, "memory_entry": entry, "source_ref_ids": sourceRefIDs, "curation_event_id": eventID})
}

func (h *Handler) splitCandidate(w http.ResponseWriter, r *http.Request) {
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
	actor, reason, err := requireActorReason(payload)
	if err != nil {
		writeError(w, err)
		return
	}
	fragments := listField(payload, "fragments")
	if len(fragments) < 2 {
		writeError(w, fmt.Errorf("%w: at_least_two_fragments_required", store.ErrInvalid))
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
	newIDs := []int64{}
	splitCandidates := []store.Record{}
	for _, fragment := range fragments {
		part, ok := fragment.(map[string]any)
		if !ok {
			continue
		}
		candidate := mergeRecord(before, part)
		candidate["source_refs"] = valueOrDefault(part["source_refs"], before["source_refs"])
		candidate["created_by"] = actor
		candidate["updated_by"] = actor
		candidate["status"] = "pending"
		candidate["layer"] = "candidate"
		delete(candidate, "id")
		newID, err := insertCandidate(ctx, tx, candidate)
		if err != nil {
			writeError(w, err)
			return
		}
		newIDs = append(newIDs, newID)
		row, err := getCandidateRecord(ctx, tx, int(newID))
		if err != nil {
			writeError(w, err)
			return
		}
		splitCandidates = append(splitCandidates, row)
	}
	afterOriginal, err := updateCandidateFields(ctx, tx, id, map[string]any{"status": "needs_split", "updated_by": actor})
	if err != nil {
		writeError(w, err)
		return
	}
	after := map[string]any{"original": afterOriginal, "split_candidates": splitCandidates}
	eventID, err := logCuration(ctx, tx, "split", actor, reason, map[string]any{"candidate_id": id}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": afterOriginal, "split_candidate_ids": newIDs, "curation_event_id": eventID})
}

func (h *Handler) mergeCandidates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	actor, reason, err := requireActorReason(payload)
	if err != nil {
		writeError(w, err)
		return
	}
	ids := listField(payload, "candidate_ids")
	if len(ids) < 2 {
		writeError(w, fmt.Errorf("%w: at_least_two_candidates_required", store.ErrInvalid))
		return
	}
	tx, err := h.store.DB().BeginTx(ctx, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	defer tx.Rollback()
	before := []store.Record{}
	mergedSources := []any{}
	for _, rawID := range ids {
		cid := intFromAny(rawID, 0)
		item, err := getCandidateRecord(ctx, tx, cid)
		if err != nil {
			writeError(w, err)
			return
		}
		before = append(before, item)
		if sources, ok := item["source_refs"].([]any); ok {
			mergedSources = append(mergedSources, sources...)
		}
	}
	mergedPayload := map[string]any{
		"title":                stringField(payload, "title", joinedTitles(before)),
		"summary":              stringField(payload, "summary", joinedField(before, "summary", "\n")),
		"body_md":              stringField(payload, "body_md", joinedField(before, "body_md", "\n\n")),
		"proposed_kind":        stringField(payload, "proposed_kind", stringValue(before[0]["proposed_kind"], "fact")),
		"scope_kind":           stringField(payload, "scope_kind", stringValue(before[0]["scope_kind"], "global")),
		"scope_id":             valueOrDefault(payload["scope_id"], before[0]["scope_id"]),
		"authority_scope_kind": stringField(payload, "authority_scope_kind", stringValue(before[0]["authority_scope_kind"], "global")),
		"authority_scope_id":   valueOrDefault(payload["authority_scope_id"], before[0]["authority_scope_id"]),
		"discovery_scope":      stringField(payload, "discovery_scope", stringValue(before[0]["discovery_scope"], "explicit_only")),
		"claim_strength":       stringField(payload, "claim_strength", stringValue(before[0]["claim_strength"], "observation")),
		"source_refs":          mergedSources,
		"created_by":           actor,
		"updated_by":           actor,
	}
	mergedID, err := insertCandidate(ctx, tx, mergedPayload)
	if err != nil {
		writeError(w, err)
		return
	}
	updatedSources := []store.Record{}
	for _, rawID := range ids {
		updated, err := updateCandidateFields(ctx, tx, intFromAny(rawID, 0), map[string]any{"status": "needs_merge", "updated_by": actor})
		if err != nil {
			writeError(w, err)
			return
		}
		updatedSources = append(updatedSources, updated)
	}
	mergedCandidate, err := getCandidateRecord(ctx, tx, int(mergedID))
	if err != nil {
		writeError(w, err)
		return
	}
	after := map[string]any{"merged_candidate": mergedCandidate, "source_candidates": updatedSources}
	eventID, err := logCuration(ctx, tx, "merge", actor, reason, map[string]any{"candidate_id": mergedID}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"merged_candidate": mergedCandidate, "curation_event_id": eventID})
}

func (h *Handler) relabelCandidate(w http.ResponseWriter, r *http.Request) {
	h.patchCandidateCuration(w, r, "relabel", []string{"title", "summary", "body_md", "proposed_kind", "claim_strength", "audience"})
}

func (h *Handler) rescopeCandidate(w http.ResponseWriter, r *http.Request) {
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
	for _, key := range []string{"scope_kind", "authority_scope_kind", "discovery_scope", "claim_strength"} {
		if _, ok := payload[key]; !ok {
			writeError(w, fmt.Errorf("%w: missing %s", store.ErrInvalid, key))
			return
		}
	}
	actor, reason, err := requireActorReason(payload)
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
	updates := map[string]any{"updated_by": actor}
	for _, key := range []string{"scope_kind", "scope_id", "authority_scope_kind", "authority_scope_id", "discovery_scope", "claim_strength"} {
		if value, ok := payload[key]; ok {
			updates[key] = value
		}
	}
	after, err := updateCandidateFields(ctx, tx, id, updates)
	if err != nil {
		writeError(w, err)
		return
	}
	eventID, err := logCuration(ctx, tx, "rescope", actor, reason, map[string]any{"candidate_id": id}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": after, "curation_event_id": eventID})
}

func (h *Handler) patchCandidateCuration(w http.ResponseWriter, r *http.Request, action string, keys []string) {
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
	actor, reason, err := requireActorReason(payload)
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
	updates := map[string]any{"updated_by": actor}
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			updates[key] = value
		}
	}
	after, err := updateCandidateFields(ctx, tx, id, updates)
	if err != nil {
		writeError(w, err)
		return
	}
	eventID, err := logCuration(ctx, tx, action, actor, reason, map[string]any{"candidate_id": id}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidate": after, "curation_event_id": eventID})
}

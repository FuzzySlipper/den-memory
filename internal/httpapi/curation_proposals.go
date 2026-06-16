package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"den-memories/internal/store"
)

var allowedProposalKinds = map[string]struct{}{
	"promote_candidate":    {},
	"reject_candidate":     {},
	"split_candidate":      {},
	"merge_candidates":     {},
	"rescope_candidate":    {},
	"relabel_candidate":    {},
	"supersede_entry":      {},
	"knowledge_candidate":  {},
	"doc_update_candidate": {},
	"defer":                {},
}

var allowedProposalStatuses = map[string]struct{}{
	"proposed":    {},
	"needs_human": {},
	"applied":     {},
	"rejected":    {},
	"deferred":    {},
	"superseded":  {},
	"invalid":     {},
}

func (h *Handler) curationQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := queryDefault(r, "status", "pending,claimed")
	statuses := strings.Split(status, ",")
	placeholders := []string{}
	args := []any{}
	for _, item := range statuses {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		placeholders = append(placeholders, "?")
		args = append(args, item)
	}
	if len(placeholders) == 0 {
		placeholders = []string{"?"}
		args = append(args, "pending")
	}
	args = append(args, intQuery(r, "limit", 100))
	candidates, err := store.QueryAll(ctx, h.store.DB(), `SELECT * FROM memory_candidates WHERE status IN (`+strings.Join(placeholders, ",")+`) ORDER BY id ASC LIMIT ?`, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	allProposals, err := store.QueryAll(ctx, h.store.DB(), `SELECT * FROM curation_proposals ORDER BY id ASC`)
	if err != nil {
		writeError(w, err)
		return
	}
	items := []map[string]any{}
	for _, candidate := range candidates {
		cid := int64FromRecord(candidate, "id")
		proposals := proposalsForCandidate(allProposals, cid)
		queueState, nextAction := queueStateForCandidate(candidate, proposals)
		items = append(items, map[string]any{
			"candidate":             candidate,
			"source_refs":           valueOrDefault(candidate["source_refs"], []any{}),
			"proposals":             proposals,
			"queue_state":           queueState,
			"suggested_next_action": nextAction,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) createCurationProposal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	proposal, err := insertCurationProposal(ctx, h.store.DB(), payload)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

func (h *Handler) listCurationProposals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clauses := []string{}
	args := []any{}
	if status := q.Get("status"); status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, status)
	}
	if kind := q.Get("proposal_kind"); kind != "" {
		clauses = append(clauses, "proposal_kind=?")
		args = append(args, kind)
	}
	sql := "SELECT * FROM curation_proposals"
	if len(clauses) > 0 {
		sql += " WHERE " + joinClauses(clauses)
	}
	sql += " ORDER BY id DESC LIMIT ?"
	args = append(args, intQuery(r, "limit", 100))
	rows, err := store.QueryAll(r.Context(), h.store.DB(), sql, args...)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getCurationProposal(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt(r, "proposal_id")
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := getCurationProposalRecord(r.Context(), h.store.DB(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) applyCurationProposal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathInt(r, "proposal_id")
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
	proposal, err := getCurationProposalRecord(ctx, tx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	status := stringValue(proposal["status"], "")
	if status != "proposed" && status != "needs_human" {
		writeError(w, fmt.Errorf("%w: proposal_not_applicable", store.ErrInvalid))
		return
	}
	kind := stringValue(proposal["proposal_kind"], "")
	var result map[string]any
	switch kind {
	case "promote_candidate":
		result, err = h.applyPromoteCandidateProposal(ctx, tx, proposal, actor, reason)
	case "reject_candidate":
		result, err = h.applyRejectCandidateProposal(ctx, tx, proposal, actor, reason)
	default:
		err = fmt.Errorf("%w: proposal_kind_apply_not_supported", store.ErrInvalid)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	curationEventID := intFromAny(result["curation_event_id"], 0)
	memoryEntryID := 0
	if entry, ok := result["memory_entry"].(store.Record); ok {
		memoryEntryID = int(int64FromRecord(entry, "id"))
	} else if entry, ok := result["memory_entry"].(map[string]any); ok {
		memoryEntryID = intFromAny(entry["id"], 0)
	}
	if err := store.Exec(ctx, tx, `UPDATE curation_proposals SET status='applied', applied_by=?, applied_at=datetime('now'), applied_curation_event_id=?, applied_memory_entry_id=?, updated_at=datetime('now') WHERE id=?`, actor, nullableInt(curationEventID), nullableInt(memoryEntryID), id); err != nil {
		writeError(w, err)
		return
	}
	updated, err := getCurationProposalRecord(ctx, tx, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	response := map[string]any{"proposal": updated}
	for key, value := range result {
		response[key] = value
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) rejectCurationProposal(w http.ResponseWriter, r *http.Request) {
	h.markCurationProposal(w, r, "rejected")
}

func (h *Handler) deferCurationProposal(w http.ResponseWriter, r *http.Request) {
	h.markCurationProposal(w, r, "deferred")
}

func (h *Handler) markCurationProposal(w http.ResponseWriter, r *http.Request, status string) {
	id, err := pathInt(r, "proposal_id")
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
	if err := store.Exec(r.Context(), h.store.DB(), `UPDATE curation_proposals SET status=?, applied_by=?, applied_at=datetime('now'), reason=CASE WHEN reason='' THEN ? ELSE reason END, updated_at=datetime('now') WHERE id=? AND status IN ('proposed','needs_human')`, status, actor, reason, id); err != nil {
		writeError(w, err)
		return
	}
	proposal, err := getCurationProposalRecord(r.Context(), h.store.DB(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal})
}

func insertCurationProposal(ctx context.Context, r store.Runner, payload map[string]any) (store.Record, error) {
	kind := stringField(payload, "proposal_kind", "")
	if _, ok := allowedProposalKinds[kind]; !ok {
		return nil, fmt.Errorf("%w: invalid_proposal_kind", store.ErrInvalid)
	}
	status := stringField(payload, "status", "proposed")
	if _, ok := allowedProposalStatuses[status]; !ok {
		return nil, fmt.Errorf("%w: invalid_proposal_status", store.ErrInvalid)
	}
	if status != "proposed" && status != "needs_human" {
		return nil, fmt.Errorf("%w: proposal_create_status_must_be_pending_decision", store.ErrInvalid)
	}
	proposer := stringField(payload, "proposer_identity", "")
	if proposer == "" {
		return nil, fmt.Errorf("%w: proposer_identity_required", store.ErrInvalid)
	}
	if strings.TrimSpace(stringField(payload, "reason", "")) == "" {
		return nil, fmt.Errorf("%w: reason_required", store.ErrInvalid)
	}
	candidateIDs := candidateIDsFromProposalPayload(payload)
	if len(candidateIDs) == 0 && strings.Contains(kind, "candidate") {
		return nil, fmt.Errorf("%w: candidate_ids_required", store.ErrInvalid)
	}
	for _, cid := range candidateIDs {
		if _, err := getCandidateRecord(ctx, r, cid); err != nil {
			return nil, err
		}
	}
	if payload["proposed_action"] == nil {
		payload["proposed_action"] = defaultProposedAction(kind, candidateIDs, payload)
	}
	id, err := store.Insert(ctx, r, `INSERT INTO curation_proposals(proposal_kind,status,candidate_ids_json,target_memory_entry_id,proposed_action_json,proposed_entry_json,proposed_graph_json,source_refs_json,evidence_refs_json,existing_context_json,proposer_identity,proposer_kind,confidence,reason,model_metadata_json)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		kind, status, mustJSON(intsToAny(candidateIDs), []any{}), valueOrNil(payload["target_memory_entry_id"]), mustJSONObject(payload["proposed_action"]), mustJSONObject(payload["proposed_entry"]), mustJSONObject(payload["proposed_graph"]), mustJSON(payload["source_refs"], []any{}), mustJSON(payload["evidence_refs"], []any{}), mustJSONObject(payload["existing_context"]), proposer, stringField(payload, "proposer_kind", "agent"), stringField(payload, "confidence", "unverified"), stringField(payload, "reason", ""), mustJSONObject(payload["model_metadata"]))
	if err != nil {
		return nil, err
	}
	return getCurationProposalRecord(ctx, r, int(id))
}

func getCurationProposalRecord(ctx context.Context, r store.Runner, proposalID int) (store.Record, error) {
	return store.QueryOne(ctx, r, `SELECT * FROM curation_proposals WHERE id=?`, proposalID)
}

func candidateIDsFromProposalPayload(payload map[string]any) []int {
	ids := []int{}
	if id := intField(payload, "candidate_id", 0); id > 0 {
		ids = append(ids, id)
	}
	for _, raw := range listField(payload, "candidate_ids") {
		id := intFromAny(raw, 0)
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return uniqueInts(ids)
}

func candidateIDsFromProposalRecord(proposal store.Record) []int {
	ids := []int{}
	for _, raw := range anySlice(proposal["candidate_ids"]) {
		id := intFromAny(raw, 0)
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return uniqueInts(ids)
}

func proposalsForCandidate(proposals []store.Record, candidateID int64) []store.Record {
	matches := []store.Record{}
	for _, proposal := range proposals {
		for _, cid := range candidateIDsFromProposalRecord(proposal) {
			if int64(cid) == candidateID {
				matches = append(matches, proposal)
				break
			}
		}
	}
	return matches
}

func queueStateForCandidate(candidate store.Record, proposals []store.Record) (string, string) {
	status := stringValue(candidate["status"], "")
	if status == "claimed" {
		return "claimed", "review_claim"
	}
	active := 0
	for _, proposal := range proposals {
		switch stringValue(proposal["status"], "") {
		case "proposed", "needs_human":
			active++
		}
	}
	if active > 0 {
		return "needs_decision", "review_proposal"
	}
	return "needs_proposal", "run_curator"
}

func (h *Handler) applyPromoteCandidateProposal(ctx context.Context, tx store.Runner, proposal store.Record, actor string, reason string) (map[string]any, error) {
	ids := candidateIDsFromProposalRecord(proposal)
	if len(ids) != 1 {
		return nil, fmt.Errorf("%w: promote_candidate_requires_one_candidate", store.ErrInvalid)
	}
	payload := proposalPayload(proposal)
	before, err := getCandidateRecord(ctx, tx, ids[0])
	if err != nil {
		return nil, err
	}
	status := stringValue(before["status"], "")
	if status != "pending" && status != "claimed" {
		return nil, fmt.Errorf("%w: candidate_not_promotable", store.ErrInvalid)
	}
	entryID, err := insertMemoryEntryFromCandidate(ctx, tx, before, payload, actor)
	if err != nil {
		return nil, err
	}
	sourceRefIDs, err := attachCandidateSourceRefs(ctx, tx, before, entryID, actor)
	if err != nil {
		return nil, err
	}
	afterCandidate, err := updateCandidateFields(ctx, tx, ids[0], map[string]any{"status": "promoted", "updated_by": actor})
	if err != nil {
		return nil, err
	}
	entry, err := store.QueryOne(ctx, tx, `SELECT * FROM memory_entries WHERE id=?`, entryID)
	if err != nil {
		return nil, err
	}
	after := map[string]any{"candidate": afterCandidate, "memory_entry": entry, "source_ref_ids": sourceRefIDs}
	eventID, err := logCuration(ctx, tx, "promote", actor, reason, map[string]any{"candidate_id": ids[0], "memory_entry_id": entryID}, before, after)
	if err != nil {
		return nil, err
	}
	return map[string]any{"candidate": afterCandidate, "memory_entry": entry, "source_ref_ids": sourceRefIDs, "curation_event_id": eventID}, nil
}

func (h *Handler) applyRejectCandidateProposal(ctx context.Context, tx store.Runner, proposal store.Record, actor string, reason string) (map[string]any, error) {
	ids := candidateIDsFromProposalRecord(proposal)
	if len(ids) != 1 {
		return nil, fmt.Errorf("%w: reject_candidate_requires_one_candidate", store.ErrInvalid)
	}
	before, err := getCandidateRecord(ctx, tx, ids[0])
	if err != nil {
		return nil, err
	}
	after, err := updateCandidateFields(ctx, tx, ids[0], map[string]any{"status": "rejected", "updated_by": actor})
	if err != nil {
		return nil, err
	}
	eventID, err := logCuration(ctx, tx, "reject", actor, reason, map[string]any{"candidate_id": ids[0]}, before, after)
	if err != nil {
		return nil, err
	}
	return map[string]any{"candidate": after, "curation_event_id": eventID}, nil
}

func proposalPayload(proposal store.Record) map[string]any {
	action, _ := proposal["proposed_action"].(map[string]any)
	payload, _ := action["payload"].(map[string]any)
	entry, _ := proposal["proposed_entry"].(map[string]any)
	merged := map[string]any{}
	for key, value := range entry {
		merged[key] = value
	}
	for key, value := range payload {
		merged[key] = value
	}
	return merged
}

func defaultProposedAction(kind string, candidateIDs []int, payload map[string]any) map[string]any {
	action := strings.TrimSuffix(kind, "s")
	result := map[string]any{"action": kind}
	if len(candidateIDs) == 1 {
		result["candidate_id"] = candidateIDs[0]
	}
	if len(candidateIDs) > 0 {
		result["candidate_ids"] = intsToAny(candidateIDs)
	}
	if payload["proposed_entry"] != nil {
		result["payload"] = payload["proposed_entry"]
	}
	if action != kind {
		result["action_alias"] = action
	}
	return result
}

func anySlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func intsToAny(ids []int) []any {
	items := make([]any, 0, len(ids))
	for _, id := range ids {
		items = append(items, id)
	}
	return items
}

func uniqueInts(ids []int) []int {
	seen := map[int]struct{}{}
	result := []int{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

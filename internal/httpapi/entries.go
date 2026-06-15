package httpapi

import (
	"fmt"
	"net/http"

	"den-memories/internal/store"
)

func (h *Handler) createEntry(w http.ResponseWriter, r *http.Request) {
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

	id, err := store.Insert(ctx, tx, `INSERT INTO memory_entries(slug,title,summary,body_md,content_format,kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,status,curation_state,confidence,stability,audience_json,tags_json,created_by,updated_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		payload["slug"], payload["title"], stringField(payload, "summary", ""), stringField(payload, "body_md", ""),
		stringField(payload, "content_format", "markdown"), payload["kind"], stringField(payload, "layer", "curated_fact"),
		stringField(payload, "scope_kind", "global"), payload["scope_id"], stringField(payload, "authority_scope_kind", "global"),
		payload["authority_scope_id"], stringField(payload, "discovery_scope", "explicit_only"), stringField(payload, "claim_strength", "observation"),
		stringField(payload, "status", "draft"), stringField(payload, "curation_state", "candidate"), stringField(payload, "confidence", "unverified"),
		stringField(payload, "stability", "evolving"), mustJSON(payload["audience"], []any{}), mustJSON(payload["tags"], []any{}),
		stringField(payload, "created_by", "api"), stringField(payload, "updated_by", stringField(payload, "created_by", "api")))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := refreshEntryFTS(ctx, tx, id, payload); err != nil {
		writeError(w, err)
		return
	}
	if sources := listField(payload, "source_refs"); len(sources) > 0 {
		if _, err := attachSourceRefs(ctx, tx, sources, "memory_entry", id, stringField(payload, "created_by", "api"), stringField(payload, "summary", ""), stringField(payload, "slug", fmt.Sprint(id))); err != nil {
			writeError(w, err)
			return
		}
	}
	action := "relabel"
	if stringField(payload, "curation_state", "") == "curated" {
		action = "promote"
	}
	if _, err := logCuration(ctx, tx, action, stringField(payload, "created_by", "api"), "entry created", map[string]any{"memory_entry_id": id}, nil, payload); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	row, err := store.QueryOne(ctx, h.store.DB(), `SELECT * FROM memory_entries WHERE id=?`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) getEntry(w http.ResponseWriter, r *http.Request) {
	row, err := getEntryRecord(r.Context(), h.store.DB(), r.PathValue("slug"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) updateEntry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")
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

	before, err := getEntryRecord(ctx, tx, slug)
	if err != nil {
		writeError(w, err)
		return
	}
	merged := mergeRecord(before, payload)
	merged["slug"] = slug
	if err := store.Exec(ctx, tx, `UPDATE memory_entries SET title=?,summary=?,body_md=?,layer=?,scope_kind=?,scope_id=?,authority_scope_kind=?,authority_scope_id=?,discovery_scope=?,claim_strength=?,status=?,curation_state=?,confidence=?,stability=?,audience_json=?,tags_json=?,updated_by=?,updated_at=datetime('now') WHERE slug=?`,
		merged["title"], stringValue(merged["summary"], ""), stringValue(merged["body_md"], ""), stringValue(merged["layer"], "curated_fact"),
		stringValue(merged["scope_kind"], "global"), merged["scope_id"], stringValue(merged["authority_scope_kind"], "global"),
		merged["authority_scope_id"], stringValue(merged["discovery_scope"], "explicit_only"), stringValue(merged["claim_strength"], "observation"),
		stringValue(merged["status"], "draft"), stringValue(merged["curation_state"], "candidate"), stringValue(merged["confidence"], "unverified"),
		stringValue(merged["stability"], "evolving"), mustJSON(merged["audience"], []any{}), mustJSON(merged["tags"], []any{}),
		stringField(payload, "updated_by", "api"), slug); err != nil {
		writeError(w, err)
		return
	}
	id := int64FromRecord(before, "id")
	if err := refreshEntryFTS(ctx, tx, id, merged); err != nil {
		writeError(w, err)
		return
	}
	if _, err := logCuration(ctx, tx, "relabel", stringField(payload, "updated_by", "api"), stringField(payload, "reason", "entry updated"), map[string]any{"memory_entry_id": id}, before, merged); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	row, err := getEntryRecord(ctx, h.store.DB(), slug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) archiveEntry(w http.ResponseWriter, r *http.Request) {
	h.statusEntry(w, r, "archived")
}

func (h *Handler) supersedeEntry(w http.ResponseWriter, r *http.Request) {
	h.statusEntry(w, r, "superseded")
}

func (h *Handler) statusEntry(w http.ResponseWriter, r *http.Request, status string) {
	payload, _ := readPayload(r)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["status"] = status
	payload["updated_by"] = stringField(payload, "actor_identity", "api")
	payload["reason"] = stringField(payload, "reason", status)
	r.Body = bodyFromMap(payload)
	h.updateEntry(w, r)
}

func (h *Handler) searchEntries(w http.ResponseWriter, r *http.Request) {
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := store.QueryAll(r.Context(), h.store.DB(), `SELECT e.* FROM memory_entries_fts f JOIN memory_entries e ON e.id=f.rowid WHERE memory_entries_fts MATCH ? LIMIT ?`, payload["query"], intField(payload, "limit", 10))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func mergeRecord(base store.Record, updates map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range updates {
		result[key] = value
	}
	return result
}

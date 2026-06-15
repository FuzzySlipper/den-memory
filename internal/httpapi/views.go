package httpapi

import (
	"net/http"

	"den-memories/internal/store"
)

func (h *Handler) createView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := store.Insert(ctx, h.store.DB(), `INSERT INTO topic_views(slug,title,root_node_id,view_type,audience_json,mode,include_relations_json,exclude_relations_json,max_depth,token_budget_hint,ordering_policy,status,created_by,updated_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		payload["slug"], payload["title"], payload["root_node_id"], payload["view_type"], mustJSON(payload["audience"], []any{}),
		stringField(payload, "mode", "general"), mustJSON(payload["include_relations"], []any{}), mustJSON(payload["exclude_relations"], []any{}),
		intField(payload, "max_depth", 2), intField(payload, "token_budget_hint", 3000), stringField(payload, "ordering_policy", "core_first_then_risks"),
		stringField(payload, "status", "active"), stringField(payload, "created_by", "api"), stringField(payload, "updated_by", stringField(payload, "created_by", "api")))
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := store.QueryOne(ctx, h.store.DB(), `SELECT * FROM topic_views WHERE id=?`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) listViews(w http.ResponseWriter, r *http.Request) {
	rows, err := store.QueryAll(r.Context(), h.store.DB(), `SELECT * FROM topic_views ORDER BY id DESC LIMIT ?`, intQuery(r, "limit", 50))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getView(w http.ResponseWriter, r *http.Request) {
	row, err := getViewRecord(r.Context(), h.store.DB(), r.PathValue("slug"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) updateView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	current, err := getViewRecord(ctx, h.store.DB(), slug)
	if err != nil {
		writeError(w, err)
		return
	}
	merged := mergeRecord(current, payload)
	if err := store.Exec(ctx, h.store.DB(), `UPDATE topic_views SET title=?,mode=?,max_depth=?,token_budget_hint=?,ordering_policy=?,status=?,updated_by=?,updated_at=datetime('now') WHERE slug=?`,
		merged["title"], stringValue(merged["mode"], "general"), intFromAny(merged["max_depth"], 2), intFromAny(merged["token_budget_hint"], 3000),
		stringValue(merged["ordering_policy"], "core_first_then_risks"), stringValue(merged["status"], "active"), stringField(payload, "updated_by", "api"), slug); err != nil {
		writeError(w, err)
		return
	}
	row, err := getViewRecord(ctx, h.store.DB(), slug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

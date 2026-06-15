package httpapi

import (
	"net/http"

	"den-memories/internal/store"
)

func (h *Handler) createNode(w http.ResponseWriter, r *http.Request) {
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
	id, err := store.Insert(ctx, tx, `INSERT INTO topic_nodes(slug,title,node_type,content_ref_kind,content_ref_id,memory_entry_id,summary,layer,canonicality,importance,stability,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,audience_json,default_unroll_policy_json,created_by,updated_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		payload["slug"], payload["title"], payload["node_type"], payload["content_ref_kind"], payload["content_ref_id"], payload["memory_entry_id"],
		stringField(payload, "summary", ""), stringField(payload, "layer", "topic_scene"), stringField(payload, "canonicality", "preferred"),
		stringField(payload, "importance", "important"), stringField(payload, "stability", "evolving"), stringField(payload, "scope_kind", "global"),
		payload["scope_id"], stringField(payload, "authority_scope_kind", "global"), payload["authority_scope_id"],
		stringField(payload, "discovery_scope", "explicit_only"), stringField(payload, "claim_strength", "observation"),
		mustJSON(payload["audience"], []any{}), mustJSON(payload["default_unroll_policy"], nil),
		stringField(payload, "created_by", "api"), stringField(payload, "updated_by", stringField(payload, "created_by", "api")))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := refreshNodeFTS(ctx, tx, id, payload); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	row, err := store.QueryOne(ctx, h.store.DB(), `SELECT * FROM topic_nodes WHERE id=?`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) listNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := store.QueryAll(r.Context(), h.store.DB(), `SELECT * FROM topic_nodes ORDER BY id DESC LIMIT ?`, intQuery(r, "limit", 50))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getNode(w http.ResponseWriter, r *http.Request) {
	row, err := getNodeRecord(r.Context(), h.store.DB(), r.PathValue("slug"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) updateNode(w http.ResponseWriter, r *http.Request) {
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
	before, err := getNodeRecord(ctx, tx, slug)
	if err != nil {
		writeError(w, err)
		return
	}
	merged := mergeRecord(before, payload)
	if err := store.Exec(ctx, tx, `UPDATE topic_nodes SET title=?,summary=?,claim_strength=?,discovery_scope=?,updated_by=?,updated_at=datetime('now') WHERE slug=?`,
		merged["title"], stringValue(merged["summary"], ""), stringValue(merged["claim_strength"], "observation"),
		stringValue(merged["discovery_scope"], "explicit_only"), stringField(payload, "updated_by", "api"), slug); err != nil {
		writeError(w, err)
		return
	}
	id := int64FromRecord(before, "id")
	if err := refreshNodeFTS(ctx, tx, id, merged); err != nil {
		writeError(w, err)
		return
	}
	if _, err := logCuration(ctx, tx, "relabel", stringField(payload, "updated_by", "api"), stringField(payload, "reason", "node updated"), map[string]any{"node_id": id}, before, merged); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	row, err := getNodeRecord(ctx, h.store.DB(), slug)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) searchNodes(w http.ResponseWriter, r *http.Request) {
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := store.QueryAll(r.Context(), h.store.DB(), `SELECT n.* FROM topic_nodes_fts f JOIN topic_nodes n ON n.id=f.rowid WHERE topic_nodes_fts MATCH ? LIMIT ?`, payload["query"], intField(payload, "limit", 10))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) createEdge(w http.ResponseWriter, r *http.Request) {
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
	id, err := store.Insert(ctx, tx, `INSERT INTO topic_edges(from_node_id,to_node_id,relation,weight,priority,condition_json,condition_hash,audience_json,status,notes,created_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		payload["from_node_id"], payload["to_node_id"], payload["relation"], valueOrDefault(payload["weight"], 1.0),
		valueOrDefault(payload["priority"], 0), mustJSON(payload["condition"], nil), stringField(payload, "condition_hash", ""),
		mustJSON(payload["audience"], []any{}), stringField(payload, "status", "active"), stringField(payload, "notes", ""),
		stringField(payload, "created_by", "api"))
	if err != nil {
		writeError(w, err)
		return
	}
	item, err := store.QueryOne(ctx, tx, `SELECT * FROM topic_edges WHERE id=?`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := logCuration(ctx, tx, "link", stringField(payload, "created_by", "api"), stringField(payload, "reason", "edge created"), map[string]any{"edge_id": id}, nil, item); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *Handler) deleteEdge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathInt(r, "edge_id")
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
	before, err := store.QueryOne(ctx, tx, `SELECT * FROM topic_edges WHERE id=?`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := logCuration(ctx, tx, "unlink", queryDefault(r, "actor_identity", "api"), queryDefault(r, "reason", "edge deleted"), map[string]any{"edge_id": id}, before, nil); err != nil {
		writeError(w, err)
		return
	}
	if err := store.Exec(ctx, tx, `DELETE FROM topic_edges WHERE id=?`, id); err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "edge_id": id})
}

func (h *Handler) neighbors(w http.ResponseWriter, r *http.Request) {
	node, err := getNodeRecord(r.Context(), h.store.DB(), r.PathValue("slug"))
	if err != nil {
		writeError(w, err)
		return
	}
	rows, err := store.QueryAll(r.Context(), h.store.DB(), `SELECT e.*, n.slug AS to_slug, n.title AS to_title FROM topic_edges e JOIN topic_nodes n ON n.id=e.to_node_id WHERE e.from_node_id=? ORDER BY e.priority DESC, e.id`, node["id"])
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

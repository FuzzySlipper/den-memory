package httpapi

import (
	"net/http"

	"den-memories/internal/store"
)

func (h *Handler) attachSourceRef(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := store.Insert(ctx, h.store.DB(), `INSERT INTO source_refs(target_kind,target_id,source_kind,source_project_id,source_id,source_locator_json,source_summary,observed_at,verified_at,verification_status,created_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		payload["target_kind"], payload["target_id"], payload["source_kind"], payload["source_project_id"], payload["source_id"],
		mustJSONObject(payload["source_locator"]), stringField(payload, "source_summary", ""), payload["observed_at"], payload["verified_at"],
		stringField(payload, "verification_status", "unverified"), stringField(payload, "created_by", "api"))
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := store.QueryOne(ctx, h.store.DB(), `SELECT * FROM source_refs WHERE id=?`, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) listSourceRefs(w http.ResponseWriter, r *http.Request) {
	clauses := []string{}
	args := []any{}
	for _, key := range []string{"target_kind", "verification_status"} {
		if value := r.URL.Query().Get(key); value != "" {
			clauses = append(clauses, key+"=?")
			args = append(args, value)
		}
	}
	if value := r.URL.Query().Get("target_id"); value != "" {
		clauses = append(clauses, "target_id=?")
		args = append(args, value)
	}
	sql := "SELECT * FROM source_refs"
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
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

package httpapi

import (
	"net/http"

	"den-memories/internal/store"
)

func (h *Handler) listCaptureEvents(w http.ResponseWriter, r *http.Request) {
	h.listEventTable(w, r, "capture_events")
}

func (h *Handler) recentCaptureEvents(w http.ResponseWriter, r *http.Request) {
	h.listEventTable(w, r, "capture_events")
}

func (h *Handler) getCaptureEvent(w http.ResponseWriter, r *http.Request) {
	h.getEventRow(w, r, "capture_events", "event_id")
}

func (h *Handler) listCurationEvents(w http.ResponseWriter, r *http.Request) {
	clauses := []string{}
	args := []any{}
	for _, key := range []string{"action", "actor_identity"} {
		if value := r.URL.Query().Get(key); value != "" {
			clauses = append(clauses, key+"=?")
			args = append(args, value)
		}
	}
	if value := r.URL.Query().Get("reason_contains"); value != "" {
		clauses = append(clauses, "reason LIKE ?")
		args = append(args, "%"+value+"%")
	}
	sql := "SELECT * FROM curation_events"
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

func (h *Handler) getCurationEvent(w http.ResponseWriter, r *http.Request) {
	h.getEventRow(w, r, "curation_events", "event_id")
}

func (h *Handler) listEventTable(w http.ResponseWriter, r *http.Request, table string) {
	rows, err := store.QueryAll(r.Context(), h.store.DB(), "SELECT * FROM "+table+" ORDER BY id DESC LIMIT ?", intQuery(r, "limit", 50))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getEventRow(w http.ResponseWriter, r *http.Request, table string, pathKey string) {
	id, err := pathInt(r, pathKey)
	if err != nil {
		writeError(w, err)
		return
	}
	row, err := store.QueryOne(r.Context(), h.store.DB(), "SELECT * FROM "+table+" WHERE id=?", id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

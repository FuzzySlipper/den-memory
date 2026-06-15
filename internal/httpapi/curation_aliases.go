package httpapi

import (
	"net/http"
	"strings"

	"den-memories/internal/store"
)

func (h *Handler) curationSupersedeEntry(w http.ResponseWriter, r *http.Request) {
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
	slug := r.PathValue("slug")
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
	if err := store.Exec(ctx, tx, `UPDATE memory_entries SET status='superseded', updated_by=?, updated_at=datetime('now') WHERE slug=?`, actor, slug); err != nil {
		writeError(w, err)
		return
	}
	after, err := getEntryRecord(ctx, tx, slug)
	if err != nil {
		writeError(w, err)
		return
	}
	eventID, err := logCuration(ctx, tx, "supersede", actor, reason, map[string]any{"memory_entry_id": after["id"]}, before, after)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memory_entry": after, "curation_event_id": eventID})
}

func (h *Handler) curationLinkEdge(w http.ResponseWriter, r *http.Request) {
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
	payload["created_by"] = actor
	payload["reason"] = reason
	r.Body = bodyFromMap(payload)
	h.createEdge(w, r)
}

func (h *Handler) curationUnlinkEdge(w http.ResponseWriter, r *http.Request) {
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
	q := r.URL.Query()
	q.Set("actor_identity", actor)
	q.Set("reason", reason)
	r.URL.RawQuery = q.Encode()
	h.deleteEdge(w, r)
}

func joinedTitles(items []store.Record) string {
	titles := make([]string, 0, len(items))
	for _, item := range items {
		titles = append(titles, stringValue(item["title"], ""))
	}
	return strings.Join(titles, " / ")
}

func joinedField(items []store.Record, field string, sep string) string {
	values := []string{}
	for _, item := range items {
		value := stringValue(item[field], "")
		if value != "" {
			values = append(values, value)
		}
	}
	return strings.Join(values, sep)
}

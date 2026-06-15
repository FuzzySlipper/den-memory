package httpapi

import (
	"net/http"
	"sort"
)

const serviceVersion = "0.1.0"

var requiredV0Tables = map[string]struct{}{
	"memory_entries":    {},
	"memory_candidates": {},
	"topic_nodes":       {},
	"topic_edges":       {},
	"topic_views":       {},
	"source_refs":       {},
	"capture_events":    {},
	"curation_events":   {},
	"recall_logs":       {},
	"schema_migrations": {},
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	names, err := h.store.TableNames()
	if err != nil {
		writeError(w, err)
		return
	}
	missing := []string{}
	for name := range requiredV0Tables {
		if _, ok := names[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               len(missing) == 0,
		"service":          "den-memories",
		"version":          serviceVersion,
		"contract_version": "v0",
		"database_path":    "",
		"missing_tables":   missing,
	})
}

func (h *Handler) version(w http.ResponseWriter, r *http.Request) {
	versions, err := h.store.MigrationVersions()
	if err != nil {
		writeError(w, err)
		return
	}
	registry, err := h.loader.Registry()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":          "den-memories",
		"version":          serviceVersion,
		"contract_version": "v0",
		"migrations":       versions,
		"registry_version": registry["contract_version"],
		"scoring_profile":  h.scoring.Defaults["profile"],
	})
}

func (h *Handler) registry(w http.ResponseWriter, r *http.Request) {
	registry, err := h.loader.Registry()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, registry)
}

func (h *Handler) scoringDefaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.scoring.Defaults)
}

func (h *Handler) scoringReadback(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.scoring.Readback())
}

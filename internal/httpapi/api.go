// Package httpapi exposes the Den Memories HTTP JSON API.
package httpapi

import (
	"net/http"

	"den-memories/internal/contracts"
	"den-memories/internal/recall"
	"den-memories/internal/store"
)

// Config contains HTTP API dependencies.
type Config struct {
	Store   *store.Store
	Loader  *contracts.Loader
	Scoring *recall.Scoring
}

// Handler serves the Den Memories HTTP API.
type Handler struct {
	store   *store.Store
	loader  *contracts.Loader
	scoring *recall.Scoring
}

// New creates an API handler.
func New(cfg Config) *Handler {
	return &Handler{store: cfg.Store, loader: cfg.Loader, scoring: cfg.Scoring}
}

// Routes returns the configured HTTP routes.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /api/version", h.version)
	mux.HandleFunc("GET /api/registry", h.registry)
	mux.HandleFunc("GET /api/scoring-defaults", h.scoringDefaults)
	mux.HandleFunc("GET /api/scoring-defaults/readback", h.scoringReadback)

	mux.HandleFunc("POST /api/memory-entries", h.createEntry)
	mux.HandleFunc("GET /api/memory-entries/{slug}", h.getEntry)
	mux.HandleFunc("PUT /api/memory-entries/{slug}", h.updateEntry)
	mux.HandleFunc("POST /api/memory-entries/{slug}/archive", h.archiveEntry)
	mux.HandleFunc("POST /api/memory-entries/{slug}/supersede", h.supersedeEntry)
	mux.HandleFunc("POST /api/memory-entries/search", h.searchEntries)

	mux.HandleFunc("POST /api/candidates", h.createCandidate)
	mux.HandleFunc("GET /api/candidates", h.listCandidates)
	mux.HandleFunc("GET /api/candidates/{candidate_id}", h.getCandidate)
	mux.HandleFunc("PATCH /api/candidates/{candidate_id}/status", h.updateCandidateStatus)
	mux.HandleFunc("POST /api/candidates/search", h.searchCandidates)
	mux.HandleFunc("POST /api/capture", h.capture)

	mux.HandleFunc("POST /api/topic-nodes", h.createNode)
	mux.HandleFunc("GET /api/topic-nodes", h.listNodes)
	mux.HandleFunc("GET /api/topic-nodes/{slug}", h.getNode)
	mux.HandleFunc("PUT /api/topic-nodes/{slug}", h.updateNode)
	mux.HandleFunc("POST /api/topic-nodes/search", h.searchNodes)
	mux.HandleFunc("GET /api/topic-nodes/{slug}/neighbors", h.neighbors)
	mux.HandleFunc("POST /api/topic-edges", h.createEdge)
	mux.HandleFunc("DELETE /api/topic-edges/{edge_id}", h.deleteEdge)
	mux.HandleFunc("POST /api/topic-views", h.createView)
	mux.HandleFunc("GET /api/topic-views", h.listViews)
	mux.HandleFunc("GET /api/topic-views/{slug}", h.getView)
	mux.HandleFunc("PUT /api/topic-views/{slug}", h.updateView)

	mux.HandleFunc("POST /api/curation/candidates/{candidate_id}/claim", h.claimCandidate)
	mux.HandleFunc("POST /api/curation/candidates/{candidate_id}/reject", h.rejectCandidate)
	mux.HandleFunc("POST /api/curation/candidates/{candidate_id}/promote", h.promoteCandidate)
	mux.HandleFunc("POST /api/curation/candidates/{candidate_id}/split", h.splitCandidate)
	mux.HandleFunc("POST /api/curation/candidates/merge", h.mergeCandidates)
	mux.HandleFunc("POST /api/curation/candidates/{candidate_id}/relabel", h.relabelCandidate)
	mux.HandleFunc("POST /api/curation/candidates/{candidate_id}/rescope", h.rescopeCandidate)
	mux.HandleFunc("POST /api/curation/memory-entries/{slug}/supersede", h.curationSupersedeEntry)
	mux.HandleFunc("POST /api/curation/topic-edges/link", h.curationLinkEdge)
	mux.HandleFunc("POST /api/curation/topic-edges/{edge_id}/unlink", h.curationUnlinkEdge)

	mux.HandleFunc("POST /api/recall", h.recall)
	mux.HandleFunc("GET /api/recall-logs/by-packet/{packet_id}", h.getRecallLogByPacket)
	mux.HandleFunc("GET /api/recall-logs", h.listRecallLogs)
	mux.HandleFunc("GET /api/recall-logs/{event_id}", h.getRecallLog)
	mux.HandleFunc("POST /api/source-refs", h.attachSourceRef)
	mux.HandleFunc("GET /api/source-refs", h.listSourceRefs)

	mux.HandleFunc("GET /api/capture-events", h.listCaptureEvents)
	mux.HandleFunc("GET /api/capture-events/recent", h.recentCaptureEvents)
	mux.HandleFunc("GET /api/capture-events/{event_id}", h.getCaptureEvent)
	mux.HandleFunc("GET /api/curation-events", h.listCurationEvents)
	mux.HandleFunc("GET /api/curation-events/{event_id}", h.getCurationEvent)
	mux.HandleFunc("GET /api/observability/summary", h.observabilitySummary)
	mux.HandleFunc("GET /api/observability/pending-candidates", h.observabilityPendingCandidates)
	mux.HandleFunc("GET /api/observability/curation-timeline", h.observabilityCurationTimeline)
	mux.HandleFunc("GET /api/observability/recall-logs", h.observabilityRecallLogs)
	mux.HandleFunc("GET /api/doctor/report", h.doctorReport)
	mux.HandleFunc("POST /api/doctor/report", h.doctorReport)
	mux.HandleFunc("GET /api/audit/export", h.auditExport)
	return mux
}

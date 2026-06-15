package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"den-memories/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeText(w http.ResponseWriter, status int, mediaType string, body string) {
	w.Header().Set("Content-Type", mediaType)
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	detail := err.Error()
	if errors.Is(err, store.ErrNotFound) {
		status = http.StatusNotFound
		detail = "not_found"
	}
	if errors.Is(err, store.ErrInvalid) || errors.Is(err, store.ErrDuplicate) {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"detail": detail})
}

func readPayload(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	payload := map[string]any{}
	if r.Body == nil {
		return payload, nil
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return nil, err
	}
	normalizeRuntimeContext(payload)
	return payload, nil
}

func normalizeRuntimeContext(payload map[string]any) {
	runtimeContext, ok := payload["runtime_context"].(map[string]any)
	if !ok {
		return
	}
	copyStringIfMissing(payload, runtimeContext, "runtime", "runtime")
	copyStringIfMissing(payload, runtimeContext, "actor_identity", "agent_identity")
	copyStringIfMissing(payload, runtimeContext, "created_by", "agent_identity")
	copyStringIfMissing(payload, runtimeContext, "updated_by", "agent_identity")
	copyStringIfMissing(payload, runtimeContext, "actor_role", "role")
	copyStringIfMissing(payload, runtimeContext, "session_kind", "session_kind")
	copyStringIfMissing(payload, runtimeContext, "source_surface", "source_surface")

	if payload["scope_kind"] == nil && payload["proposed_scope_kind"] != nil {
		payload["scope_kind"] = payload["proposed_scope_kind"]
	}
	if payload["scope_id"] == nil && payload["proposed_scope_id"] != nil {
		payload["scope_id"] = payload["proposed_scope_id"]
	}
	if payload["scope_kind"] == nil && runtimeContext["project_id"] != nil {
		payload["scope_kind"] = "project"
	}
	if payload["scope_id"] == nil && runtimeContext["project_id"] != nil {
		payload["scope_id"] = runtimeContext["project_id"]
	}
	if payload["authority_scope_kind"] == nil && payload["scope_kind"] != nil {
		payload["authority_scope_kind"] = payload["scope_kind"]
	}
	if payload["authority_scope_id"] == nil && payload["scope_id"] != nil {
		payload["authority_scope_id"] = payload["scope_id"]
	}
	if payload["raw_text"] == nil && payload["raw_content"] != nil {
		payload["raw_text"] = payload["raw_content"]
	}
}

func copyStringIfMissing(payload map[string]any, runtimeContext map[string]any, targetKey string, sourceKey string) {
	if payload[targetKey] != nil {
		return
	}
	if value, ok := runtimeContext[sourceKey].(string); ok && value != "" {
		payload[targetKey] = value
	}
}

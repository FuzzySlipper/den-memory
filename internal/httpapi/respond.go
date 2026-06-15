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
	return payload, nil
}

package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
)

func stringField(m map[string]any, key string, fallback string) string {
	if value, ok := m[key]; ok && value != nil {
		if text, ok := value.(string); ok {
			return text
		}
		return fmt.Sprint(value)
	}
	return fallback
}

func intField(m map[string]any, key string, fallback int) int {
	if value, ok := m[key]; ok && value != nil {
		switch v := value.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func intQuery(r *http.Request, key string, fallback int) int {
	if value := r.URL.Query().Get(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func queryDefault(r *http.Request, key string, fallback string) string {
	if value := r.URL.Query().Get(key); value != "" {
		return value
	}
	return fallback
}

func pathInt(r *http.Request, key string) (int, error) {
	return strconv.Atoi(r.PathValue(key))
}

func listField(m map[string]any, key string) []any {
	value, ok := m[key]
	if !ok || value == nil {
		return nil
	}
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func int64FromRecord(m map[string]any, key string) int64 {
	value, ok := m[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		parsed, _ := strconv.ParseInt(v, 10, 64)
		return parsed
	default:
		return 0
	}
}

func intFromAny(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

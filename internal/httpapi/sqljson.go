package httpapi

import "den-memories/internal/store"

func mustJSON(value any, fallback any) string {
	text, err := store.JSON(value, fallback)
	if err != nil {
		return "[]"
	}
	return text
}

func mustJSONObject(value any) string {
	text, err := store.JSON(value, map[string]any{})
	if err != nil {
		return "{}"
	}
	return text
}

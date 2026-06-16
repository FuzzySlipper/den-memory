package store

import (
	"encoding/json"
	"strings"
)

var jsonColumns = map[string]struct{}{
	"audience_json":              {},
	"tags_json":                  {},
	"source_refs_json":           {},
	"source_locator_json":        {},
	"candidate_ids_json":         {},
	"before_json":                {},
	"after_json":                 {},
	"request_json":               {},
	"root_node_ids_json":         {},
	"included_node_ids_json":     {},
	"skipped_json":               {},
	"warnings_json":              {},
	"packet_json":                {},
	"include_relations_json":     {},
	"exclude_relations_json":     {},
	"default_unroll_policy_json": {},
	"condition_json":             {},
	"proposed_action_json":       {},
	"proposed_entry_json":        {},
	"proposed_graph_json":        {},
	"evidence_refs_json":         {},
	"existing_context_json":      {},
	"model_metadata_json":        {},
}

// JSON returns canonical JSON text for SQLite storage.
func JSON(value any, fallback any) (string, error) {
	if value == nil {
		value = fallback
	}
	if value == nil {
		value = []any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeJSONColumns(record Record) Record {
	for key, value := range record {
		if _, ok := jsonColumns[key]; !ok {
			continue
		}
		name := strings.TrimSuffix(key, "_json")
		text, ok := value.(string)
		if !ok {
			record[name] = value
			delete(record, key)
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			record[name] = text
		} else {
			record[name] = decoded
		}
		delete(record, key)
	}
	return record
}

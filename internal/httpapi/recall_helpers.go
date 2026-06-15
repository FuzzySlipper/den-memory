package httpapi

import (
	"fmt"
	"strings"

	"den-memories/internal/store"
)

func (h *Handler) loadView(payload map[string]any) (store.Record, error) {
	slug := stringField(payload, "topic_view_slug", "")
	if slug == "" {
		return nil, nil
	}
	return getViewRecord(nilContext(), h.store.DB(), slug)
}

func (h *Handler) traverseTopicEdges(roots []store.Record, view store.Record, policy map[string]any) ([]store.Record, error) {
	include := stringSet(valueOrDefault(policy["include_relations"], viewValue(view, "include_relations", []any{})))
	exclude := stringSet(valueOrDefault(policy["exclude_relations"], viewValue(view, "exclude_relations", []any{})))
	maxDepth := intFromAny(valueOrDefault(policy["max_depth"], viewValue(view, "max_depth", 1)), 1)
	if maxDepth <= 0 {
		return nil, nil
	}
	visited := map[any]struct{}{}
	type frontierItem struct {
		id    any
		depth int
	}
	frontier := []frontierItem{}
	for _, root := range roots {
		visited[root["id"]] = struct{}{}
		frontier = append(frontier, frontierItem{id: root["id"]})
	}
	found := []store.Record{}
	for len(frontier) > 0 {
		item := frontier[0]
		frontier = frontier[1:]
		if item.depth >= maxDepth {
			continue
		}
		rows, err := store.QueryAll(nilContext(), h.store.DB(), `SELECT e.*, n.* FROM topic_edges e JOIN topic_nodes n ON n.id=e.to_node_id WHERE e.from_node_id=? AND e.status='active' ORDER BY e.priority DESC, e.id`, item.id)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			relation := stringValue(row["relation"], "")
			toID := row["to_node_id"]
			if len(include) > 0 {
				if _, ok := include[relation]; !ok {
					continue
				}
			}
			if _, ok := exclude[relation]; ok {
				continue
			}
			if _, ok := visited[toID]; ok {
				continue
			}
			visited[toID] = struct{}{}
			node, err := store.QueryOne(nilContext(), h.store.DB(), `SELECT * FROM topic_nodes WHERE id=?`, toID)
			if err != nil {
				return nil, err
			}
			node["edge_relation"] = relation
			node["edge_depth"] = item.depth + 1
			found = append(found, node)
			frontier = append(frontier, frontierItem{id: toID, depth: item.depth + 1})
		}
	}
	return found, nil
}

func (h *Handler) entryProvenance(entryID any) ([]store.Record, error) {
	return store.QueryAll(nilContext(), h.store.DB(), `SELECT * FROM source_refs WHERE target_kind='memory_entry' AND target_id=? ORDER BY id`, entryID)
}

func sourceStatuses(sources []store.Record) []string {
	statuses := []string{}
	for _, source := range sources {
		statuses = append(statuses, stringValue(source["verification_status"], "unverified"))
	}
	return statuses
}

func sourceRefForContract(source store.Record) map[string]any {
	return map[string]any{
		"source_kind":         stringValue(source["source_kind"], "manual_note"),
		"source_project_id":   source["source_project_id"],
		"source_id":           fmt.Sprint(valueOrDefault(source["source_id"], "unknown")),
		"source_locator":      valueOrDefault(source["source_locator"], map[string]any{}),
		"source_summary":      stringValue(source["source_summary"], ""),
		"observed_at":         source["observed_at"],
		"verified_at":         source["verified_at"],
		"verification_status": stringValue(source["verification_status"], "unverified"),
	}
}

func markdownForPacket(packet map[string]any) string {
	lines := []string{fmt.Sprintf("# Recall packet %s", packet["packet_id"]), "", "## Included"}
	if nodes, ok := packet["included_nodes"].([]map[string]any); ok && len(nodes) > 0 {
		for _, item := range nodes {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) score=%v interpretation=%v", item["title"], item["slug"], item["score"], item["interpretation"]))
			if summary := stringValue(item["summary"], ""); summary != "" {
				lines = append(lines, "  - "+summary)
			}
		}
	} else {
		lines = append(lines, "- none")
	}
	lines = append(lines, "", "## Skipped")
	if skipped, ok := packet["skipped"].([]map[string]any); ok && len(skipped) > 0 {
		for _, item := range skipped {
			lines = append(lines, fmt.Sprintf("- `%s`: %s", item["node_slug"], item["reason"]))
		}
	} else {
		lines = append(lines, "- none")
	}
	return strings.Join(lines, "\n")
}

func authorityScopeString(item store.Record) string {
	scopeID := stringValue(item["authority_scope_id"], "")
	if scopeID == "" {
		return stringValue(item["authority_scope_kind"], "global")
	}
	return stringValue(item["authority_scope_kind"], "global") + ":" + scopeID
}

func matchKind(label string) string {
	switch label {
	case "authoritative":
		return "authoritative"
	case "discovered_evidence":
		return "discoverable"
	default:
		return "fts_only"
	}
}

func containsNodeID(nodes []store.Record, id any) bool {
	for _, node := range nodes {
		if fmt.Sprint(node["id"]) == fmt.Sprint(id) {
			return true
		}
	}
	return false
}

func nodeIDs(nodes []store.Record) []any {
	ids := make([]any, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node["id"])
	}
	return ids
}

func includedTopicNodeIDs(nodes []map[string]any) []any {
	ids := []any{}
	for _, node := range nodes {
		if node["kind"] == "topic_node" {
			ids = append(ids, node["id"])
		}
	}
	return ids
}

func take(items []map[string]any, limit int) []map[string]any {
	if limit < len(items) {
		return items[:limit]
	}
	return items
}

func stringSet(value any) map[string]struct{} {
	result := map[string]struct{}{}
	if items, ok := value.([]any); ok {
		for _, item := range items {
			result[fmt.Sprint(item)] = struct{}{}
		}
	}
	return result
}

func viewValue(view store.Record, key string, fallback any) any {
	if view == nil {
		return fallback
	}
	if value, ok := view[key]; ok && value != nil {
		return value
	}
	return fallback
}

func floatFromAny(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func entryKind(value string) string {
	return value
}

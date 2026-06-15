package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"den-memories/internal/recall"
	"den-memories/internal/store"
)

var excludedEntryStatuses = map[string]struct{}{"superseded": {}, "deprecated": {}, "archived": {}}

func (h *Handler) recall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := readPayload(r)
	if err != nil {
		writeError(w, err)
		return
	}
	query := stringField(payload, "query", "")
	limit := intField(payload, "limit", 10)
	recallCtx := recall.Context{ScopeKind: stringField(payload, "scope_kind", "global"), ScopeID: stringField(payload, "scope_id", "")}
	view, err := h.loadView(payload)
	if err != nil {
		writeError(w, err)
		return
	}
	rootEntries, err := store.QueryAll(ctx, h.store.DB(), `SELECT e.* FROM memory_entries_fts f JOIN memory_entries e ON e.id=f.rowid WHERE memory_entries_fts MATCH ? LIMIT ?`, query, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	rootNodes, err := store.QueryAll(ctx, h.store.DB(), `SELECT n.* FROM topic_nodes_fts f JOIN topic_nodes n ON n.id=f.rowid WHERE topic_nodes_fts MATCH ? LIMIT ?`, query, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	if view != nil && !containsNodeID(rootNodes, view["root_node_id"]) {
		root, err := store.QueryOne(ctx, h.store.DB(), `SELECT * FROM topic_nodes WHERE id=?`, view["root_node_id"])
		if err == nil {
			rootNodes = append([]store.Record{root}, rootNodes...)
		}
	}
	edgeNodes, err := h.traverseTopicEdges(rootNodes, view, payload)
	if err != nil {
		writeError(w, err)
		return
	}

	includedNodes := []map[string]any{}
	includedEdges := []map[string]any{}
	rootMatches := []map[string]any{}
	skipped := []map[string]any{}
	provenance := []map[string]any{}
	warnings := []string{}

	for _, entry := range rootEntries {
		status := stringValue(entry["status"], "")
		if _, ok := excludedEntryStatuses[status]; ok {
			skipped = append(skipped, map[string]any{"node_slug": entry["slug"], "reason": "status:" + status})
			continue
		}
		sources, err := h.entryProvenance(entry["id"])
		if err != nil {
			writeError(w, err)
			return
		}
		statuses := sourceStatuses(sources)
		score := h.scoring.ScoreItem(entry, recallCtx, statuses, 0, "")
		label := stringValue(score["authority_label"], "")
		if label == "out_of_scope" {
			skipped = append(skipped, map[string]any{"node_slug": entry["slug"], "reason": "out_of_scope"})
			continue
		}
		if label == "discovered_evidence" {
			warnings = append(warnings, fmt.Sprintf("%s is discovered cross-scope evidence, not authority for this scope", entry["slug"]))
		}
		for _, source := range sources {
			provenance = append(provenance, sourceRefForContract(source))
		}
		rootMatches = append(rootMatches, map[string]any{"node_slug": entry["slug"], "match_kind": matchKind(label), "score": score["score"], "why": "FTS entry match; " + label})
		includedNodes = append(includedNodes, map[string]any{
			"kind": entryKind("memory_entry"), "id": entry["id"], "slug": entry["slug"], "title": entry["title"],
			"summary": stringValue(entry["summary"], ""), "score": score["score"], "authority_scope": authorityScopeString(entry),
			"discovery_scope": entry["discovery_scope"], "claim_strength": entry["claim_strength"], "interpretation": label,
			"scope_kind": entry["scope_kind"], "scope_id": entry["scope_id"], "authority_scope_kind": entry["authority_scope_kind"],
			"authority_scope_id": entry["authority_scope_id"],
		})
	}

	for _, node := range append(rootNodes, edgeNodes...) {
		item := mergeRecord(node, map[string]any{"status": stringValue(node["status"], "active"), "confidence": "source_backed"})
		score := h.scoring.ScoreItem(item, recallCtx, nil, intFromAny(node["edge_depth"], 0), stringValue(node["edge_relation"], ""))
		label := stringValue(score["authority_label"], "")
		if label == "out_of_scope" {
			skipped = append(skipped, map[string]any{"node_slug": node["slug"], "reason": "out_of_scope"})
			continue
		}
		if stringValue(node["edge_relation"], "") == "" {
			rootMatches = append(rootMatches, map[string]any{"node_slug": node["slug"], "match_kind": matchKind(label), "score": score["score"], "why": "FTS/topic-view root; " + label})
		} else {
			includedEdges = append(includedEdges, map[string]any{"from_view": payload["topic_view_slug"], "to_node_slug": node["slug"], "relation": node["edge_relation"], "depth": intFromAny(node["edge_depth"], 0)})
		}
		includedNodes = append(includedNodes, map[string]any{
			"kind": entryKind("topic_node"), "id": node["id"], "slug": node["slug"], "title": node["title"],
			"summary": stringValue(node["summary"], ""), "score": score["score"], "authority_scope": authorityScopeString(node),
			"discovery_scope": node["discovery_scope"], "claim_strength": node["claim_strength"], "interpretation": label,
			"edge_relation": node["edge_relation"], "edge_depth": intFromAny(node["edge_depth"], 0), "scope_kind": node["scope_kind"],
			"scope_id": node["scope_id"], "authority_scope_kind": node["authority_scope_kind"], "authority_scope_id": node["authority_scope_id"],
		})
	}
	sort.SliceStable(includedNodes, func(i, j int) bool {
		return floatFromAny(includedNodes[i]["score"]) > floatFromAny(includedNodes[j]["score"])
	})
	packetID := stringField(payload, "packet_id", "")
	if packetID == "" {
		count := 0
		_ = h.store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM recall_logs`).Scan(&count)
		packetID = fmt.Sprintf("recall-%d", count+1)
	}
	packet := map[string]any{
		"packet_id": packetID, "packet_md": "", "root_matches": rootMatches, "included_nodes": take(includedNodes, limit),
		"included_edges": includedEdges, "skipped": skipped, "warnings": warnings, "provenance": provenance,
		"audit": map[string]any{"scoring_profile": h.scoring.Defaults["profile"], "scoring_defaults_ref": "contracts/v0/scoring-defaults.json"},
	}
	packet["packet_md"] = markdownForPacket(packet)
	logID, err := store.Insert(ctx, h.store.DB(), `INSERT INTO recall_logs(packet_id,request_json,root_node_ids_json,included_node_ids_json,skipped_json,warnings_json,scoring_profile,token_budget,estimated_tokens,created_by,packet_json)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		packetID, mustJSONObject(payload), mustJSON(nodeIDs(rootNodes), []any{}), mustJSON(includedTopicNodeIDs(includedNodes), []any{}),
		mustJSON(skipped, []any{}), mustJSON(warnings, []any{}), h.scoring.Defaults["profile"], payload["token_budget"], len(strings.Fields(stringValue(packet["packet_md"], ""))), payload["actor_identity"], "{}")
	if err != nil {
		writeError(w, err)
		return
	}
	packet["audit"].(map[string]any)["recall_log_id"] = logID
	if err := store.Exec(ctx, h.store.DB(), `UPDATE recall_logs SET packet_json=? WHERE id=?`, mustJSONObject(packet), logID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, packet)
}

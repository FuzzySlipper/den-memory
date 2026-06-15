package httpapi

import (
	"context"
	"fmt"
	"strings"

	"den-memories/internal/store"
)

var secretMarkers = []string{"BEGIN PRIVATE KEY", "BEGIN RSA PRIVATE KEY", "api_key=", "xoxb-", "ghp_", "sk-"}

func refreshEntryFTS(ctx context.Context, r store.Runner, rowID int64, payload map[string]any) error {
	if err := store.Exec(ctx, r, `DELETE FROM memory_entries_fts WHERE rowid=?`, rowID); err != nil {
		return err
	}
	return store.Exec(ctx, r, `INSERT INTO memory_entries_fts(rowid,slug,title,summary,body_md,tags_json) VALUES (?,?,?,?,?,?)`,
		rowID, stringField(payload, "slug", ""), stringField(payload, "title", ""), stringField(payload, "summary", ""),
		stringField(payload, "body_md", ""), mustJSON(payload["tags"], []any{}))
}

func refreshCandidateFTS(ctx context.Context, r store.Runner, rowID int64, payload map[string]any) error {
	if err := store.Exec(ctx, r, `DELETE FROM memory_candidates_fts WHERE rowid=?`, rowID); err != nil {
		return err
	}
	return store.Exec(ctx, r, `INSERT INTO memory_candidates_fts(rowid,title,summary,body_md,proposed_kind) VALUES (?,?,?,?,?)`,
		rowID, stringField(payload, "title", ""), stringField(payload, "summary", ""), stringField(payload, "body_md", ""),
		stringField(payload, "proposed_kind", ""))
}

func refreshNodeFTS(ctx context.Context, r store.Runner, rowID int64, payload map[string]any) error {
	if err := store.Exec(ctx, r, `DELETE FROM topic_nodes_fts WHERE rowid=?`, rowID); err != nil {
		return err
	}
	return store.Exec(ctx, r, `INSERT INTO topic_nodes_fts(rowid,slug,title,summary) VALUES (?,?,?,?)`,
		rowID, stringField(payload, "slug", ""), stringField(payload, "title", ""), stringField(payload, "summary", ""))
}

func logCuration(ctx context.Context, r store.Runner, action string, actor string, reason string, ids map[string]any, before any, after any) (int64, error) {
	return store.Insert(ctx, r, `INSERT INTO curation_events(candidate_id,memory_entry_id,node_id,edge_id,action,actor_identity,reason,before_json,after_json)
VALUES (?,?,?,?,?,?,?,?,?)`,
		ids["candidate_id"], ids["memory_entry_id"], ids["node_id"], ids["edge_id"],
		action, actor, reason, mustJSONObject(before), mustJSONObject(after))
}

func logCapture(ctx context.Context, r store.Runner, payload map[string]any, decision string, reason string, candidateIDs []any, rawSize int, extractedSize int) (int64, error) {
	body := stringField(payload, "body_md", "")
	if body == "" {
		body = stringField(payload, "raw_text", stringField(payload, "content", ""))
	}
	summary := stringField(payload, "summary", "")
	if rawSize < 0 {
		rawSize = len(body)
	}
	if extractedSize < 0 {
		extractedSize = len(summary)
	}
	return store.Insert(ctx, r, `INSERT INTO capture_events(event_kind,source_refs_json,actor_identity,runtime,proposed_scope_kind,proposed_scope_id,capture_policy_id,decision,reason,candidate_ids_json,raw_size,extracted_size)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		stringField(payload, "event_kind", "manual_note"),
		mustJSON(payload["source_refs"], []any{}),
		stringField(payload, "actor_identity", stringField(payload, "created_by", "api")),
		stringField(payload, "runtime", "manual"),
		stringField(payload, "scope_kind", "global"),
		valueOrNil(payload["scope_id"]),
		stringField(payload, "capture_policy_id", "api-capture"),
		decision, reason, mustJSON(candidateIDs, []any{}), rawSize, extractedSize)
}

func insertCandidate(ctx context.Context, r store.Runner, payload map[string]any) (int64, error) {
	if status := stringField(payload, "status", ""); status != "" && status != "pending" {
		return 0, fmt.Errorf("%w: candidate_create_status_must_be_pending", store.ErrInvalid)
	}
	if layer := stringField(payload, "layer", ""); layer != "" && layer != "candidate" {
		return 0, fmt.Errorf("%w: candidate_create_layer_must_be_candidate", store.ErrInvalid)
	}
	if stringField(payload, "claim_strength", "observation") == "policy" {
		return 0, fmt.Errorf("%w: candidate_create_cannot_use_policy_claim_strength", store.ErrInvalid)
	}
	payload["status"] = "pending"
	payload["layer"] = "candidate"
	id, err := store.Insert(ctx, r, `INSERT INTO memory_candidates(proposed_slug,title,body_md,summary,proposed_kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,audience_json,source_refs_json,extraction_confidence,status,created_by,updated_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		valueOrNil(payload["proposed_slug"]), payload["title"], stringField(payload, "body_md", ""), stringField(payload, "summary", ""),
		payload["proposed_kind"], "candidate", stringField(payload, "scope_kind", "global"), valueOrNil(payload["scope_id"]),
		stringField(payload, "authority_scope_kind", "global"), valueOrNil(payload["authority_scope_id"]),
		stringField(payload, "discovery_scope", "explicit_only"), stringField(payload, "claim_strength", "observation"),
		mustJSON(payload["audience"], []any{}), mustJSON(payload["source_refs"], []any{}), valueOrNil(payload["extraction_confidence"]),
		"pending", stringField(payload, "created_by", "api"), stringField(payload, "updated_by", stringField(payload, "created_by", "api")))
	if err != nil {
		return 0, err
	}
	return id, refreshCandidateFTS(ctx, r, id, payload)
}

func candidatePayloadFromCapture(payload map[string]any) map[string]any {
	body := stringField(payload, "body_md", "")
	if body == "" {
		body = stringField(payload, "raw_text", stringField(payload, "content", ""))
	}
	title := stringField(payload, "title", "")
	if title == "" {
		title = firstLine(body, 80)
	}
	if title == "" {
		title = "Captured memory candidate"
	}
	summary := stringField(payload, "summary", "")
	if summary == "" {
		summary = truncate(body, 180)
	}
	if summary == "" {
		summary = title
	}
	return map[string]any{
		"proposed_slug":         payload["proposed_slug"],
		"title":                 title,
		"body_md":               body,
		"summary":               summary,
		"proposed_kind":         stringField(payload, "proposed_kind", stringField(payload, "kind", "fact")),
		"layer":                 "candidate",
		"scope_kind":            stringField(payload, "scope_kind", "global"),
		"scope_id":              payload["scope_id"],
		"authority_scope_kind":  stringField(payload, "authority_scope_kind", stringField(payload, "scope_kind", "global")),
		"authority_scope_id":    valueOrDefault(payload["authority_scope_id"], payload["scope_id"]),
		"discovery_scope":       stringField(payload, "discovery_scope", "explicit_only"),
		"claim_strength":        stringField(payload, "claim_strength", "observation"),
		"audience":              valueOrDefault(payload["audience"], []any{}),
		"source_refs":           valueOrDefault(payload["source_refs"], []any{}),
		"extraction_confidence": payload["extraction_confidence"],
		"status":                "pending",
		"created_by":            stringField(payload, "actor_identity", stringField(payload, "created_by", "api")),
		"updated_by":            stringField(payload, "actor_identity", stringField(payload, "created_by", "api")),
	}
}

func validateCaptureCandidate(candidate map[string]any) string {
	text := strings.TrimSpace(stringField(candidate, "title", "") + stringField(candidate, "summary", "") + stringField(candidate, "body_md", ""))
	if text == "" {
		return "empty_capture_content"
	}
	lower := strings.ToLower(text)
	for _, marker := range secretMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return "secret_like_content_filtered"
		}
	}
	if stringField(candidate, "claim_strength", "") == "policy" {
		return "policy_strength_capture_filtered"
	}
	return ""
}

func valueOrNil(value any) any {
	return value
}

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func firstLine(text string, limit int) string {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return truncate(line, limit)
		}
	}
	return ""
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

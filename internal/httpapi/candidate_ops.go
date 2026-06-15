package httpapi

import (
	"context"
	"fmt"
	"strings"

	"den-memories/internal/store"
)

func getCandidateRecord(ctx context.Context, r store.Runner, candidateID int) (store.Record, error) {
	return store.QueryOne(ctx, r, `SELECT * FROM memory_candidates WHERE id=?`, candidateID)
}

func getEntryRecord(ctx context.Context, r store.Runner, slug string) (store.Record, error) {
	return store.QueryOne(ctx, r, `SELECT * FROM memory_entries WHERE slug=?`, slug)
}

func getNodeRecord(ctx context.Context, r store.Runner, slug string) (store.Record, error) {
	return store.QueryOne(ctx, r, `SELECT * FROM topic_nodes WHERE slug=?`, slug)
}

func getViewRecord(ctx context.Context, r store.Runner, slug string) (store.Record, error) {
	return store.QueryOne(ctx, r, `SELECT * FROM topic_views WHERE slug=?`, slug)
}

func updateCandidateFields(ctx context.Context, r store.Runner, candidateID int, updates map[string]any) (store.Record, error) {
	allowed := map[string]string{
		"status":               "status",
		"proposed_slug":        "proposed_slug",
		"title":                "title",
		"body_md":              "body_md",
		"summary":              "summary",
		"proposed_kind":        "proposed_kind",
		"scope_kind":           "scope_kind",
		"scope_id":             "scope_id",
		"authority_scope_kind": "authority_scope_kind",
		"authority_scope_id":   "authority_scope_id",
		"discovery_scope":      "discovery_scope",
		"claim_strength":       "claim_strength",
		"layer":                "layer",
		"audience":             "audience_json",
		"source_refs":          "source_refs_json",
		"updated_by":           "updated_by",
	}
	sets := []string{}
	args := []any{}
	for key, value := range updates {
		column, ok := allowed[key]
		if !ok {
			continue
		}
		if key == "audience" || key == "source_refs" {
			value = mustJSON(value, []any{})
		}
		sets = append(sets, column+"=?")
		args = append(args, value)
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at=datetime('now')")
		args = append(args, candidateID)
		if err := store.Exec(ctx, r, `UPDATE memory_candidates SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
			return nil, err
		}
	}
	row, err := getCandidateRecord(ctx, r, candidateID)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"title":         row["title"],
		"summary":       row["summary"],
		"body_md":       row["body_md"],
		"proposed_kind": row["proposed_kind"],
	}
	return row, refreshCandidateFTS(ctx, r, int64(candidateID), payload)
}

func requireActorReason(payload map[string]any) (string, string, error) {
	actor := stringField(payload, "actor_identity", "")
	reason := stringField(payload, "reason", "")
	if actor == "" {
		return "", "", fmt.Errorf("%w: actor_identity_required", store.ErrInvalid)
	}
	if reason == "" {
		return "", "", fmt.Errorf("%w: reason_required", store.ErrInvalid)
	}
	return actor, reason, nil
}

func insertMemoryEntryFromCandidate(ctx context.Context, r store.Runner, candidate store.Record, payload map[string]any, actor string) (int64, error) {
	slug := stringField(payload, "slug", "")
	if slug == "" {
		slug = stringValue(candidate["proposed_slug"], "")
	}
	if slug == "" {
		slug = fmt.Sprintf("candidate-%v", candidate["id"])
	}
	entry := map[string]any{
		"slug":                 slug,
		"title":                stringField(payload, "title", stringValue(candidate["title"], "")),
		"summary":              stringField(payload, "summary", stringValue(candidate["summary"], "")),
		"body_md":              stringField(payload, "body_md", stringValue(candidate["body_md"], "")),
		"kind":                 stringField(payload, "kind", stringValue(candidate["proposed_kind"], "fact")),
		"layer":                stringField(payload, "layer", "curated_fact"),
		"scope_kind":           stringField(payload, "scope_kind", stringValue(candidate["scope_kind"], "global")),
		"scope_id":             valueOrDefault(payload["scope_id"], candidate["scope_id"]),
		"authority_scope_kind": stringField(payload, "authority_scope_kind", stringValue(candidate["authority_scope_kind"], "global")),
		"authority_scope_id":   valueOrDefault(payload["authority_scope_id"], candidate["authority_scope_id"]),
		"discovery_scope":      stringField(payload, "discovery_scope", stringValue(candidate["discovery_scope"], "explicit_only")),
		"claim_strength":       stringField(payload, "claim_strength", stringValue(candidate["claim_strength"], "observation")),
		"status":               "active",
		"curation_state":       "curated",
		"confidence":           stringField(payload, "confidence", "source_backed"),
		"stability":            stringField(payload, "stability", "evolving"),
		"audience":             valueOrDefault(payload["audience"], candidate["audience"]),
		"tags":                 valueOrDefault(payload["tags"], []any{}),
	}
	id, err := store.Insert(ctx, r, `INSERT INTO memory_entries(slug,title,summary,body_md,content_format,kind,layer,scope_kind,scope_id,authority_scope_kind,authority_scope_id,discovery_scope,claim_strength,status,curation_state,confidence,stability,audience_json,tags_json,created_by,updated_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		entry["slug"], entry["title"], entry["summary"], entry["body_md"], "markdown", entry["kind"], entry["layer"],
		entry["scope_kind"], entry["scope_id"], entry["authority_scope_kind"], entry["authority_scope_id"], entry["discovery_scope"],
		entry["claim_strength"], entry["status"], entry["curation_state"], entry["confidence"], entry["stability"],
		mustJSON(entry["audience"], []any{}), mustJSON(entry["tags"], []any{}), actor, actor)
	if err != nil {
		return 0, err
	}
	return id, refreshEntryFTS(ctx, r, id, entry)
}

func attachCandidateSourceRefs(ctx context.Context, r store.Runner, candidate store.Record, entryID int64, actor string) ([]int64, error) {
	sources, ok := candidate["source_refs"].([]any)
	if !ok {
		return nil, nil
	}
	return attachSourceRefs(ctx, r, sources, "memory_entry", entryID, actor, stringValue(candidate["summary"], ""), fmt.Sprint(candidate["id"]))
}

func attachSourceRefs(ctx context.Context, r store.Runner, sources []any, targetKind string, targetID int64, actor string, fallbackSummary string, fallbackSourceID string) ([]int64, error) {
	refIDs := []int64{}
	for _, item := range sources {
		source, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, err := store.Insert(ctx, r, `INSERT INTO source_refs(target_kind,target_id,source_kind,source_project_id,source_id,source_locator_json,source_summary,observed_at,verified_at,verification_status,created_by)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			targetKind, targetID, stringField(source, "source_kind", "manual_note"), source["source_project_id"],
			stringField(source, "source_id", fallbackSourceID), mustJSONObject(source["source_locator"]),
			stringField(source, "source_summary", fallbackSummary), source["observed_at"], source["verified_at"],
			stringField(source, "verification_status", "unverified"), actor)
		if err != nil {
			return nil, err
		}
		refIDs = append(refIDs, id)
	}
	return refIDs, nil
}

func stringValue(value any, fallback string) string {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

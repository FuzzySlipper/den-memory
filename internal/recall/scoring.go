// Package recall implements guided recall scoring and packet helpers.
package recall

import (
	"fmt"
	"math"

	"den-memories/internal/contracts"
	"den-memories/internal/store"
)

// Context describes the authority scope for a recall request.
type Context struct {
	ScopeKind string
	ScopeID   string
}

// Scoring contains v0 recall scoring constants.
type Scoring struct {
	Defaults map[string]any
	Weights  map[string]float64
}

// LoadScoring loads scoring defaults from contract artifacts.
func LoadScoring(loader *contracts.Loader) (*Scoring, error) {
	defaults, err := loader.ScoringDefaults()
	if err != nil {
		return nil, err
	}
	s := &Scoring{Defaults: defaults, Weights: map[string]float64{}}
	for section, values := range defaults {
		items, ok := values.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range items {
			if f, ok := value.(float64); ok {
				s.Weights[section+"."+key] = f
			}
		}
	}
	return s, nil
}

// ScoreItem computes the v0 recall score and authority label for an item.
func (s *Scoring) ScoreItem(item store.Record, ctx Context, sourceStatuses []string, edgeDepth int, relation string) map[string]any {
	label := authorityLabel(item, ctx)
	score := 0.0
	switch label {
	case "authoritative":
		score = s.weight("root_match.authoritative_scope_match")
	case "discovered_evidence":
		score = s.weight("root_match.discoverable_scope_match") + s.weight("penalties.cross_scope_discovered_evidence")
	default:
		score = s.weight("penalties.authority_scope_mismatch")
	}
	score += s.weight("claim_strength_modifier." + stringValue(item["claim_strength"], "observation"))
	score += s.weight("entry_status_modifier." + stringValue(item["status"], "active"))
	score += s.weight("confidence_modifier." + stringValue(item["confidence"], "unverified"))
	score += s.sourceValidationScore(sourceStatuses)
	if relation != "" {
		score += s.weight("edge_relation_modifier." + relation)
	}
	if edgeDepth > 0 {
		score *= math.Pow(s.weight("traversal.edge_traversal_decay"), float64(edgeDepth))
	}
	return map[string]any{"score": math.Round(score*1000) / 1000, "authority_label": label}
}

// Readback returns the public scoring default readback shape.
func (s *Scoring) Readback() map[string]any {
	return map[string]any{
		"profile":                              s.Defaults["profile"],
		"description":                          s.Defaults["description"],
		"AUTHORITATIVE_MATCH_WEIGHT":           s.weight("root_match.authoritative_scope_match"),
		"DISCOVERABLE_MATCH_WEIGHT":            s.weight("root_match.discoverable_scope_match"),
		"CROSS_SCOPE_PENALTY":                  s.weight("penalties.cross_scope_discovered_evidence"),
		"CLAIM_STRENGTH_POLICY_WEIGHT":         s.weight("claim_strength_modifier.policy"),
		"CLAIM_STRENGTH_RECOMMENDATION_WEIGHT": s.weight("claim_strength_modifier.recommendation"),
		"CLAIM_STRENGTH_ASSESSMENT_WEIGHT":     s.weight("claim_strength_modifier.assessment"),
		"CLAIM_STRENGTH_OBSERVATION_WEIGHT":    s.weight("claim_strength_modifier.observation"),
		"CURATED_STATUS_WEIGHT":                s.weight("curation_modifier.curated"),
		"SOURCE_VERIFIED_WEIGHT":               s.weight("source_validation_modifier.verified"),
		"SOURCE_BROKEN_PENALTY":                s.weight("source_validation_modifier.broken"),
		"EDGE_TRAVERSAL_DECAY":                 s.weight("traversal.edge_traversal_decay"),
	}
}

func (s *Scoring) sourceValidationScore(statuses []string) float64 {
	if len(statuses) == 0 {
		return s.weight("source_validation_modifier.unverified")
	}
	total := 0.0
	for _, status := range statuses {
		total += s.weight("source_validation_modifier." + status)
	}
	return total / float64(len(statuses))
}

func (s *Scoring) weight(key string) float64 {
	value, ok := s.Weights[key]
	if !ok {
		return 0
	}
	return value
}

func authorityLabel(item store.Record, ctx Context) string {
	if authorityMatches(item, ctx) || isGlobalAuthority(item) {
		return "authoritative"
	}
	if isDiscoverable(item, ctx) {
		return "discovered_evidence"
	}
	return "out_of_scope"
}

func authorityMatches(item store.Record, ctx Context) bool {
	return stringValue(item["authority_scope_kind"], "") == ctx.ScopeKind && stringValue(item["authority_scope_id"], "") == ctx.ScopeID
}

func isGlobalAuthority(item store.Record) bool {
	return stringValue(item["authority_scope_kind"], "global") == "global" && stringValue(item["authority_scope_id"], "") == ""
}

func isDiscoverable(item store.Record, ctx Context) bool {
	if authorityMatches(item, ctx) || isGlobalAuthority(item) {
		return true
	}
	discovery := stringValue(item["discovery_scope"], "explicit_only")
	if discovery == "global_discoverable" {
		return true
	}
	if discovery == "same_project" && ctx.ScopeKind == "project" && stringValue(item["scope_kind"], "") == "project" && stringValue(item["scope_id"], "") == ctx.ScopeID {
		return true
	}
	return discovery == "linked_projects" && ctx.ScopeKind == "project"
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

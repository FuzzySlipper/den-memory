package curator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Packet is the bounded input presented to a deterministic or LLM-backed proposer.
type Packet struct {
	Candidate  map[string]any   `json:"candidate"`
	SourceRefs []any            `json:"source_refs"`
	Proposals  []map[string]any `json:"proposals"`
	QueueState string           `json:"queue_state"`
}

// Proposal is a service proposal-create payload.
type Proposal map[string]any

// Proposer creates proposal payloads from queue packets. Future LLM-backed
// proposers should implement this interface without changing the CLI runner.
type Proposer interface {
	Propose(context.Context, Packet) ([]Proposal, error)
}

// Config controls one curator pass.
type Config struct {
	BaseURL          string
	Action           string
	CandidateIDs     []int
	Limit            int
	ProposerIdentity string
	ProposerKind     string
	Reason           string
	HTTPClient       *http.Client
	DryRun           bool
}

// Result summarizes a curator pass.
type Result struct {
	SelectedCandidateIDs []int            `json:"selected_candidate_ids"`
	CreatedProposalIDs   []int            `json:"created_proposal_ids"`
	Proposals            []map[string]any `json:"proposals"`
	DryRun               bool             `json:"dry_run"`
}

// DeterministicProposer creates simple proposal payloads without model calls.
type DeterministicProposer struct {
	Action           string
	ProposerIdentity string
	ProposerKind     string
	Reason           string
}

func (p DeterministicProposer) Propose(_ context.Context, packet Packet) ([]Proposal, error) {
	cid := intFromAny(packet.Candidate["id"])
	if cid == 0 {
		return nil, fmt.Errorf("candidate missing id")
	}
	reason := strings.TrimSpace(p.Reason)
	if reason == "" {
		reason = "deterministic curator proposal"
	}
	identity := strings.TrimSpace(p.ProposerIdentity)
	if identity == "" {
		identity = "den-memory-curator"
	}
	kind := strings.TrimSpace(p.ProposerKind)
	if kind == "" {
		kind = "deterministic_cli"
	}
	base := Proposal{
		"candidate_ids":     []any{cid},
		"proposer_identity": identity,
		"proposer_kind":     kind,
		"reason":            reason,
		"confidence":        "unverified",
		"source_refs":       packet.SourceRefs,
		"existing_context":  map[string]any{"queue_state": packet.QueueState},
		"model_metadata":    map[string]any{"mode": "deterministic"},
	}
	switch p.Action {
	case "promote":
		entry := proposedEntryFromCandidate(packet.Candidate)
		base["proposal_kind"] = "promote_candidate"
		base["confidence"] = "source_backed"
		base["proposed_entry"] = entry
		base["proposed_action"] = map[string]any{"action": "promote_candidate", "candidate_id": cid, "payload": entry}
	case "reject":
		base["proposal_kind"] = "reject_candidate"
		base["proposed_action"] = map[string]any{"action": "reject_candidate", "candidate_id": cid, "payload": map[string]any{"reason": reason}}
	case "defer":
		base["proposal_kind"] = "defer"
		base["status"] = "needs_human"
		base["proposed_action"] = map[string]any{"action": "defer", "candidate_id": cid, "payload": map[string]any{"reason": reason}}
	default:
		return nil, fmt.Errorf("unsupported deterministic action %q", p.Action)
	}
	return []Proposal{base}, nil
}

// Run performs one proposal-only curator pass. It never calls apply endpoints.
func Run(ctx context.Context, cfg Config, proposer Proposer) (Result, error) {
	if proposer == nil {
		proposer = DeterministicProposer{Action: cfg.Action, ProposerIdentity: cfg.ProposerIdentity, ProposerKind: cfg.ProposerKind, Reason: cfg.Reason}
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8780"
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = 50
	}
	queueURL := fmt.Sprintf("%s/api/curation/queue?limit=%d", baseURL, limit)
	queue, err := getJSON(ctx, client, queueURL)
	if err != nil {
		return Result{}, err
	}
	wanted := map[int]struct{}{}
	for _, id := range cfg.CandidateIDs {
		if id > 0 {
			wanted[id] = struct{}{}
		}
	}
	result := Result{DryRun: cfg.DryRun}
	for _, raw := range anySlice(queue["items"]) {
		item, ok := raw.(map[string]any)
		if !ok || item["queue_state"] != "needs_proposal" {
			continue
		}
		candidate, _ := item["candidate"].(map[string]any)
		cid := intFromAny(candidate["id"])
		if len(wanted) > 0 {
			if _, ok := wanted[cid]; !ok {
				continue
			}
		}
		packet := Packet{Candidate: candidate, SourceRefs: anySlice(item["source_refs"]), QueueState: stringFromAny(item["queue_state"])}
		for _, rawProposal := range anySlice(item["proposals"]) {
			if proposal, ok := rawProposal.(map[string]any); ok {
				packet.Proposals = append(packet.Proposals, proposal)
			}
		}
		proposals, err := proposer.Propose(ctx, packet)
		if err != nil {
			return result, err
		}
		result.SelectedCandidateIDs = append(result.SelectedCandidateIDs, cid)
		for _, proposal := range proposals {
			if cfg.DryRun {
				result.Proposals = append(result.Proposals, proposal)
				continue
			}
			created, err := postJSON(ctx, client, baseURL+"/api/curation/proposals", proposal)
			if err != nil {
				return result, err
			}
			result.Proposals = append(result.Proposals, created)
			if id := intFromAny(created["id"]); id > 0 {
				result.CreatedProposalIDs = append(result.CreatedProposalIDs, id)
			}
		}
	}
	return result, nil
}

func proposedEntryFromCandidate(candidate map[string]any) map[string]any {
	slug := stringFromAny(candidate["proposed_slug"])
	if slug == "" {
		slug = fmt.Sprintf("candidate-%d", intFromAny(candidate["id"]))
	}
	return map[string]any{
		"slug":                 slug,
		"title":                stringFromAny(candidate["title"]),
		"summary":              stringFromAny(candidate["summary"]),
		"body_md":              stringFromAny(candidate["body_md"]),
		"kind":                 fallbackString(candidate["proposed_kind"], "fact"),
		"scope_kind":           fallbackString(candidate["scope_kind"], "global"),
		"scope_id":             candidate["scope_id"],
		"authority_scope_kind": fallbackString(candidate["authority_scope_kind"], fallbackString(candidate["scope_kind"], "global")),
		"authority_scope_id":   candidate["authority_scope_id"],
		"discovery_scope":      fallbackString(candidate["discovery_scope"], "explicit_only"),
		"claim_strength":       fallbackString(candidate["claim_strength"], "observation"),
		"confidence":           "source_backed",
		"stability":            "evolving",
	}
}

func getJSON(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeResponse(resp)
}

func postJSON(ctx context.Context, client *http.Client, target string, payload any) (map[string]any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return decodeResponse(resp)
}

func decodeResponse(resp *http.Response) (map[string]any, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, err
		}
	} else {
		decoded = map[string]any{}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %d %s", resp.Request.Method, safeURL(resp.Request.URL), resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return decoded, nil
}

func anySlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		parsed, _ := v.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func fallbackString(value any, fallback string) string {
	if text := stringFromAny(value); text != "" {
		return text
	}
	return fallback
}

func safeURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Redacted()
}

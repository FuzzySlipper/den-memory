package curator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultMaxLLMPacketBytes = 12000

var supportedLLMProposalKinds = map[string]struct{}{
	"promote_candidate":    {},
	"reject_candidate":     {},
	"split_candidate":      {},
	"merge_candidates":     {},
	"rescope_candidate":    {},
	"relabel_candidate":    {},
	"supersede_entry":      {},
	"knowledge_candidate":  {},
	"doc_update_candidate": {},
	"defer":                {},
}

// LLMConfig configures an OpenAI-compatible chat-completions proposer.
// The proposer is intentionally proposal-only: it returns POST /api/curation/proposals
// payloads and never applies them.
type LLMConfig struct {
	BaseURL          string
	APIKey           string
	Model            string
	Temperature      float64
	MaxPacketBytes   int
	ProposerIdentity string
	ProposerKind     string
	HTTPClient       *http.Client
}

// LLMProposer asks an OpenAI-compatible model for strict JSON curation proposals.
type LLMProposer struct {
	cfg LLMConfig
}

func NewLLMProposer(cfg LLMConfig) (*LLMProposer, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("llm base URL required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("llm model required")
	}
	if cfg.MaxPacketBytes <= 0 {
		cfg.MaxPacketBytes = defaultMaxLLMPacketBytes
	}
	if strings.TrimSpace(cfg.ProposerIdentity) == "" {
		cfg.ProposerIdentity = "den-memory-llm-curator"
	}
	if strings.TrimSpace(cfg.ProposerKind) == "" {
		cfg.ProposerKind = "llm"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &LLMProposer{cfg: cfg}, nil
}

func (p *LLMProposer) Propose(ctx context.Context, packet Packet) ([]Proposal, error) {
	prompt, packetHash, err := buildLLMPrompt(packet, p.cfg.MaxPacketBytes)
	if err != nil {
		return nil, err
	}
	responseText, responseHash, err := p.callChatCompletions(ctx, prompt)
	if err != nil {
		return nil, err
	}
	proposals, err := parseLLMProposalResponse(responseText)
	if err != nil {
		return nil, err
	}
	if len(proposals) == 0 {
		return nil, fmt.Errorf("llm returned no proposals")
	}
	for idx, proposal := range proposals {
		if err := normalizeAndValidateLLMProposal(proposal, packet, p.cfg, packetHash, responseHash); err != nil {
			return nil, fmt.Errorf("proposal %d invalid: %w", idx, err)
		}
	}
	return proposals, nil
}

func buildLLMPrompt(packet Packet, maxBytes int) (string, string, error) {
	bounded := boundedPacketForLLM(packet)
	data, err := json.MarshalIndent(bounded, "", "  ")
	if err != nil {
		return "", "", err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		data = data[:maxBytes]
	}
	hash := sha256Hex(data)
	prompt := strings.TrimSpace(`You are the Den Memories curation proposer.

Your job is to produce proposal JSON only. You may propose but must never apply curated memory.
Runtime agents store candidates; proposals are still not truth until an explicit curator applies them.

Allowed proposal_kind values:
- promote_candidate
- reject_candidate
- split_candidate
- merge_candidates
- rescope_candidate
- relabel_candidate
- supersede_entry
- knowledge_candidate
- doc_update_candidate
- defer

Return strict JSON with this shape:
{
  "proposals": [
    {
      "proposal_kind": "promote_candidate|reject_candidate|knowledge_candidate|doc_update_candidate|defer|...",
      "status": "proposed" or "needs_human",
      "candidate_ids": [123],
      "reason": "why this proposal is appropriate",
      "confidence": "unverified|inferred|source_backed|verified",
      "proposed_entry": { ... },
      "proposed_action": { "action": "...", "candidate_id": 123, "payload": { ... } },
      "evidence_refs": [ ... ]
    }
  ]
}

Rules:
- For promote_candidate, include a proposed_entry with slug, title, summary, body_md, kind, scope_kind, scope_id, authority_scope_kind, authority_scope_id, discovery_scope, claim_strength, confidence, and stability when known.
- For reject_candidate, explain the rejection in reason and proposed_action.payload.reason.
- Use knowledge_candidate for reusable reference/manual content that should not be Den Memories truth yet.
- Use doc_update_candidate for project documentation truth updates that belong in Den docs rather than memory.
- Use needs_human when evidence is ambiguous.
- Do not invent source evidence beyond the packet. Keep proposals concise.

Curator input packet JSON follows:
`) + "\n" + string(data)
	return prompt, hash, nil
}

func boundedPacketForLLM(packet Packet) map[string]any {
	candidate := copyMap(packet.Candidate)
	candidate["body_md"] = truncateString(stringFromAny(candidate["body_md"]), 4000)
	candidate["summary"] = truncateString(stringFromAny(candidate["summary"]), 1000)
	return map[string]any{
		"candidate":   candidate,
		"source_refs": takeAny(packet.SourceRefs, 12),
		"proposals":   takeMap(packet.Proposals, 6),
		"queue_state": packet.QueueState,
		"registry": map[string]any{
			"proposal_kinds": []string{"promote_candidate", "reject_candidate", "split_candidate", "merge_candidates", "rescope_candidate", "relabel_candidate", "supersede_entry", "knowledge_candidate", "doc_update_candidate", "defer"},
			"statuses":       []string{"proposed", "needs_human"},
		},
	}
}

func (p *LLMProposer) callChatCompletions(ctx context.Context, prompt string) (string, string, error) {
	payload := map[string]any{
		"model":       p.cfg.Model,
		"temperature": p.cfg.Temperature,
		"messages": []map[string]string{
			{"role": "system", "content": "Return only strict JSON. Do not include markdown fences."},
			{"role": "user", "content": prompt},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	endpoint := chatCompletionsURL(p.cfg.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(p.cfg.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.cfg.APIKey))
	}
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("llm HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	content, err := chatMessageContent(body)
	if err != nil {
		return "", "", err
	}
	return content, sha256Hex([]byte(content)), nil
}

func chatCompletionsURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/chat/completions"
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/v1/chat/completions"
}

func chatMessageContent(body []byte) (string, error) {
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("llm response had no choices")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("llm response content empty")
	}
	return content, nil
}

func parseLLMProposalResponse(text string) ([]Proposal, error) {
	clean := stripJSONFence(strings.TrimSpace(text))
	var envelope struct {
		Proposals []Proposal `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(clean), &envelope); err == nil && len(envelope.Proposals) > 0 {
		return envelope.Proposals, nil
	}
	var single Proposal
	if err := json.Unmarshal([]byte(clean), &single); err != nil {
		return nil, err
	}
	if _, ok := single["proposal_kind"]; !ok {
		return nil, fmt.Errorf("missing proposals array or proposal_kind")
	}
	return []Proposal{single}, nil
}

func normalizeAndValidateLLMProposal(proposal Proposal, packet Packet, cfg LLMConfig, packetHash string, responseHash string) error {
	kind := stringFromAny(proposal["proposal_kind"])
	if _, ok := supportedLLMProposalKinds[kind]; !ok {
		return fmt.Errorf("unsupported proposal_kind %q", kind)
	}
	status := fallbackString(proposal["status"], "proposed")
	if status != "proposed" && status != "needs_human" {
		return fmt.Errorf("unsupported proposal status %q", status)
	}
	proposal["status"] = status
	candidateID := intFromAny(packet.Candidate["id"])
	if candidateID == 0 {
		return fmt.Errorf("candidate missing id")
	}
	if err := validateProposalCandidateScope(proposal, candidateID); err != nil {
		return err
	}
	if len(anySlice(proposal["candidate_ids"])) == 0 {
		proposal["candidate_ids"] = []any{candidateID}
	}
	if strings.TrimSpace(stringFromAny(proposal["reason"])) == "" {
		return fmt.Errorf("reason required")
	}
	if strings.TrimSpace(stringFromAny(proposal["proposer_identity"])) == "" {
		proposal["proposer_identity"] = cfg.ProposerIdentity
	}
	if strings.TrimSpace(stringFromAny(proposal["proposer_kind"])) == "" {
		proposal["proposer_kind"] = cfg.ProposerKind
	}
	if strings.TrimSpace(stringFromAny(proposal["confidence"])) == "" {
		proposal["confidence"] = "unverified"
	}
	if proposal["source_refs"] == nil {
		proposal["source_refs"] = packet.SourceRefs
	}
	if proposal["existing_context"] == nil {
		proposal["existing_context"] = map[string]any{"queue_state": packet.QueueState}
	}
	metadata, _ := proposal["model_metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["mode"] = "llm"
	metadata["provider"] = "openai_compatible"
	metadata["model"] = cfg.Model
	metadata["prompt_sha256"] = packetHash
	metadata["response_sha256"] = responseHash
	metadata["max_packet_bytes"] = cfg.MaxPacketBytes
	proposal["model_metadata"] = metadata
	if kind == "promote_candidate" {
		entry, ok := proposal["proposed_entry"].(map[string]any)
		if !ok || strings.TrimSpace(stringFromAny(entry["slug"])) == "" || strings.TrimSpace(stringFromAny(entry["body_md"])) == "" {
			return fmt.Errorf("promote_candidate requires proposed_entry with slug and body_md")
		}
	}
	if proposal["proposed_action"] == nil {
		proposal["proposed_action"] = defaultLLMProposedAction(kind, candidateID, proposal)
	}
	return nil
}

func validateProposalCandidateScope(proposal Proposal, packetCandidateID int) error {
	ids := candidateIDsFromProposal(proposal)
	for _, id := range ids {
		if id != packetCandidateID {
			return fmt.Errorf("proposal candidate id %d does not match bounded packet candidate %d", id, packetCandidateID)
		}
	}
	return nil
}

func candidateIDsFromProposal(proposal Proposal) []int {
	ids := []int{}
	if id := intFromAny(proposal["candidate_id"]); id > 0 {
		ids = append(ids, id)
	}
	for _, raw := range anySlice(proposal["candidate_ids"]) {
		if id := intFromAny(raw); id > 0 {
			ids = append(ids, id)
		}
	}
	if action, ok := proposal["proposed_action"].(map[string]any); ok {
		if id := intFromAny(action["candidate_id"]); id > 0 {
			ids = append(ids, id)
		}
		for _, raw := range anySlice(action["candidate_ids"]) {
			if id := intFromAny(raw); id > 0 {
				ids = append(ids, id)
			}
		}
	}
	return uniqueIntsForValidation(ids)
}

func uniqueIntsForValidation(ids []int) []int {
	seen := map[int]struct{}{}
	result := []int{}
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func defaultLLMProposedAction(kind string, candidateID int, proposal Proposal) map[string]any {
	payload := map[string]any{"reason": proposal["reason"]}
	if entry, ok := proposal["proposed_entry"].(map[string]any); ok {
		payload = entry
	}
	return map[string]any{"action": kind, "candidate_id": candidateID, "candidate_ids": []any{candidateID}, "payload": payload}
}

func stripJSONFence(text string) string {
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
	}
	return strings.TrimSpace(text)
}

func copyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func takeAny(items []any, limit int) []any {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func takeMap(items []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func truncateString(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

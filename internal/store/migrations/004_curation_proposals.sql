CREATE TABLE IF NOT EXISTS curation_proposals (
  id INTEGER PRIMARY KEY,
  proposal_kind TEXT NOT NULL CHECK (proposal_kind IN ('promote_candidate','reject_candidate','split_candidate','merge_candidates','rescope_candidate','relabel_candidate','supersede_entry','knowledge_candidate','doc_update_candidate','defer')),
  status TEXT NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','needs_human','applied','rejected','deferred','superseded','invalid')),
  candidate_ids_json TEXT NOT NULL DEFAULT '[]',
  target_memory_entry_id INTEGER REFERENCES memory_entries(id),
  proposed_action_json TEXT NOT NULL DEFAULT '{}',
  proposed_entry_json TEXT NOT NULL DEFAULT '{}',
  proposed_graph_json TEXT NOT NULL DEFAULT '{}',
  source_refs_json TEXT NOT NULL DEFAULT '[]',
  evidence_refs_json TEXT NOT NULL DEFAULT '[]',
  existing_context_json TEXT NOT NULL DEFAULT '{}',
  proposer_identity TEXT NOT NULL,
  proposer_kind TEXT NOT NULL DEFAULT 'agent' CHECK (proposer_kind IN ('agent','human','llm','deterministic_cli','worker','system')),
  confidence TEXT NOT NULL DEFAULT 'unverified' CHECK (confidence IN ('unverified','inferred','source_backed','verified','needs_human')),
  reason TEXT NOT NULL DEFAULT '',
  model_metadata_json TEXT NOT NULL DEFAULT '{}',
  applied_curation_event_id INTEGER REFERENCES curation_events(id),
  applied_memory_entry_id INTEGER REFERENCES memory_entries(id),
  applied_by TEXT,
  applied_at TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_curation_proposals_status ON curation_proposals(status);
CREATE INDEX IF NOT EXISTS idx_curation_proposals_kind ON curation_proposals(proposal_kind);

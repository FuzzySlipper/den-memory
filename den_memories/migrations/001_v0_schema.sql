CREATE TABLE IF NOT EXISTS memory_entries (
  id INTEGER PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  summary TEXT NOT NULL DEFAULT '',
  body_md TEXT NOT NULL DEFAULT '',
  content_format TEXT NOT NULL DEFAULT 'markdown' CHECK (content_format IN ('markdown','plain','structured_json','external_ref')),
  kind TEXT NOT NULL,
  layer TEXT NOT NULL DEFAULT 'curated_fact' CHECK (layer IN ('source_evidence','candidate','curated_fact','identity_profile','topic_scene','operating_model','schema_model','intent_hypothesis')),
  scope_kind TEXT NOT NULL DEFAULT 'global' CHECK (scope_kind IN ('global','project','space','profile','agent_instance','user','task','external')),
  scope_id TEXT,
  authority_scope_kind TEXT NOT NULL DEFAULT 'global' CHECK (authority_scope_kind IN ('global','project','space','profile','agent_instance','user','task')),
  authority_scope_id TEXT,
  discovery_scope TEXT NOT NULL DEFAULT 'explicit_only' CHECK (discovery_scope IN ('same_project','linked_projects','global_discoverable','explicit_only')),
  claim_strength TEXT NOT NULL DEFAULT 'observation' CHECK (claim_strength IN ('observation','assessment','recommendation','policy')),
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','disputed','superseded','deprecated','archived')),
  curation_state TEXT NOT NULL DEFAULT 'candidate' CHECK (curation_state IN ('raw','candidate','claimed','curated','rejected','needs_split','needs_merge','needs_review')),
  confidence TEXT NOT NULL DEFAULT 'unverified' CHECK (confidence IN ('unverified','inferred','source_backed','verified')),
  stability TEXT NOT NULL DEFAULT 'evolving' CHECK (stability IN ('volatile','evolving','stable','canonical')),
  audience_json TEXT NOT NULL DEFAULT '[]',
  tags_json TEXT NOT NULL DEFAULT '[]',
  valid_from TEXT,
  valid_until TEXT,
  verified_at TEXT,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_entries_fts USING fts5(slug, title, summary, body_md, tags_json, content='memory_entries', content_rowid='id');

CREATE TABLE IF NOT EXISTS memory_candidates (
  id INTEGER PRIMARY KEY,
  proposed_slug TEXT,
  title TEXT NOT NULL,
  body_md TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  proposed_kind TEXT NOT NULL,
  layer TEXT NOT NULL DEFAULT 'candidate' CHECK (layer IN ('source_evidence','candidate','curated_fact','identity_profile','topic_scene','operating_model','schema_model','intent_hypothesis')),
  scope_kind TEXT NOT NULL DEFAULT 'global' CHECK (scope_kind IN ('global','project','space','profile','agent_instance','user','task','external')),
  scope_id TEXT,
  authority_scope_kind TEXT NOT NULL DEFAULT 'global' CHECK (authority_scope_kind IN ('global','project','space','profile','agent_instance','user','task')),
  authority_scope_id TEXT,
  discovery_scope TEXT NOT NULL DEFAULT 'explicit_only' CHECK (discovery_scope IN ('same_project','linked_projects','global_discoverable','explicit_only')),
  claim_strength TEXT NOT NULL DEFAULT 'observation' CHECK (claim_strength IN ('observation','assessment','recommendation','policy')),
  audience_json TEXT NOT NULL DEFAULT '[]',
  source_refs_json TEXT NOT NULL DEFAULT '[]',
  extraction_confidence REAL CHECK (extraction_confidence IS NULL OR (extraction_confidence >= 0 AND extraction_confidence <= 1)),
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','claimed','promoted','rejected','needs_split','needs_merge','superseded')),
  created_by TEXT NOT NULL,
  updated_by TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS topic_nodes (
  id INTEGER PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  node_type TEXT NOT NULL CHECK (node_type IN ('concept','fact','decision','runbook','warning','failure_mode','pattern','anti_pattern','example','open_question','index','bundle','source_ref')),
  content_ref_kind TEXT,
  content_ref_id TEXT,
  memory_entry_id INTEGER REFERENCES memory_entries(id),
  summary TEXT NOT NULL DEFAULT '',
  layer TEXT NOT NULL DEFAULT 'topic_scene' CHECK (layer IN ('source_evidence','candidate','curated_fact','identity_profile','topic_scene','operating_model','schema_model','intent_hypothesis')),
  canonicality TEXT NOT NULL DEFAULT 'preferred' CHECK (canonicality IN ('canonical','preferred','alternate','historical','superseded')),
  importance TEXT NOT NULL DEFAULT 'important' CHECK (importance IN ('core','important','optional','niche')),
  stability TEXT NOT NULL DEFAULT 'evolving' CHECK (stability IN ('volatile','evolving','stable','canonical')),
  scope_kind TEXT NOT NULL DEFAULT 'global' CHECK (scope_kind IN ('global','project','space','profile','agent_instance','user','task','external')),
  scope_id TEXT,
  authority_scope_kind TEXT NOT NULL DEFAULT 'global' CHECK (authority_scope_kind IN ('global','project','space','profile','agent_instance','user','task')),
  authority_scope_id TEXT,
  discovery_scope TEXT NOT NULL DEFAULT 'explicit_only' CHECK (discovery_scope IN ('same_project','linked_projects','global_discoverable','explicit_only')),
  claim_strength TEXT NOT NULL DEFAULT 'observation' CHECK (claim_strength IN ('observation','assessment','recommendation','policy')),
  audience_json TEXT NOT NULL DEFAULT '[]',
  default_unroll_policy_json TEXT,
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS topic_nodes_fts USING fts5(slug, title, summary, content='topic_nodes', content_rowid='id');

CREATE TABLE IF NOT EXISTS topic_edges (
  id INTEGER PRIMARY KEY,
  from_node_id INTEGER NOT NULL REFERENCES topic_nodes(id),
  to_node_id INTEGER NOT NULL REFERENCES topic_nodes(id),
  relation TEXT NOT NULL CHECK (relation IN ('prerequisite','implementation_detail','operational_runbook','failure_mode','warning','evidence_for','contradicted_by','supersedes','historical_context','related_concept','example','review_risk','source_trace','owned_by_project','contextual_assessment')),
  weight REAL NOT NULL DEFAULT 1.0,
  priority INTEGER NOT NULL DEFAULT 0,
  condition_json TEXT,
  condition_hash TEXT NOT NULL DEFAULT '',
  audience_json TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disputed','deprecated')),
  notes TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(from_node_id, to_node_id, relation, condition_hash)
);

CREATE TABLE IF NOT EXISTS topic_views (
  id INTEGER PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  root_node_id INTEGER NOT NULL REFERENCES topic_nodes(id),
  view_type TEXT NOT NULL CHECK (view_type IN ('beginner_path','implementation_path','reviewer_path','ops_runbook','failure_modes','architecture_map','project_onboarding')),
  audience_json TEXT NOT NULL DEFAULT '[]',
  mode TEXT NOT NULL DEFAULT 'general' CHECK (mode IN ('planning','implementation','review','ops','general','audit')),
  include_relations_json TEXT NOT NULL DEFAULT '[]',
  exclude_relations_json TEXT NOT NULL DEFAULT '[]',
  max_depth INTEGER NOT NULL DEFAULT 2 CHECK (max_depth >= 0),
  token_budget_hint INTEGER NOT NULL DEFAULT 3000 CHECK (token_budget_hint > 0),
  ordering_policy TEXT NOT NULL DEFAULT 'core_first_then_risks' CHECK (ordering_policy IN ('core_first_then_risks','score_desc','roots_then_edges','review_risk_first','ops_runbook_first')),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disputed','deprecated')),
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS source_refs (
  id INTEGER PRIMARY KEY,
  target_kind TEXT NOT NULL CHECK (target_kind IN ('memory_entry','memory_candidate','topic_node','topic_edge','topic_view','capture_event','curation_event','recall_log')),
  target_id INTEGER NOT NULL,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('den_task','den_document','den_message','den_review_finding','hermes_session','hermes_memory_file','pi_crew_session','pi_crew_assignment','pi_crew_run','url','file','manual_note','imported_transcript')),
  source_project_id TEXT,
  source_id TEXT NOT NULL,
  source_locator_json TEXT NOT NULL DEFAULT '{}',
  source_summary TEXT NOT NULL DEFAULT '',
  observed_at TEXT,
  verified_at TEXT,
  verification_status TEXT NOT NULL DEFAULT 'unverified' CHECK (verification_status IN ('unverified','verified','broken','stale')),
  created_by TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS capture_events (
  id INTEGER PRIMARY KEY,
  event_kind TEXT NOT NULL CHECK (event_kind IN ('turn','task_message','document_comment','worker_packet','tool_result','manual_note','imported_session')),
  source_refs_json TEXT NOT NULL DEFAULT '[]',
  actor_identity TEXT NOT NULL,
  runtime TEXT NOT NULL CHECK (runtime IN ('hermes','pi_crew','den_core','manual','import')),
  proposed_scope_kind TEXT NOT NULL CHECK (proposed_scope_kind IN ('global','project','space','profile','agent_instance','user','task','external')),
  proposed_scope_id TEXT,
  capture_policy_id TEXT NOT NULL,
  decision TEXT NOT NULL CHECK (decision IN ('captured','ignored','filtered','errored')),
  reason TEXT NOT NULL DEFAULT '',
  candidate_ids_json TEXT NOT NULL DEFAULT '[]',
  raw_size INTEGER NOT NULL DEFAULT 0 CHECK (raw_size >= 0),
  extracted_size INTEGER NOT NULL DEFAULT 0 CHECK (extracted_size >= 0),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS curation_events (
  id INTEGER PRIMARY KEY,
  candidate_id INTEGER REFERENCES memory_candidates(id),
  memory_entry_id INTEGER REFERENCES memory_entries(id),
  node_id INTEGER REFERENCES topic_nodes(id),
  edge_id INTEGER REFERENCES topic_edges(id),
  action TEXT NOT NULL CHECK (action IN ('claim','promote','reject','split','merge','supersede','rescope','relabel','link','unlink')),
  actor_identity TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  before_json TEXT NOT NULL DEFAULT '{}',
  after_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS recall_logs (
  id INTEGER PRIMARY KEY,
  packet_id TEXT NOT NULL UNIQUE,
  request_json TEXT NOT NULL,
  root_node_ids_json TEXT NOT NULL DEFAULT '[]',
  included_node_ids_json TEXT NOT NULL DEFAULT '[]',
  skipped_json TEXT NOT NULL DEFAULT '[]',
  warnings_json TEXT NOT NULL DEFAULT '[]',
  scoring_profile TEXT NOT NULL DEFAULT 'v0-default',
  token_budget INTEGER,
  estimated_tokens INTEGER,
  created_by TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

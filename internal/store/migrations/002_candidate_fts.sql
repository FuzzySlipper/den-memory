DROP TABLE IF EXISTS memory_entries_fts;
DROP TABLE IF EXISTS topic_nodes_fts;
CREATE VIRTUAL TABLE IF NOT EXISTS memory_entries_fts USING fts5(slug, title, summary, body_md, tags_json);
CREATE VIRTUAL TABLE IF NOT EXISTS topic_nodes_fts USING fts5(slug, title, summary);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_candidates_fts USING fts5(title, summary, body_md, proposed_kind);

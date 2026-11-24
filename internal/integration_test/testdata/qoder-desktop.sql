CREATE TABLE workspaces (
  workspace_id TEXT PRIMARY KEY, name TEXT NOT NULL, workspace_source TEXT NOT NULL,
  root_paths_json TEXT NOT NULL, product_mode TEXT NOT NULL, extra_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, archived INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER
);
CREATE TABLE chat_sessions (
  session_id TEXT PRIMARY KEY, session_kind TEXT NOT NULL, conversation_mode TEXT NOT NULL,
  product_mode TEXT NOT NULL, title TEXT NOT NULL, cwd TEXT NOT NULL, execution_kind TEXT,
  workspace_id TEXT, model TEXT, permission_mode TEXT, additional_direcotries TEXT NOT NULL DEFAULT '{}',
  extra_json TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
  execution_target_json TEXT NOT NULL, message_envelope_version INTEGER, archived INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER
);
CREATE TABLE chat_session_messages (
  session_id TEXT NOT NULL REFERENCES chat_sessions(session_id), message_id TEXT NOT NULL,
  turn_id TEXT, sequence INTEGER NOT NULL, payload_json TEXT NOT NULL, status TEXT NOT NULL,
  source TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
  role TEXT, turn_started_at INTEGER, is_compaction_boundary INTEGER, has_visible_content INTEGER,
  payload_bytes INTEGER, payload_hash TEXT, payload_schema_version INTEGER,
  PRIMARY KEY(session_id,message_id), UNIQUE(session_id,sequence)
);

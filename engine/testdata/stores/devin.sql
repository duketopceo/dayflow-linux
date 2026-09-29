-- dayflow agent-store contract fixture: devin
-- captured from: sessions.db (schema + synthesized sentinel rows only — never real row payloads)
-- regenerate:    dayflow fixtures capture devin
-- sentinel instant: 2026-01-05T10:00:00Z (unix 1767607200) — contract tests scan its local day
-- contract:      store_contracts_test.go replays this file and asserts the devin adapter
--                extracts >=1 session; failure means upstream store drift.
--                See docs/maintenance.md.

CREATE TABLE message_nodes (row_id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL, node_id INTEGER NOT NULL, parent_node_id INTEGER,
  chat_message TEXT NOT NULL, created_at INTEGER NOT NULL);
-- pragma_table_info(message_nodes): row_id INTEGER PK, session_id TEXT NOT NULL, node_id INTEGER NOT NULL, parent_node_id INTEGER, chat_message TEXT NOT NULL, created_at INTEGER NOT NULL
INSERT INTO "message_nodes" ("row_id", "session_id", "node_id", "parent_node_id", "chat_message", "created_at") VALUES(NULL, '__fixture_session_1__', 1, 0, '{"message_id":"__fixture_msg_1__","role":"user","content":"__fixture_title_1__","metadata":{"is_user_input":true}}', 1767607200);
INSERT INTO "message_nodes" ("row_id", "session_id", "node_id", "parent_node_id", "chat_message", "created_at") VALUES(NULL, '__fixture_session_1__', 2, 0, '{"message_id":"__fixture_msg_2__","role":"assistant","content":"__fixture_assistant_1__","metadata":{}}', 1767607500);

CREATE TABLE sessions (id TEXT PRIMARY KEY, working_directory TEXT NOT NULL,
  backend_type TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
  agent_mode TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL,
  last_activity_at INTEGER NOT NULL, title TEXT);
-- pragma_table_info(sessions): id TEXT PK, working_directory TEXT NOT NULL, backend_type TEXT NOT NULL, model TEXT NOT NULL, agent_mode TEXT NOT NULL, created_at INTEGER NOT NULL, last_activity_at INTEGER NOT NULL, title TEXT
INSERT INTO "sessions" ("id", "working_directory", "backend_type", "model", "agent_mode", "created_at", "last_activity_at", "title") VALUES('__fixture_session_1__', '/fixture/proj', '__fixture_backend_type__', '__fixture_model__', '__fixture_agent_mode__', 1767607200, 1767607500, '__fixture_title_1__');


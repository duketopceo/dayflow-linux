-- dayflow agent-store contract fixture: opencode
-- captured from: opencode.db (schema + synthesized sentinel rows only — never real row payloads)
-- regenerate:    dayflow fixtures capture opencode
-- sentinel instant: 2026-01-05T10:00:00Z (unix 1767607200) — contract tests scan its local day
-- contract:      store_contracts_test.go replays this file and asserts the opencode adapter
--                extracts >=1 session; failure means upstream store drift.
--                See docs/maintenance.md.

CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);
-- pragma_table_info(message): id TEXT PK, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL
INSERT INTO "message" ("id", "session_id", "time_created", "time_updated", "data") VALUES('__fixture_msg_1__', '__fixture_session_1__', 1767607200000, 1767607200000, '{"role":"user"}');
INSERT INTO "message" ("id", "session_id", "time_created", "time_updated", "data") VALUES('__fixture_msg_2__', '__fixture_session_1__', 1767607500000, 1767607500000, '{"role":"assistant"}');

CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL,
  session_id TEXT NOT NULL, time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL, data TEXT NOT NULL);
-- pragma_table_info(part): id TEXT PK, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL
INSERT INTO "part" ("id", "message_id", "session_id", "time_created", "time_updated", "data") VALUES('__fixture_part_1__', '__fixture_msg_1__', '__fixture_session_1__', 1767607200000, 1767607200000, '{"type":"text","text":"__fixture_title_1__"}');
INSERT INTO "part" ("id", "message_id", "session_id", "time_created", "time_updated", "data") VALUES('__fixture_part_2__', '__fixture_msg_2__', '__fixture_session_1__', 1767607500000, 1767607500000, '{"type":"text","text":"__fixture_assistant_1__"}');

CREATE TABLE session (id TEXT PRIMARY KEY, title TEXT NOT NULL,
  directory TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL);
-- pragma_table_info(session): id TEXT PK, title TEXT NOT NULL, directory TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL
INSERT INTO "session" ("id", "title", "directory", "time_created", "time_updated") VALUES('__fixture_session_1__', '__fixture_title_1__', '/fixture/proj', 1767607200000, 1767607500000);

CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
  type TEXT NOT NULL, seq INTEGER NOT NULL, time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL, data TEXT NOT NULL);
-- pragma_table_info(session_message): id TEXT PK, session_id TEXT NOT NULL, type TEXT NOT NULL, seq INTEGER NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL


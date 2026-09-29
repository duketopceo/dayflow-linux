-- dayflow agent-store contract fixture: cursor
-- captured from: state-fallback.vscdb (schema + synthesized sentinel rows only — never real row payloads)
-- regenerate:    dayflow fixtures capture cursor
-- sentinel instant: 2026-01-05T10:00:00Z (unix 1767607200) — contract tests scan its local day
-- contract:      store_contracts_test.go replays this file and asserts the cursor adapter
--                extracts >=1 session; failure means upstream store drift.
--                See docs/maintenance.md.

CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
-- pragma_table_info(ItemTable): key TEXT, value BLOB

CREATE TABLE cursorDiskKV (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);
-- pragma_table_info(cursorDiskKV): key TEXT, value BLOB
INSERT INTO "cursorDiskKV" ("key", "value") VALUES('composerData:__fixture_composer_1__', '{"_v":18,"composerId":"__fixture_composer_1__","createdAt":1767607200000,"lastUpdatedAt":1767607500000,"name":"__fixture_title_1__","fullConversationHeadersOnly":[{"bubbleId":"__fixture_bubble_1__","type":1},{"bubbleId":"__fixture_bubble_2__","type":2}],"conversationMap":{}}');
INSERT INTO "cursorDiskKV" ("key", "value") VALUES('bubbleId:__fixture_composer_1__:__fixture_bubble_1__', '{"type":1,"bubbleId":"__fixture_bubble_1__","text":"__fixture_title_1__","createdAt":1767607200000}');
INSERT INTO "cursorDiskKV" ("key", "value") VALUES('bubbleId:__fixture_composer_1__:__fixture_bubble_2__', '{"type":2,"bubbleId":"__fixture_bubble_2__","text":"__fixture_assistant_1__","createdAt":1767607500000}');


package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Minimal MCP server over stdio (JSON-RPC 2.0). Exposes the journal to agents:
//   claude mcp add dayflow -- dayflow mcp
// or any MCP client that launches `dayflow mcp`.

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func mcpRespond(id json.RawMessage, result any) {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	fmt.Println(string(b))
}

func mcpErr(id json.RawMessage, code int, msg string) {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": msg},
	})
	fmt.Println(string(b))
}

var mcpTools = []map[string]any{
	{"name": "get_timeline", "description": "Summarized activity blocks for a date (YYYY-MM-DD, 'today', 'yesterday') or a range ('week', 'month'). Returns title/summary/category per 15-min block.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"date": map[string]any{"type": "string", "description": "YYYY-MM-DD, today, yesterday, week, month"}}}},
	{"name": "get_status", "description": "Current recording state: paused, frames today, blocks done/pending, model.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "search_journal", "description": "Search block titles/summaries for a substring (e.g. an app, file, or topic). Returns matching blocks.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"query": map[string]any{"type": "string"}}, "required": []string{"query"}}},
	{"name": "get_events", "description": "Recent engine event log (captures, dedup skips, errors).",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"limit": map[string]any{"type": "integer"}}}},
	{"name": "get_usage", "description": "OpenRouter token usage totals.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "get_stats", "description": "Storage usage (db, frames, total), journal block counts, date coverage, and API call/token totals.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "get_standup", "description": "Generate a standup update from yesterday and today's blocks.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "get_insights", "description": "Focus, category, app, and distraction analytics for a range (day, week, month).",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"range": map[string]any{"type": "string", "description": "day, week, or month"}}}},
	{"name": "chat", "description": "Ask a question about the user's work journal. Optionally continue an existing conversation.",
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"message":         map[string]any{"type": "string", "description": "The user's question"},
			"conversation_id": map[string]any{"type": "integer", "description": "Optional existing conversation id"}},
			"required": []string{"message"}}},
}

func mcpText(v any) map[string]any {
	var text string
	if s, ok := v.(string); ok {
		text = s
	} else {
		b, _ := json.MarshalIndent(v, "", "  ")
		text = string(b)
	}
	return map[string]any{"content": []map[string]any{
		{"type": "text", "text": text},
	}}
}

func mcpCall(db *sql.DB, cfg Config, name string, args map[string]any) (any, error) {
	switch name {
	case "get_timeline":
		d, _ := args["date"].(string)
		now := time.Now()
		var start, end time.Time
		switch d {
		case "", "today":
			start, end = dayBounds(now)
		case "yesterday":
			start, end = dayBounds(now.AddDate(0, 0, -1))
		case "week":
			start, end = weekBounds(now)
		case "month":
			start, end = monthBounds(now)
		default:
			t, err := time.ParseInLocation("2006-01-02", d, time.Local)
			if err != nil {
				return nil, fmt.Errorf("bad date %q", d)
			}
			start, end = dayBounds(t)
		}
		blocks, err := blocksBetween(db, start, end)
		if err != nil {
			return nil, err
		}
		return map[string]any{"start": start.Format("2006-01-02"), "end": end.Format("2006-01-02"), "blocks": blocks}, nil

	case "get_status":
		now := time.Now()
		frames, _ := countFramesToday(db, now)
		done, _ := countBlocksToday(db, now)
		pending, _ := pendingBlocks(db, cfg, now)
		return map[string]any{
			"paused": paused(), "frames_today": frames, "blocks_done": done,
			"blocks_pending": len(pending), "model": cfg.Model,
		}, nil

	case "search_journal":
		q, _ := args["query"].(string)
		if q == "" {
			return nil, fmt.Errorf("query required")
		}
		rows, err := db.Query(`SELECT start_ts,end_ts,title,summary,category FROM blocks
		  WHERE status='done' AND (title LIKE ? OR summary LIKE ?) ORDER BY start_ts DESC LIMIT 50`,
			"%"+q+"%", "%"+q+"%")
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []map[string]string
		for rows.Next() {
			var s, e int64
			var t, su, c string
			if err := rows.Scan(&s, &e, &t, &su, &c); err != nil {
				continue
			}
			out = append(out, map[string]string{
				"start": time.Unix(s, 0).Local().Format("2006-01-02 15:04"),
				"title": t, "summary": su, "category": c,
			})
		}
		return map[string]any{"matches": out}, nil

	case "get_events":
		limit := 20.0
		if l, ok := args["limit"].(float64); ok {
			limit = l
		}
		rows, err := db.Query(`SELECT ts,type,detail FROM events ORDER BY ts DESC LIMIT ?`, int(limit))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []map[string]string
		for rows.Next() {
			var ts int64
			var t, d string
			if err := rows.Scan(&ts, &t, &d); err != nil {
				continue
			}
			out = append(out, map[string]string{
				"time": time.Unix(ts, 0).Local().Format("2006-01-02 15:04:05"), "type": t, "detail": d,
			})
		}
		return map[string]any{"events": out}, nil

	case "get_usage":
		var calls, pt, ct, okn, failed int
		if err := db.QueryRow(`SELECT COUNT(1), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
		  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0)
		  FROM api_calls`).Scan(&calls, &pt, &ct, &okn, &failed); err != nil {
			return nil, err
		}
		return map[string]any{"api_calls": calls, "ok": okn, "failed": failed,
			"prompt_tokens": pt, "completion_tokens": ct}, nil

	case "get_stats":
		var blocksTotal, blocksDone, blocksFailed, blocksDead, framesPending, eventsTotal int
		db.QueryRow(`SELECT COUNT(1),
		  COALESCE(SUM(CASE WHEN status='done' THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN status='dead' THEN 1 ELSE 0 END),0)
		  FROM blocks`).Scan(&blocksTotal, &blocksDone, &blocksFailed, &blocksDead)
		db.QueryRow(`SELECT COUNT(1) FROM frames`).Scan(&framesPending)
		db.QueryRow(`SELECT COUNT(1) FROM events`).Scan(&eventsTotal)
		var firstTS, lastTS sql.NullInt64
		db.QueryRow(`SELECT MIN(start_ts), MAX(end_ts) FROM blocks WHERE status='done'`).Scan(&firstTS, &lastTS)
		firstDay, lastDay := "", ""
		if firstTS.Valid {
			firstDay = time.Unix(firstTS.Int64, 0).Local().Format("2006-01-02")
		}
		if lastTS.Valid {
			lastDay = time.Unix(lastTS.Int64, 0).Local().Format("2006-01-02")
		}
		var calls2, ok2, failed2, pt2, ct2 int
		var avgLat float64
		db.QueryRow(`SELECT COUNT(1),
		  COALESCE(SUM(CASE WHEN status='ok' THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(CASE WHEN status!='ok' THEN 1 ELSE 0 END),0),
		  COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0),
		  COALESCE(AVG(latency_ms),0)
		  FROM api_calls`).Scan(&calls2, &ok2, &failed2, &pt2, &ct2, &avgLat)
		var dbBytes, walBytes int64
		if fi, err := os.Stat(dbPath()); err == nil {
			dbBytes = fi.Size()
		}
		if fi, err := os.Stat(dbPath() + "-wal"); err == nil {
			walBytes = fi.Size()
		}
		framesBytes, frameFiles := dirStats(framesDir())
		return map[string]any{
			"storage": map[string]any{
				"total_bytes": dataDirSize(), "total": humanBytes(dataDirSize()),
				"db_bytes": dbBytes, "db": humanBytes(dbBytes),
				"wal_bytes": walBytes, "wal": humanBytes(walBytes),
				"frames_bytes": framesBytes, "frames": humanBytes(framesBytes),
				"frame_files": frameFiles, "data_dir": dataDir(),
				"cap_mb": cfg.MaxStorageMB,
			},
			"blocks": map[string]any{
				"total": blocksTotal, "done": blocksDone,
				"failed": blocksFailed, "dead": blocksDead,
				"pending_frames": framesPending,
				"first_day":      firstDay, "last_day": lastDay,
			},
			"events": eventsTotal,
			"api": map[string]any{
				"calls": calls2, "ok": ok2, "failed": failed2,
				"prompt_tokens": pt2, "completion_tokens": ct2,
				"avg_latency_ms": int(avgLat),
			},
			"config": map[string]any{
				"provider": cfg.Provider, "model": cfg.Model,
				"retention_days": cfg.RetentionDays, "keep_frames": cfg.KeepFrames,
				"max_storage_mb": cfg.MaxStorageMB, "debug": cfg.Debug,
			},
		}, nil

	case "get_standup":
		_, j, err := generateStandup(db, cfg, true)
		if err != nil {
			return nil, err
		}
		return j, nil

	case "get_insights":
		r := "week"
		if v, ok := args["range"].(string); ok && v != "" {
			r = v
		}
		now := time.Now()
		var start, end time.Time
		switch r {
		case "day", "today":
			start, end = dayBounds(now)
		case "week":
			start, end = weekBounds(now)
		case "month":
			start, end = monthBounds(now)
		default:
			return nil, fmt.Errorf("range must be day, week, or month")
		}
		in, err := generateInsights(db, cfg, start, end)
		if err != nil {
			return nil, err
		}
		return in.JSON(), nil

	case "chat":
		msg, _ := args["message"].(string)
		if msg == "" {
			return nil, fmt.Errorf("message required")
		}
		convID := int64(0)
		if v, ok := args["conversation_id"]; ok {
			id, err := convIDFromArg(v)
			if err != nil {
				return nil, err
			}
			convID = id
		}
		res, err := chatWithJournal(db, cfg, convID, msg)
		if err != nil {
			return nil, err
		}
		return res.Reply, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func runMCP(cfg Config) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if json.Unmarshal(line, &req) != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			mcpRespond(req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "dayflow", "version": version},
			})
		case "notifications/initialized", "initialized":
			// no response
		case "tools/list":
			mcpRespond(req.ID, map[string]any{"tools": mcpTools})
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				mcpErr(req.ID, -32602, "bad params")
				continue
			}
			res, err := mcpCall(db, cfg, p.Name, p.Arguments)
			if err != nil {
				mcpErr(req.ID, -32000, err.Error())
				continue
			}
			mcpRespond(req.ID, mcpText(res))
		case "ping":
			mcpRespond(req.ID, map[string]any{})
		default:
			if len(req.ID) > 0 {
				mcpErr(req.ID, -32601, "method not found")
			}
		}
	}
	return nil
}

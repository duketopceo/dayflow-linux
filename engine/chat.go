package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Conversation is a chat thread with the journal assistant.
type Conversation struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	CreatedAt int64     `json:"created_at"`
	UpdatedAt int64     `json:"updated_at"`
	Messages  []Message `json:"messages,omitempty"`
}

// Message is one turn in a conversation.
type Message struct {
	ID             int64  `json:"id"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCalls      string `json:"tool_calls,omitempty"`
	CreatedAt      int64  `json:"created_at"`
	ConversationID int64  `json:"conversation_id,omitempty"`
}

// ChatRequest is the request shape for `dayflow chat` when called by tooling.
type ChatRequest struct {
	Message        string `json:"message"`
	ConversationID int64  `json:"conversation_id,omitempty"`
}

// ChatResponse is the result of a journal chat turn.
type ChatResponse struct {
	ConversationID   int64     `json:"conversation_id"`
	Reply            string    `json:"reply"`
	Messages         []Message `json:"messages,omitempty"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
}

// createConversation starts a new conversation with the given title.
func createConversation(db *sql.DB, title string) (int64, error) {
	now := time.Now().Unix()
	if title == "" {
		title = "New conversation"
	}
	res, err := db.Exec(`INSERT INTO chat_conversations(title, created_at, updated_at) VALUES(?,?,?)`,
		title, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// getConversation returns a conversation and its messages, oldest first.
func getConversation(db *sql.DB, id int64) (Conversation, error) {
	var c Conversation
	err := db.QueryRow(`SELECT id, title, created_at, updated_at FROM chat_conversations WHERE id = ?`, id).
		Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	rows, err := db.Query(`SELECT id, role, content, tool_calls, created_at FROM chat_messages
	  WHERE conversation_id = ? ORDER BY created_at ASC, id ASC`, id)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.ToolCalls, &m.CreatedAt); err != nil {
			return c, err
		}
		m.ConversationID = c.ID
		c.Messages = append(c.Messages, m)
	}
	return c, rows.Err()
}

// saveMessage persists a conversation turn.
func saveMessage(db *sql.DB, conversationID int64, role, content, toolCalls string) (int64, error) {
	res, err := db.Exec(`INSERT INTO chat_messages(conversation_id, role, content, tool_calls, created_at)
	  VALUES(?,?,?,?,?)`, conversationID, role, content, toolCalls, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	if _, err := db.Exec(`UPDATE chat_conversations SET updated_at = ? WHERE id = ?`, time.Now().Unix(), conversationID); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// listConversations returns recent conversations, newest first.
func listConversations(db *sql.DB, limit int) ([]Conversation, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`SELECT id, title, created_at, updated_at FROM chat_conversations
	  ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// logLLMCall records a chat LLM call in the llm_calls table.
func logLLMCall(db *sql.DB, task, provider, model string, promptTok, completionTok, latency int, status, errStr string) {
	if db == nil {
		return
	}
	db.Exec(`INSERT INTO llm_calls(ts, task, provider, model, prompt_tokens, completion_tokens, latency_ms, status, error)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		time.Now().Unix(), task, provider, model, promptTok, completionTok, latency, status, errStr)
}

// callChatModel sends the message list to the routed "chat" provider and logs the call.
func callChatModel(db *sql.DB, cfg Config, messages []orMessage) (string, int, int, error) {
	p, err := providerForTask(cfg, "chat")
	if err != nil {
		return "", 0, 0, err
	}
	start := time.Now()
	text, pt, ct, err := callProviderChat(cfg, p, messages)
	latency := int(time.Since(start).Milliseconds())
	status := "ok"
	errStr := ""
	if err != nil {
		status = "failed"
		errStr = err.Error()
	}
	logLLMCall(db, "chat", p.ID, p.Model, pt, ct, latency, status, errStr)
	if err != nil {
		return "", 0, 0, err
	}
	return text, pt, ct, nil
}

const chatSystemPrompt = `You are a helpful assistant for the user's local work journal.

The journal contains time-blocked activity records from their desktop.
You can answer questions about what they did, how long they spent, what apps they used, and trends.

You have access to these tools. To call one, reply with exactly one JSON object (no markdown, no prose) and nothing else:
{"tool":"fetchTimeline","date":"today"}
{"tool":"getStandup"}
{"tool":"getInsights","range":"week"}
{"tool":"searchJournal","query":"<search terms>"}

Tool argument reference:
- fetchTimeline: date can be today, yesterday, week, month, or YYYY-MM-DD.
- getInsights: range can be day, week, or month.
- searchJournal: query is free text searched in titles, summaries, and app names.

After you receive a tool result, answer the user in plain text. Do not make another tool call unless the user's question clearly needs it. Be concise and cite times when possible.`

func buildChatMessages(cfg Config, conv Conversation, userMessage string) []orMessage {
	p, _ := providerForTask(cfg, "chat")
	sys := chatSystemPrompt
	if p.PromptOverrides.ChatPrompt != "" {
		sys = p.PromptOverrides.ChatPrompt + "\n\n" + sys
	}
	sys += fmt.Sprintf("\n\nToday's date is %s.", time.Now().Format("2006-01-02"))

	var messages []orMessage
	messages = append(messages, orMessage{Role: "system", Content: []orContent{{Type: "text", Text: sys}}})

	// Keep the most recent 20 turns so the prompt stays focused.
	history := conv.Messages
	if len(history) > 20 {
		history = history[len(history)-20:]
	}
	for _, m := range history {
		if m.Role == "" {
			continue
		}
		text := m.Content
		if m.Role == "assistant" && m.ToolCalls != "" {
			text = m.ToolCalls
		}
		messages = append(messages, orMessage{
			Role:    m.Role,
			Content: []orContent{{Type: "text", Text: text}},
		})
	}
	if userMessage != "" {
		messages = append(messages, orMessage{
			Role:    "user",
			Content: []orContent{{Type: "text", Text: userMessage}},
		})
	}
	return messages
}

// parseToolCall detects a JSON tool call in model output.
func parseToolCall(raw string) (string, map[string]any, bool) {
	raw = stripFences(raw)
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start == -1 || end == -1 || end <= start {
		return "", nil, false
	}
	candidate := raw[start : end+1]
	var obj map[string]any
	if err := json.Unmarshal([]byte(candidate), &obj); err != nil {
		return "", nil, false
	}
	name, ok := obj["tool"].(string)
	if !ok || name == "" {
		return "", nil, false
	}
	delete(obj, "tool")
	return name, obj, true
}

// executeTool runs a journal tool and returns a JSON-serializable result.
func executeTool(db *sql.DB, cfg Config, name string, args map[string]any) (any, error) {
	switch name {
	case "fetchTimeline":
		d := "today"
		if v, ok := args["date"].(string); ok && v != "" {
			d = v
		}
		return fetchTimeline(db, d)
	case "getStandup":
		_, j, err := generateStandup(db, cfg, true)
		return j, err
	case "getInsights":
		r := "week"
		if v, ok := args["range"].(string); ok && v != "" {
			r = v
		}
		return fetchInsights(db, cfg, r)
	case "searchJournal":
		q := ""
		if v, ok := args["query"].(string); ok {
			q = v
		}
		if q == "" {
			return nil, fmt.Errorf("searchJournal requires a query")
		}
		blocks, err := searchBlocks(db, q)
		if err != nil {
			return nil, err
		}
		return map[string]any{"matches": blocks}, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func fetchTimeline(db *sql.DB, d string) (map[string]any, error) {
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
	return map[string]any{
		"date":   start.Format("2006-01-02"),
		"blocks": blocks,
		"cards":  mergeCards(blocks),
	}, nil
}

func fetchInsights(db *sql.DB, cfg Config, r string) (map[string]any, error) {
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
}

func searchBlocks(db *sql.DB, query string) ([]Block, error) {
	like := "%" + query + "%"
	rows, err := db.Query(`SELECT start_ts, end_ts, title, summary, category, app, activities, productive FROM blocks
	  WHERE status='done' AND (title LIKE ? OR summary LIKE ? OR app LIKE ?)
	  ORDER BY start_ts DESC LIMIT 50`, like, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		var s, e int64
		var acts string
		var prod sql.NullBool
		if err := rows.Scan(&s, &e, &b.Title, &b.Summary, &b.Category, &b.App, &acts, &prod); err != nil {
			return nil, err
		}
		if prod.Valid {
			b.Productive = &prod.Bool
		}
		if acts != "" {
			json.Unmarshal([]byte(acts), &b.Activities)
		}
		b.Start = time.Unix(s, 0).Local()
		b.End = time.Unix(e, 0).Local()
		b.StartTs = s
		b.EndTs = e
		b.StartStr = b.Start.Format("3:04 PM")
		b.EndStr = b.End.Format("3:04 PM")
		b.AppName = appDisplayName(b.App)
		out = append(out, b)
	}
	return out, rows.Err()
}

// chatWithJournal sends a user message and carries out up to 3 tool turns.
func chatWithJournal(db *sql.DB, cfg Config, conversationID int64, userMessage string) (*ChatResponse, error) {
	if strings.TrimSpace(userMessage) == "" {
		return nil, fmt.Errorf("message required")
	}
	if conversationID == 0 {
		title := userMessage
		if len(title) > 60 {
			title = title[:60] + "..."
		}
		id, err := createConversation(db, title)
		if err != nil {
			return nil, err
		}
		conversationID = id
	}
	if _, err := saveMessage(db, conversationID, "user", userMessage, ""); err != nil {
		return nil, err
	}
	conv, err := getConversation(db, conversationID)
	if err != nil {
		return nil, err
	}
	messages := buildChatMessages(cfg, conv, "")

	var reply string
	var totalPT, totalCT int
	for turn := 0; turn < 3; turn++ {
		text, pt, ct, err := callChatModel(db, cfg, messages)
		totalPT += pt
		totalCT += ct
		if err != nil {
			return nil, err
		}
		raw := text
		tool, args, ok := parseToolCall(raw)
		if !ok {
			reply = raw
			if _, err := saveMessage(db, conversationID, "assistant", raw, ""); err != nil {
				return nil, err
			}
			break
		}
		// Save the assistant's tool call.
		if _, err := saveMessage(db, conversationID, "assistant", raw, raw); err != nil {
			return nil, err
		}
		res, err := executeTool(db, cfg, tool, args)
		resJSON, _ := json.Marshal(res)
		if err != nil {
			resJSON = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
		}
		if _, err := saveMessage(db, conversationID, "tool", string(resJSON), ""); err != nil {
			return nil, err
		}
		messages = append(messages,
			orMessage{Role: "assistant", Content: []orContent{{Type: "text", Text: raw}}},
			orMessage{Role: "tool", Content: []orContent{{Type: "text", Text: string(resJSON)}}},
		)
		// After the last allowed turn, if the model returns another tool call,
		// we still want to surface something. The loop will exit and reply stays empty.
		if turn == 2 {
			reply = raw
		}
	}
	conv, _ = getConversation(db, conversationID)
	return &ChatResponse{
		ConversationID:   conversationID,
		Reply:            reply,
		Messages:         conv.Messages,
		PromptTokens:     totalPT,
		CompletionTokens: totalCT,
	}, nil
}

// mcpChatTurn is the bridge used by the MCP server.
func mcpChatTurn(db *sql.DB, cfg Config, req ChatRequest) (*ChatResponse, error) {
	return chatWithJournal(db, cfg, req.ConversationID, req.Message)
}

// convIDFromArg turns an MCP/float conversation id into an int64.
func convIDFromArg(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		return int64(x), nil
	case int64:
		return x, nil
	case string:
		return strconv.ParseInt(x, 10, 64)
	}
	return 0, fmt.Errorf("conversation_id must be an integer")
}

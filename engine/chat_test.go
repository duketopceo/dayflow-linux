package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func chatTestConfig(t *testing.T, srv *httptest.Server) Config {
	cfg := testEnv(t)
	cfg.Providers = []Provider{{
		ID:         "test",
		Name:       "Test",
		Kind:       "custom",
		APIBaseURL: srv.URL + "/v1",
		APIKey:     "test-key",
		Model:      "test-model",
		Enabled:    true,
		Chat:       true,
	}}
	cfg.Routing = Routing{Primary: "test", TaskProvider: map[string]string{"chat": "test"}}
	return cfg
}

func chatTestServer(responses []string) (*httptest.Server, *[]string) {
	var bodies []string
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", 404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		content := ""
		if idx < len(responses) {
			content = responses[idx]
			idx++
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content": %q}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, content)
	}))
	return srv, &bodies
}

func TestCreateConversation(t *testing.T) {
	testEnv(t)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, err := createConversation(db, "First chat")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("expected positive conversation id, got %d", id)
	}
	c, err := getConversation(db, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "First chat" || c.ID != id {
		t.Fatalf("conversation mismatch: %+v", c)
	}
	if c.CreatedAt == 0 || c.UpdatedAt == 0 {
		t.Fatalf("conversation timestamps not set: %+v", c)
	}
}

func TestChatWithJournalPlainText(t *testing.T) {
	srv, bodies := chatTestServer([]string{"Hello!"})
	defer srv.Close()
	cfg := chatTestConfig(t, srv)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	res, err := chatWithJournal(db, cfg, 0, "hi")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "Hello!" {
		t.Fatalf("reply=%q", res.Reply)
	}
	if res.ConversationID == 0 {
		t.Fatal("conversation id not created")
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected 1 provider call, got %d", len(*bodies))
	}
}

func TestChatWithJournalFetchTimeline(t *testing.T) {
	srv, bodies := chatTestServer([]string{
		`{"tool":"fetchTimeline","date":"today"}`,
		"Here is your timeline for today.",
	})
	defer srv.Close()
	cfg := chatTestConfig(t, srv)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	day := time.Now()
	start := time.Date(day.Year(), day.Month(), day.Day(), 9, 0, 0, 0, day.Location())
	end := start.Add(30 * time.Minute)
	tru := true
	if err := upsertBlockFull(db, start, end, "Refactor engine", "Split store.go", "coding", "neovim", "", 2, 0, "done", "", &tru); err != nil {
		t.Fatal(err)
	}

	res, err := chatWithJournal(db, cfg, 0, "What did I do today?")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "Here is your timeline for today." {
		t.Fatalf("reply=%q", res.Reply)
	}
	if len(*bodies) != 2 {
		t.Fatalf("expected 2 provider calls, got %d", len(*bodies))
	}
	// The second request should include a tool result message.
	var req orRequest
	if err := json.Unmarshal([]byte((*bodies)[1]), &req); err != nil {
		t.Fatalf("second request was not valid JSON: %v", err)
	}
	hasTool := false
	hasUser := false
	for _, m := range req.Messages {
		if m.Role == "tool" && strings.Contains(m.Content[0].Text, "Refactor engine") {
			hasTool = true
		}
		if m.Role == "user" {
			hasUser = true
		}
	}
	if !hasTool {
		t.Fatalf("second request did not include a tool result with the block: %s", (*bodies)[1])
	}
	if !hasUser {
		t.Fatal("second request did not include the original user message")
	}
}

func TestChatSearchJournalTool(t *testing.T) {
	srv, _ := chatTestServer([]string{
		`{"tool":"searchJournal","query":"Refactor"}`,
		"I found one matching block.",
	})
	defer srv.Close()
	cfg := chatTestConfig(t, srv)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	day := time.Now()
	start := time.Date(day.Year(), day.Month(), day.Day(), 10, 0, 0, 0, day.Location())
	end := start.Add(30 * time.Minute)
	tru := true
	if err := upsertBlockFull(db, start, end, "Refactor engine", "Split store.go", "coding", "neovim", "", 2, 0, "done", "", &tru); err != nil {
		t.Fatal(err)
	}

	res, err := chatWithJournal(db, cfg, 0, "Find my Refactor work")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reply != "I found one matching block." {
		t.Fatalf("reply=%q", res.Reply)
	}
	// The tool result is saved as a tool message; verify it contains the match.
	found := false
	for _, m := range res.Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "Refactor engine") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool result did not contain the matched block: %+v", res.Messages)
	}
}

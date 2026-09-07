package main

import (
	"strings"
	"testing"
)

func TestChatWithJournalRejectsJSONAnswer(t *testing.T) {
	// If the model returns a JSON object that is not a tool call, it must not be
	// treated as a tool call and should be returned as plain text to the caller.
	srv, _ := chatTestServer([]string{`{"date":"2026-09-07","blocks":[]}`})
	defer srv.Close()
	cfg := chatTestConfig(t, srv)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	res, err := chatWithJournal(db, cfg, 0, "What did I do?")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Reply) != `{"date":"2026-09-07","blocks":[]}` {
		t.Fatalf("reply=%q", res.Reply)
	}
}

func TestChatWithJournalFinalToolTurnProducesPlainAnswer(t *testing.T) {
	// If the model keeps calling tools and uses the last allowed turn for a tool,
	// chatWithJournal must make one final plain-text answer call instead of
	// returning the tool-call JSON to the user.
	srv, bodies := chatTestServer([]string{
		`{"tool":"fetchTimeline","date":"today"}`,
		`{"tool":"getInsights","range":"day"}`,
		`{"tool":"searchJournal","query":"meeting"}`,
		"You worked on a few things today, including a meeting block.",
	})
	defer srv.Close()
	cfg := chatTestConfig(t, srv)
	db, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	res, err := chatWithJournal(db, cfg, 0, "What did I do today?")
	if err != nil {
		t.Fatal(err)
	}
	want := "You worked on a few things today, including a meeting block."
	if res.Reply != want {
		t.Fatalf("reply=%q, want %q", res.Reply, want)
	}
	if len(*bodies) != 4 {
		t.Fatalf("expected 4 provider calls, got %d", len(*bodies))
	}
	// The final call should include the plain-text instruction.
	last := (*bodies)[3]
	if !strings.Contains(last, "plain English") {
		t.Fatalf("final call did not ask for plain English: %s", last)
	}
}

package main

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// benchEnv returns a default config rooted in a temp directory.
func benchEnv(b *testing.B) Config {
	b.Helper()
	dir := b.TempDir()
	b.Setenv("DAYFLOW_DATA_DIR", dir)
	b.Setenv("DAYFLOW_CONFIG", dir+"/nonexistent.json")
	cfg := defaultConfig()
	cfg.OpenRouterAPIKey = "test-key"
	return cfg
}

func benchmarkBlocks(b *testing.B, count int) (*sql.DB, Config) {
	b.Helper()
	cfg := benchEnv(b)
	db, err := openDB()
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.Local)
	cats := []string{"coding", "meetings", "browsing", "writing", "communication"}
	apps := []string{"neovim", "brave", "slack", "cursor"}
	truePtr := true
	falsePtr := false
	for i := 0; i < count; i++ {
		start := now.Add(time.Duration(i) * 15 * time.Minute)
		end := start.Add(15 * time.Minute)
		cat := cats[i%len(cats)]
		app := apps[i%len(apps)]
		prod := &truePtr
		if cat == "browsing" {
			prod = &falsePtr
		}
		title := fmt.Sprintf("Activity %d", i)
		summary := fmt.Sprintf("Summary for activity %d", i)
		if err := upsertBlockFull(db, start, end, title, summary, cat, app, "", 0, 0, "done", "", prod); err != nil {
			b.Fatal(err)
		}
	}
	return db, cfg
}

func BenchmarkBlocksBetweenDay(b *testing.B) {
	db, _ := benchmarkBlocks(b, 5000)
	defer db.Close()
	start, end := dayBounds(time.Date(2026, 9, 5, 0, 0, 0, 0, time.Local))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := blocksBetween(db, start, end); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSearchBlocks(b *testing.B) {
	db, _ := benchmarkBlocks(b, 5000)
	defer db.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := searchBlocks(db, "Activity 2500"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateInsights(b *testing.B) {
	db, cfg := benchmarkBlocks(b, 5000)
	defer db.Close()
	start, end := weekBounds(time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := generateInsights(db, cfg, start, end); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateStandup(b *testing.B) {
	db, cfg := benchmarkBlocks(b, 5000)
	defer db.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := generateStandup(db, cfg, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGenerateWeeklyPayload(b *testing.B) {
	db, cfg := benchmarkBlocks(b, 5000)
	defer db.Close()
	start, end := weekBounds(time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := generateWeeklyPayload(db, cfg, start, end); err != nil {
			b.Fatal(err)
		}
	}
}

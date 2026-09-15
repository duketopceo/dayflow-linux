package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeEndpoint(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer up.Close()

	if !probeEndpoint(up.URL + "/v1") {
		t.Fatal("expected probe to succeed against live endpoint")
	}
	if probeEndpoint(up.URL + "/wrong") {
		t.Fatal("404 endpoint must not count as detected")
	}

	dead := httptest.NewServer(nil)
	url := dead.URL
	dead.Close()
	start := time.Now()
	if probeEndpoint(url) {
		t.Fatal("expected probe to fail against dead endpoint")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("dead-endpoint probe should fail fast")
	}
}

func TestCollectDoctorChecks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DAYFLOW_DATA_DIR", dir)
	t.Setenv("DAYFLOW_CONFIG", dir+"/config.json")

	checks, _ := collectDoctorChecks(Config{Model: "google/gemma-4-31b-it"})
	if len(checks) == 0 {
		t.Fatal("expected at least one check")
	}
	for _, c := range checks {
		if c.Name == "" {
			t.Fatal("check missing name")
		}
		switch c.Status {
		case "ok", "warn", "fail":
		default:
			t.Fatalf("check %q has invalid status %q", c.Name, c.Status)
		}
	}
	// JSON shape must round-trip for panel consumption.
	b, err := json.Marshal(checks)
	if err != nil {
		t.Fatal(err)
	}
	var back []map[string]interface{}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal("doctor checks must marshal to JSON")
	}
}

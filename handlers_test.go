package main

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
)

type recordingHeartbeatTracker struct {
	events []HeartbeatEvent
}

func (t *recordingHeartbeatTracker) TrackHeartbeat(event HeartbeatEvent) {
	t.events = append(t.events, event)
}

func TestHeartbeatHandlerTracksSuccessfulHeartbeat(t *testing.T) {
	t.Setenv("TRUST_PROXY", "false")
	resetHeartbeatLimits(t)

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec(`
		CREATE TABLE instances (
			id TEXT PRIMARY KEY,
			first_seen DATETIME NOT NULL,
			last_seen DATETIME NOT NULL,
			latest_version TEXT NOT NULL,
			server_type TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("create instances table: %v", err)
	}

	tracker := &recordingHeartbeatTracker{}
	body := bytes.NewBufferString(`{
		"instance_id":"b316815f-5f81-488f-89f8-12b62013dfa4",
		"version":"1.2.3",
		"server_type":"agent"
	}`)
	request := httptest.NewRequest(http.MethodPost, "/heartbeat", body)
	request.RemoteAddr = "203.0.113.10:4321"
	request.Header.Set("User-Agent", "Arcane/1.2.3")
	response := httptest.NewRecorder()

	HeartbeatHandler(db, tracker).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if len(tracker.events) != 1 {
		t.Fatalf("expected one tracked event, got %d", len(tracker.events))
	}

	event := tracker.events[0]
	if event.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %q", event.Version)
	}
	if event.ServerType != "agent" {
		t.Errorf("expected server type agent, got %q", event.ServerType)
	}
	if event.UserAgent != "Arcane/1.2.3" {
		t.Errorf("expected forwarded user agent, got %q", event.UserAgent)
	}
	if event.ClientIP != "203.0.113.10" {
		t.Errorf("expected client IP 203.0.113.10, got %q", event.ClientIP)
	}

	var storedVersion string
	if err := db.QueryRow(`SELECT latest_version FROM instances WHERE id = ?`, "b316815f-5f81-488f-89f8-12b62013dfa4").Scan(&storedVersion); err != nil {
		t.Fatalf("query stored instance: %v", err)
	}
	if storedVersion != "1.2.3" {
		t.Errorf("expected stored version 1.2.3, got %q", storedVersion)
	}
}

func resetHeartbeatLimits(t *testing.T) {
	t.Helper()
	heartbeatClientsMu.Lock()
	clear(heartbeatClients)
	heartbeatClientsMu.Unlock()
	t.Cleanup(func() {
		heartbeatClientsMu.Lock()
		clear(heartbeatClients)
		heartbeatClientsMu.Unlock()
	})
}

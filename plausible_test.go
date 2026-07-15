package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/andrerfcsantos/go-plausible/plausible"
)

type recordingPlausibleClient struct {
	event      plausible.EventRequest
	events     []plausible.EventRequest
	err        error
	failOnCall int
}

func (c *recordingPlausibleClient) PushEvent(event plausible.EventRequest) ([]byte, error) {
	c.event = event
	c.events = append(c.events, event)
	if c.err != nil && (c.failOnCall == 0 || len(c.events) == c.failOnCall) {
		return nil, c.err
	}
	return nil, nil
}

func TestPlausibleTrackerSendsHeartbeatEvent(t *testing.T) {
	client := &recordingPlausibleClient{}
	tracker := &plausibleHeartbeatTracker{
		client: client,
		config: plausibleConfig{
			Domain:   "analytics.getarcane.app",
			EventURL: "https://analytics.getarcane.app/heartbeat",
		},
	}

	err := tracker.sendHeartbeat(HeartbeatEvent{
		Version:    "1.2.3",
		ServerType: "manager",
		UserAgent:  "Arcane/1.2.3",
		ClientIP:   "198.51.100.20",
	})
	if err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	event := client.event
	if event.Domain != "analytics.getarcane.app" {
		t.Errorf("expected domain analytics.getarcane.app, got %q", event.Domain)
	}
	if event.Name != "Heartbeat" {
		t.Errorf("expected event name Heartbeat, got %q", event.Name)
	}
	if event.URL != "https://analytics.getarcane.app/heartbeat" {
		t.Errorf("unexpected event URL %q", event.URL)
	}
	if event.UserAgent != "Arcane/1.2.3" {
		t.Errorf("expected user agent Arcane/1.2.3, got %q", event.UserAgent)
	}
	if event.XForwardedFor != "198.51.100.20" {
		t.Errorf("expected forwarded IP 198.51.100.20, got %q", event.XForwardedFor)
	}
	if event.Props["version"] != "1.2.3" {
		t.Errorf("expected version property 1.2.3, got %q", event.Props["version"])
	}
	if event.Props["server_type"] != "manager" {
		t.Errorf("expected server_type property manager, got %q", event.Props["server_type"])
	}
	if event.Props["source"] != "live" {
		t.Errorf("expected live source property, got %q", event.Props["source"])
	}
	if _, exists := event.Props["instance_id"]; exists {
		t.Error("instance_id must not be sent to Plausible")
	}
}

func TestPlausibleTrackerDefaultsOptionalEventFields(t *testing.T) {
	client := &recordingPlausibleClient{}
	tracker := &plausibleHeartbeatTracker{
		client: client,
		config: plausibleConfig{
			Domain:   "analytics.getarcane.app",
			EventURL: "https://analytics.getarcane.app/heartbeat",
		},
	}

	if err := tracker.sendHeartbeat(HeartbeatEvent{Version: "1.2.3", ClientIP: "not-an-ip"}); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	if client.event.UserAgent != "Arcane Analytics Heartbeat" {
		t.Errorf("unexpected fallback user agent %q", client.event.UserAgent)
	}
	if client.event.XForwardedFor != "" {
		t.Errorf("expected invalid IP to be omitted, got %q", client.event.XForwardedFor)
	}
	if client.event.Props["server_type"] != "unknown" {
		t.Errorf("expected unknown server type, got %q", client.event.Props["server_type"])
	}
}

func TestPlausibleTrackerReturnsClientError(t *testing.T) {
	expected := errors.New("plausible unavailable")
	client := &recordingPlausibleClient{err: expected}
	tracker := &plausibleHeartbeatTracker{
		client: client,
		config: plausibleConfig{Domain: "analytics.getarcane.app", EventURL: "https://analytics.getarcane.app/heartbeat"},
	}

	if err := tracker.sendHeartbeat(HeartbeatEvent{Version: "1.2.3"}); !errors.Is(err, expected) {
		t.Fatalf("expected client error, got %v", err)
	}
}

func TestLoadPlausibleConfig(t *testing.T) {
	t.Setenv("PLAUSIBLE_BASE_URL", "https://plausible.example.com/api/v1/")
	t.Setenv("PLAUSIBLE_API_TOKEN", "token")
	t.Setenv("PLAUSIBLE_DOMAIN", "analytics.getarcane.app")
	t.Setenv("PLAUSIBLE_EVENT_URL", "")

	config, enabled, err := loadPlausibleConfigFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !enabled {
		t.Fatal("expected Plausible integration to be enabled")
	}
	if config.EventURL != "https://analytics.getarcane.app/heartbeat" {
		t.Errorf("unexpected default event URL %q", config.EventURL)
	}
}

func TestLoadPlausibleConfigRejectsPartialConfiguration(t *testing.T) {
	t.Setenv("PLAUSIBLE_BASE_URL", "https://plausible.example.com/api/v1/")
	t.Setenv("PLAUSIBLE_DOMAIN", "")

	_, _, err := loadPlausibleConfigFromEnv()
	if err == nil {
		t.Fatal("expected partial Plausible configuration to fail")
	}
}

func TestPlausibleBackfillSendsExistingInstancesOnce(t *testing.T) {
	db := newPlausibleBackfillTestDB(t)
	firstSeen := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	lastSeen := firstSeen.Add(24 * time.Hour)
	insertBackfillTestInstance(t, db, "instance-1", firstSeen, lastSeen, "1.2.3", "manager")
	insertBackfillTestInstance(t, db, "instance-2", firstSeen.Add(time.Hour), lastSeen.Add(time.Hour), "1.2.4", "agent")

	client := &recordingPlausibleClient{}
	tracker := newBackfillTestTracker(client, "analytics.getarcane.app")

	count, err := tracker.Backfill(context.Background(), db)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 backfilled instances, got %d", count)
	}
	if len(client.events) != 2 {
		t.Fatalf("expected 2 Plausible events, got %d", len(client.events))
	}

	event := client.events[0]
	if event.Props["source"] != "backfill" {
		t.Errorf("expected backfill source, got %q", event.Props["source"])
	}
	if event.Props["first_seen"] != firstSeen.Format(time.RFC3339) {
		t.Errorf("unexpected first_seen %q", event.Props["first_seen"])
	}
	if event.Props["last_seen"] != lastSeen.Format(time.RFC3339) {
		t.Errorf("unexpected last_seen %q", event.Props["last_seen"])
	}
	if _, exists := event.Props["instance_id"]; exists {
		t.Error("instance_id must not be sent in a backfill event")
	}

	count, err = tracker.Backfill(context.Background(), db)
	if err != nil {
		t.Fatalf("repeat backfill: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected repeat backfill to send 0 instances, got %d", count)
	}
	if len(client.events) != 2 {
		t.Fatalf("repeat backfill sent duplicate events; got %d total", len(client.events))
	}
}

func TestPlausibleBackfillExcludesInstancesCreatedAfterInitialCutoff(t *testing.T) {
	db := newPlausibleBackfillTestDB(t)
	client := &recordingPlausibleClient{}
	tracker := newBackfillTestTracker(client, "analytics.getarcane.app")

	if count, err := tracker.Backfill(context.Background(), db); err != nil || count != 0 {
		t.Fatalf("initialize empty backfill: count=%d err=%v", count, err)
	}

	createdAfterCutoff := time.Now().UTC().Add(time.Minute)
	insertBackfillTestInstance(t, db, "new-live-instance", createdAfterCutoff, createdAfterCutoff, "2.0.0", "agent")

	if count, err := tracker.Backfill(context.Background(), db); err != nil || count != 0 {
		t.Fatalf("backfill after new live instance: count=%d err=%v", count, err)
	}
	if len(client.events) != 0 {
		t.Fatalf("expected no backfill events for a new live instance, got %d", len(client.events))
	}
}

func TestPlausibleBackfillResumesAfterFailure(t *testing.T) {
	db := newPlausibleBackfillTestDB(t)
	seen := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	insertBackfillTestInstance(t, db, "instance-1", seen, seen, "1.0.0", "manager")
	insertBackfillTestInstance(t, db, "instance-2", seen.Add(time.Hour), seen.Add(time.Hour), "1.0.1", "agent")

	client := &recordingPlausibleClient{err: errors.New("plausible unavailable"), failOnCall: 2}
	tracker := newBackfillTestTracker(client, "analytics.getarcane.app")

	count, err := tracker.Backfill(context.Background(), db)
	if err == nil {
		t.Fatal("expected backfill failure")
	}
	if count != 1 {
		t.Fatalf("expected one checkpointed instance before failure, got %d", count)
	}

	client.err = nil
	client.failOnCall = 0
	count, err = tracker.Backfill(context.Background(), db)
	if err != nil {
		t.Fatalf("resume backfill: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected resumed backfill to send only the remaining instance, got %d", count)
	}
}

func newPlausibleBackfillTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	db.SetMaxOpenConns(1)
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
	if err := initPlausibleBackfillSchema(db); err != nil {
		t.Fatalf("create backfill schema: %v", err)
	}
	return db
}

func insertBackfillTestInstance(t *testing.T, db *sql.DB, id string, firstSeen, lastSeen time.Time, version, serverType string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO instances (id, first_seen, last_seen, latest_version, server_type)
		VALUES (?, ?, ?, ?, ?)
	`, id, firstSeen, lastSeen, version, serverType)
	if err != nil {
		t.Fatalf("insert test instance: %v", err)
	}
}

func newBackfillTestTracker(client plausibleEventPusher, domain string) *plausibleHeartbeatTracker {
	return &plausibleHeartbeatTracker{
		client: client,
		config: plausibleConfig{
			Domain:   domain,
			EventURL: "https://analytics.getarcane.app/heartbeat",
		},
	}
}

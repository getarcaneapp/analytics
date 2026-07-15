package main

import (
	"errors"
	"testing"

	"github.com/andrerfcsantos/go-plausible/plausible"
)

type recordingPlausibleClient struct {
	event plausible.EventRequest
	err   error
}

func (c *recordingPlausibleClient) PushEvent(event plausible.EventRequest) ([]byte, error) {
	c.event = event
	return nil, c.err
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

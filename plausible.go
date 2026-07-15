package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/andrerfcsantos/go-plausible/plausible"
)

const (
	plausibleEventQueueSize     = 256
	plausibleBackfillBatchSize  = 100
	plausibleBackfillRetryDelay = time.Minute
)

type HeartbeatEvent struct {
	Version    string
	ServerType string
	UserAgent  string
	ClientIP   string
	FirstSeen  time.Time
	LastSeen   time.Time
	IsBackfill bool
}

type HeartbeatEventTracker interface {
	TrackHeartbeat(event HeartbeatEvent)
}

type PlausibleTracker interface {
	HeartbeatEventTracker
	PrepareBackfill(context.Context, *sql.DB) error
	Backfill(context.Context, *sql.DB) (int, error)
}

type plausibleEventPusher interface {
	PushEvent(event plausible.EventRequest) ([]byte, error)
}

type plausibleConfig struct {
	BaseURL  string
	Token    string
	Domain   string
	EventURL string
}

type plausibleHeartbeatTracker struct {
	client plausibleEventPusher
	config plausibleConfig
	queue  chan HeartbeatEvent
}

func NewPlausibleHeartbeatTrackerFromEnv() (PlausibleTracker, error) {
	config, enabled, err := loadPlausibleConfigFromEnv()
	if err != nil || !enabled {
		return nil, err
	}

	client := plausible.NewClientWithBaseURL(config.Token, config.BaseURL)
	tracker := &plausibleHeartbeatTracker{
		client: client,
		config: config,
		queue:  make(chan HeartbeatEvent, plausibleEventQueueSize),
	}
	go tracker.run()

	return tracker, nil
}

func loadPlausibleConfigFromEnv() (plausibleConfig, bool, error) {
	config := plausibleConfig{
		BaseURL:  strings.TrimSpace(os.Getenv("PLAUSIBLE_BASE_URL")),
		Token:    strings.TrimSpace(os.Getenv("PLAUSIBLE_API_TOKEN")),
		Domain:   strings.TrimSpace(os.Getenv("PLAUSIBLE_DOMAIN")),
		EventURL: strings.TrimSpace(os.Getenv("PLAUSIBLE_EVENT_URL")),
	}

	if config.BaseURL == "" && config.Domain == "" {
		return plausibleConfig{}, false, nil
	}
	if config.BaseURL == "" || config.Domain == "" {
		return plausibleConfig{}, false, fmt.Errorf("PLAUSIBLE_BASE_URL and PLAUSIBLE_DOMAIN must both be set")
	}
	if err := validatePlausibleURL("PLAUSIBLE_BASE_URL", config.BaseURL); err != nil {
		return plausibleConfig{}, false, err
	}
	if config.EventURL == "" {
		config.EventURL = (&url.URL{
			Scheme: "https",
			Host:   config.Domain,
			Path:   "/heartbeat",
		}).String()
	}
	if err := validatePlausibleURL("PLAUSIBLE_EVENT_URL", config.EventURL); err != nil {
		return plausibleConfig{}, false, err
	}

	return config, true, nil
}

func validatePlausibleURL(name, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute http or https URL", name)
	}
	return nil
}

func (t *plausibleHeartbeatTracker) TrackHeartbeat(event HeartbeatEvent) {
	select {
	case t.queue <- event:
	default:
		log.Printf("Plausible event queue is full; dropping heartbeat event")
	}
}

func (t *plausibleHeartbeatTracker) run() {
	for event := range t.queue {
		if err := t.sendHeartbeat(event); err != nil {
			log.Printf("Error sending heartbeat to Plausible: %v", err)
		}
	}
}

func (t *plausibleHeartbeatTracker) PrepareBackfill(ctx context.Context, db *sql.DB) error {
	_, err := GetPlausibleBackfillCutoff(ctx, db, t.config.Domain)
	return err
}

func runPlausibleBackfill(ctx context.Context, tracker PlausibleTracker, db *sql.DB) {
	for {
		backfilled, err := tracker.Backfill(ctx, db)
		if err == nil {
			log.Printf("Plausible backfill complete: %d instances sent", backfilled)
			return
		}

		log.Printf("Plausible backfill stopped after %d instances: %v; retrying in %s", backfilled, err, plausibleBackfillRetryDelay)
		timer := time.NewTimer(plausibleBackfillRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (t *plausibleHeartbeatTracker) Backfill(ctx context.Context, db *sql.DB) (int, error) {
	cutoff, err := GetPlausibleBackfillCutoff(ctx, db, t.config.Domain)
	if err != nil {
		return 0, err
	}

	backfilled := 0
	for {
		instances, err := GetPendingPlausibleBackfillInstances(
			ctx,
			db,
			t.config.Domain,
			cutoff,
			plausibleBackfillBatchSize,
		)
		if err != nil {
			return backfilled, err
		}
		if len(instances) == 0 {
			return backfilled, nil
		}

		for _, instance := range instances {
			if err := t.sendHeartbeat(HeartbeatEvent{
				Version:    instance.Version,
				ServerType: instance.ServerType,
				FirstSeen:  instance.FirstSeen,
				LastSeen:   instance.LastSeen,
				IsBackfill: true,
			}); err != nil {
				return backfilled, fmt.Errorf("failed to backfill instance: %w", err)
			}
			if err := MarkPlausibleInstanceBackfilled(ctx, db, t.config.Domain, instance.ID); err != nil {
				return backfilled, err
			}
			backfilled++
		}
	}
}

func (t *plausibleHeartbeatTracker) sendHeartbeat(event HeartbeatEvent) error {
	userAgent := strings.TrimSpace(event.UserAgent)
	if userAgent == "" {
		userAgent = "Arcane Analytics Heartbeat"
	}

	serverType := strings.TrimSpace(event.ServerType)
	if serverType == "" {
		serverType = "unknown"
	}

	forwardedFor := ""
	if net.ParseIP(event.ClientIP) != nil {
		forwardedFor = event.ClientIP
	}

	props := map[string]string{
		"version":     event.Version,
		"server_type": serverType,
		"source":      "live",
	}
	if event.IsBackfill {
		props["source"] = "backfill"
		props["first_seen"] = event.FirstSeen.UTC().Format(time.RFC3339)
		props["last_seen"] = event.LastSeen.UTC().Format(time.RFC3339)
	}

	_, err := t.client.PushEvent(plausible.EventRequest{
		EventData: plausible.EventData{
			Domain: t.config.Domain,
			Name:   "Heartbeat",
			URL:    t.config.EventURL,
			Props:  props,
		},
		UserAgent:     userAgent,
		XForwardedFor: forwardedFor,
	})
	return err
}

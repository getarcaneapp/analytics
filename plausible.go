package main

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/andrerfcsantos/go-plausible/plausible"
)

const plausibleEventQueueSize = 256

type HeartbeatEvent struct {
	Version    string
	ServerType string
	UserAgent  string
	ClientIP   string
}

type HeartbeatEventTracker interface {
	TrackHeartbeat(event HeartbeatEvent)
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

func NewPlausibleHeartbeatTrackerFromEnv() (HeartbeatEventTracker, error) {
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

	_, err := t.client.PushEvent(plausible.EventRequest{
		EventData: plausible.EventData{
			Domain: t.config.Domain,
			Name:   "Heartbeat",
			URL:    t.config.EventURL,
			Props: map[string]string{
				"version":     event.Version,
				"server_type": serverType,
			},
		},
		UserAgent:     userAgent,
		XForwardedFor: forwardedFor,
	})
	return err
}

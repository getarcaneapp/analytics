package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var statsCache = NewTTLCache[InstancesStats](15 * time.Minute)

type HeartbeatRequest struct {
	InstanceID string `json:"instance_id"`
	Version    string `json:"version"`
	ServerType string `json:"server_type"`
}

func HeartbeatHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req HeartbeatRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Validate request
		if req.InstanceID == "" || req.Version == "" {
			http.Error(w, "instance_id and version are required", http.StatusBadRequest)
			return
		}

		if req.ServerType != "" && req.ServerType != "manager" && req.ServerType != "agent" {
			http.Error(w, "server_type must be 'manager' or 'agent'", http.StatusBadRequest)
			return
		}

		instanceExists, err := DoesInstanceExist(r.Context(), db, req.InstanceID)
		if err != nil {
			log.Printf("Error checking instance existence: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Check rate limit
		clientIP := getClientIP(r)
		if !IsAllowedToCreateHeartbeat(clientIP, instanceExists) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Store/update instance
		err = UpsertInstance(r.Context(), db, req.InstanceID, req.Version, req.ServerType)
		if err != nil {
			log.Printf("Error upserting instance: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		log.Printf("Heartbeat received: Instance %s, Version %s", req.InstanceID, req.Version)

		w.WriteHeader(http.StatusOK)
	}
}

func StatsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Add CORS headers to allow all origins
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight OPTIONS request
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		timeframe := r.URL.Query().Get("timeframe")
		if timeframe != "daily" && timeframe != "monthly" {
			timeframe = "daily"
		}

		cacheKey := "stats_" + timeframe

		// Check cache first
		if cachedData, found := statsCache.Get(cacheKey); found {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(cachedData)
			return
		}

		// Cache miss, fetch fresh data
		totalInstances, err := GetTotalInstances(r.Context(), db)
		if err != nil {
			log.Printf("Error getting total instances: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		inactiveInstances, err := GetInactiveInstances(r.Context(), db)
		if err != nil {
			log.Printf("Error getting inactive instances: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		instancesByType, err := GetInstancesByType(r.Context(), db)
		if err != nil {
			log.Printf("Error getting instances by type: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		instancesByVersion, err := GetInstancesByVersion(r.Context(), db)
		if err != nil {
			log.Printf("Error getting instances by version: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		chartData, err := GetInstancesOverTime(r.Context(), db, timeframe)
		if err != nil {
			log.Printf("Error getting chart data: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		data := InstancesStats{
			Total:     totalInstances,
			Inactive:  inactiveInstances,
			ByType:    instancesByType,
			ByVersion: instancesByVersion,
			History:   chartData,
		}

		// Store in cache
		statsCache.Set(cacheKey, data)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

func getClientIP(r *http.Request) string {
	trustProxy := os.Getenv("TRUST_PROXY") == "true"
	if trustProxy {
		xForwardedFor := r.Header.Get("X-Forwarded-For")
		if xForwardedFor != "" {
			ips := strings.Split(xForwardedFor, ",")
			clientIP := strings.TrimSpace(ips[0])
			if clientIP != "" {
				return clientIP
			}
		}

		cfConnectingIP := r.Header.Get("CF-Connecting-IP")
		if cfConnectingIP != "" {
			return cfConnectingIP
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

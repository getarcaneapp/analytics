package main

import (
	"context"
	"log"
	"net/http"
	"os"
)

func main() {
	// Initialize database
	db, err := initDB()
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close()

	plausibleTracker, err := NewPlausibleHeartbeatTrackerFromEnv()
	if err != nil {
		log.Fatal("Failed to configure Plausible:", err)
	}
	if plausibleTracker != nil {
		log.Printf("Plausible heartbeat forwarding enabled for %s", os.Getenv("PLAUSIBLE_DOMAIN"))
		if err := plausibleTracker.PrepareBackfill(context.Background(), db); err != nil {
			log.Printf("Failed to prepare Plausible backfill: %v", err)
		} else {
			go runPlausibleBackfill(context.Background(), plausibleTracker, db)
		}
	}

	// Set up HTTP handlers
	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	http.HandleFunc("POST /heartbeat", func(w http.ResponseWriter, r *http.Request) {
		HeartbeatHandler(db, plausibleTracker)(w, r)
	})

	http.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		StatsHandler(db)(w, r)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server starting on :" + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

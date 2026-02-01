package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

type InstancesStats struct {
	Total     int                `json:"total"`
	Inactive  int                `json:"inactive"`
	ByType    map[string]int     `json:"by_type"`
	ByVersion map[string]int     `json:"by_version"`
	History   []InstancesHistory `json:"history"`
}

type InstancesHistory struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func DoesInstanceExist(parentCtx context.Context, db *sql.DB, instanceID string) (bool, error) {
	const query = `
	SELECT EXISTS(SELECT 1 FROM instances WHERE id = ?)
	`

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	var exists bool
	err := db.QueryRowContext(ctx, query, instanceID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check instance existence: %w", err)
	}

	return exists, nil
}

func UpsertInstance(parentCtx context.Context, db *sql.DB, instanceID, version, serverType string) error {
	now := time.Now()

	// Upsert the instance
	const query = `
	INSERT INTO instances (id, first_seen, last_seen, latest_version, server_type)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		last_seen = excluded.last_seen,
		latest_version = excluded.latest_version,
		server_type = CASE
			WHEN excluded.server_type IS NULL OR excluded.server_type = '' THEN instances.server_type
			ELSE excluded.server_type
		END
	`

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	_, err := db.ExecContext(
		ctx,
		query,
		instanceID, now, now, version, serverType,
	)

	return err
}

func GetTotalInstances(parentCtx context.Context, db *sql.DB) (int, error) {
	// Only count instances that have been active in the last 2 days.
	const query = `
	SELECT COUNT(*) 
	FROM instances 
	WHERE last_seen >= datetime('now', '-2 days')
	`

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	var count int
	err := db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

func GetInactiveInstances(parentCtx context.Context, db *sql.DB) (int, error) {
	const query = `
	SELECT COUNT(*) 
	FROM instances 
	WHERE last_seen < datetime('now', '-2 days')
	`

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	var count int
	err := db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

func GetInstancesByType(parentCtx context.Context, db *sql.DB) (map[string]int, error) {
	const query = `
	SELECT 
		CASE 
			WHEN server_type IS NULL OR server_type = '' THEN 'unknown'
			ELSE server_type
		END as server_type,
		COUNT(*) as count
	FROM instances
	WHERE last_seen >= datetime('now', '-2 days')
	GROUP BY CASE 
		WHEN server_type IS NULL OR server_type = '' THEN 'unknown'
		ELSE server_type
	END
	`

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var serverType string
		var count int
		if err := rows.Scan(&serverType, &count); err != nil {
			return nil, err
		}
		counts[serverType] = count
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return counts, nil
}

func GetInstancesByVersion(parentCtx context.Context, db *sql.DB) (map[string]int, error) {
	const query = `
	SELECT 
		latest_version as version,
		COUNT(*) as count
	FROM instances
	WHERE last_seen >= datetime('now', '-2 days')
	GROUP BY latest_version
	`

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var version string
		var count int
		if err := rows.Scan(&version, &count); err != nil {
			return nil, err
		}
		counts[version] = count
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return counts, nil
}

func GetInstancesOverTime(parentCtx context.Context, db *sql.DB, timeframe string) ([]InstancesHistory, error) {
	var query string

	switch timeframe {
	case "daily":
		// Get daily instance counts for the last 30 days
		// Only include instances that were active in the last 2 days
		query = `
		SELECT 
			DATE(first_seen) as date,
			COUNT(*) as daily_new,
			(SELECT COUNT(*) 
			 FROM instances i2 
			 WHERE DATE(i2.first_seen) <= DATE(i1.first_seen)
			 AND i2.last_seen >= datetime('now', '-2 days')) as cumulative_count
		FROM instances i1
		WHERE first_seen >= datetime('now', '-30 days')
		AND last_seen >= datetime('now', '-2 days')
		GROUP BY DATE(first_seen)
		ORDER BY date
		`
	case "monthly":
		// Get monthly instance counts for all time
		// Only include instances that were active in the last 2 days
		query = `
		SELECT 
			strftime('%Y-%m', first_seen) as date,
			COUNT(*) as monthly_new,
			(SELECT COUNT(*) 
			 FROM instances i2 
			 WHERE strftime('%Y-%m', i2.first_seen) <= strftime('%Y-%m', i1.first_seen)
			 AND i2.last_seen >= datetime('now', '-2 days')) as cumulative_count
		FROM instances i1
		WHERE last_seen >= datetime('now', '-2 days')
		GROUP BY strftime('%Y-%m', first_seen)
		ORDER BY date
		`
	default:
		return nil, fmt.Errorf("invalid timeframe: %s. Use 'daily' or 'monthly'", timeframe)
	}

	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	chartData := make([]InstancesHistory, 0, 36)
	for rows.Next() {
		var date string
		var newCount, cumulativeCount int

		err := rows.Scan(&date, &newCount, &cumulativeCount)
		if err != nil {
			return nil, err
		}

		chartData = append(chartData, InstancesHistory{
			Date:  date,
			Count: cumulativeCount,
		})
	}

	return chartData, nil
}

func initDB() (*sql.DB, error) {
	if err := os.MkdirAll("./data", 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", "./data/pocket-id-analytics.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_txlock=immediate")
	if err != nil {
		return nil, err
	}

	// Create instances table
	createTableSQL := `
    CREATE TABLE IF NOT EXISTS instances (
        id TEXT PRIMARY KEY,
        first_seen DATETIME NOT NULL,
        last_seen DATETIME NOT NULL,
        latest_version TEXT NOT NULL,
        server_type TEXT NOT NULL DEFAULT ''
    );

    CREATE INDEX IF NOT EXISTS idx_first_seen ON instances(first_seen);
    CREATE INDEX IF NOT EXISTS idx_last_seen ON instances(last_seen);
    `

	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, err
	}

	_, _ = db.Exec(`ALTER TABLE instances ADD COLUMN server_type TEXT DEFAULT ''`)

	return db, nil
}

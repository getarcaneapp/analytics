package main

import (
	"maps"
	"testing"
	"time"
)

func TestDatabaseStatsAndTimestampCompatibility(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := initDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	if err := UpsertInstance(ctx, db, "active", "1.2.3", "manager"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertInstance(ctx, db, "active", "1.2.4", ""); err != nil {
		t.Fatal(err)
	}
	insertBackfillTestInstance(t, db, "inactive", time.Now().Add(-96*time.Hour), time.Now().Add(-72*time.Hour), "1.0.0", "agent")
	for _, id := range []string{"active", "missing"} {
		exists, err := DoesInstanceExist(ctx, db, id)
		if err != nil || exists != (id == "active") {
			t.Fatalf("exists(%q) = %v, %v", id, exists, err)
		}
	}
	if total, err := GetTotalInstances(ctx, db); err != nil || total != 1 {
		t.Fatalf("total = %d, %v; want 1", total, err)
	}
	if inactive, err := GetInactiveInstances(ctx, db); err != nil || inactive != 1 {
		t.Fatalf("inactive = %d, %v; want 1", inactive, err)
	}
	if counts, err := GetInstancesByType(ctx, db); err != nil || !maps.Equal(counts, map[string]int{"manager": 1}) {
		t.Fatalf("types = %v, %v", counts, err)
	}
	if counts, err := GetInstancesByVersion(ctx, db); err != nil || !maps.Equal(counts, map[string]int{"1.2.4": 1}) {
		t.Fatalf("versions = %v, %v", counts, err)
	}
	for _, timeframe := range []string{"daily", "monthly"} {
		history, err := GetInstancesOverTime(ctx, db, timeframe)
		if err != nil || len(history) != 1 || history[0].Count != 1 || history[0].Date == "" {
			t.Fatalf("%s history = %v, %v", timeframe, history, err)
		}
	}
	cutoff, err := GetPlausibleBackfillCutoff(ctx, db, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	instances, err := GetPendingPlausibleBackfillInstances(ctx, db, "example.com", cutoff, 10)
	if err != nil || len(instances) != 2 {
		t.Fatalf("backfill = %v, %v; want two instances", instances, err)
	}
}

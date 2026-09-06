package sqlutil

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"testing"

	_ "modernc.org/sqlite"
)

func TestQueryHelpers(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()

	value, err := QueryScalar[string](ctx, db, "SELECT ?", "hello")
	if err != nil || value != "hello" {
		t.Fatalf("scalar = %q, %v", value, err)
	}
	if _, err := QueryScalar[int](ctx, db, "SELECT 1 WHERE 0"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty scalar error = %v", err)
	}
	if _, err := QueryScalar[int](ctx, db, "SELECT 'invalid'"); err == nil {
		t.Fatal("expected scalar scan error")
	}
	values, err := QueryMap[int, string](ctx, db, "SELECT 1, ? UNION ALL SELECT 2, ?", "one", "two")
	if err != nil || !maps.Equal(values, map[int]string{1: "one", 2: "two"}) {
		t.Fatalf("map = %v, %v", values, err)
	}
	values, err = QueryMap[int, string](ctx, db, "SELECT 1, 'one' WHERE 0")
	if err != nil || values == nil || len(values) != 0 {
		t.Fatalf("empty map = %v, %v", values, err)
	}
	if _, err := QueryMap[string, int](ctx, db, "SELECT 'key', 'invalid'"); err == nil {
		t.Fatal("expected map scan error")
	}
	if db.Stats().InUse != 0 {
		t.Fatal("scan failure left a connection in use")
	}
	if _, err := QueryMap[string, int](ctx, db, "SELECT * FROM missing_table"); err == nil {
		t.Fatal("expected query error")
	}
	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := QueryScalar[int](canceledCtx, db, "SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("scalar cancellation error = %v", err)
	}
	if _, err := QueryMap[int, int](canceledCtx, db, "SELECT 1, 2"); !errors.Is(err, context.Canceled) {
		t.Fatalf("map cancellation error = %v", err)
	}
}

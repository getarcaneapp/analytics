// Package sqlutil provides typed helpers for SQL query results.
package sqlutil

import (
	"context"
	"database/sql"
)

// QueryScalar scans the first row's single column into T.
// It returns sql.ErrNoRows when the query has no results.
func QueryScalar[T any](ctx context.Context, db *sql.DB, query string, args ...any) (T, error) {
	var value T
	err := db.QueryRowContext(ctx, query, args...).Scan(&value)
	return value, err
}

// QueryMap scans two-column rows into a map. The last value wins for duplicate keys.
// An empty result returns an initialized map.
func QueryMap[K comparable, V any](ctx context.Context, db *sql.DB, query string, args ...any) (map[K]V, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := make(map[K]V)
	for rows.Next() {
		var key K
		var value V
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

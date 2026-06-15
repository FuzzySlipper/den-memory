package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Record is a decoded SQLite record suitable for API response mapping.
type Record map[string]any

// Runner is implemented by sql.DB and sql.Tx.
type Runner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Insert executes an INSERT and returns the inserted row ID.
func Insert(ctx context.Context, r Runner, query string, args ...any) (int64, error) {
	result, err := r.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, sqliteError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read last insert id: %w", err)
	}
	return id, nil
}

// Exec executes a write statement.
func Exec(ctx context.Context, r Runner, query string, args ...any) error {
	_, err := r.ExecContext(ctx, query, args...)
	return sqliteError(err)
}

// QueryAll executes a query and decodes all rows.
func QueryAll(ctx context.Context, r Runner, query string, args ...any) ([]Record, error) {
	rows, err := r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, sqliteError(err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	results := []Record{}
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		record := Record{}
		for i, column := range columns {
			record[column] = normalizeValue(values[i])
		}
		results = append(results, decodeJSONColumns(record))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}
	return results, nil
}

// QueryOne executes a query and returns exactly the first row.
func QueryOne(ctx context.Context, r Runner, query string, args ...any) (Record, error) {
	rows, err := QueryAll(ctx, r, query, args...)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}

func normalizeValue(value any) any {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

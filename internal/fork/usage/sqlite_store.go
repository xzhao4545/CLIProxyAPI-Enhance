package usage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if path == "" {
		path = defaultSQLitePath
	}
	if path != ":memory:" {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create usage sqlite directory: %w", err)
			}
		}
	}

	db, errOpen := sql.Open("sqlite", path)
	if errOpen != nil {
		return nil, fmt.Errorf("open usage sqlite: %w", errOpen)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &SQLiteStore{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) configure(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("usage sqlite store is not initialized")
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return fmt.Errorf("configure usage sqlite %q: %w", pragma, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, usageSchema); err != nil {
		return fmt.Errorf("migrate usage sqlite schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) InsertEvent(ctx context.Context, event Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("usage sqlite store is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if event.StartedAt.IsZero() {
		event.StartedAt = time.Now().UTC()
	}
	if event.CompletedAt.IsZero() {
		event.CompletedAt = event.StartedAt
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO usage_events (
	request_id, started_at, completed_at, duration_ms, provider_key, provider_label,
	auth_id, auth_label, auth_index, model, client_model, route, status, http_status,
	upstream_status, prompt_tokens, completion_tokens, total_tokens, reasoning_tokens,
	cached_tokens, client_key_hash, error_stage, error_code, error_message,
	provider_error_raw, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RequestID,
		event.StartedAt.UTC().UnixMilli(),
		event.CompletedAt.UTC().UnixMilli(),
		event.DurationMS,
		event.ProviderKey,
		event.ProviderLabel,
		event.AuthID,
		event.AuthLabel,
		event.AuthIndex,
		event.Model,
		event.ClientModel,
		event.Route,
		event.Status,
		event.HTTPStatus,
		event.UpstreamStatus,
		event.PromptTokens,
		event.CompletionTokens,
		event.TotalTokens,
		event.ReasoningTokens,
		event.CachedTokens,
		event.ClientKeyHash,
		event.ErrorStage,
		event.ErrorCode,
		event.ErrorMessage,
		event.ProviderErrorRaw,
		event.MetadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert usage event: %w", err)
	}
	return nil
}

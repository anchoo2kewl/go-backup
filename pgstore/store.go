// Package pgstore provides a PostgreSQL-backed implementation of backup.Store.
package pgstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	backup "github.com/anchoo2kewl/go-backup"
	_ "github.com/lib/pq"
)

// PgStore implements backup.Store using a *sql.DB.
type PgStore struct {
	db *sql.DB
}

// New returns a PgStore backed by the provided *sql.DB.
// The caller is responsible for running the migration SQL before first use.
func New(db *sql.DB) *PgStore {
	return &PgStore{db: db}
}

// GetSettings fetches the singleton settings row.
func (s *PgStore) GetSettings(ctx context.Context) (*backup.BackupSettings, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT enabled, cron_expression, folder_id, provider_name, provider_config,
		       retention_full_days, retention_alternate_days, retention_weekly_days, updated_at
		FROM backup_settings
		WHERE id = 'singleton'`)

	var bs backup.BackupSettings
	err := row.Scan(
		&bs.Enabled,
		&bs.CronExpression,
		&bs.FolderID,
		&bs.ProviderName,
		&bs.ProviderConfig,
		&bs.RetentionPolicy.FullDays,
		&bs.RetentionPolicy.AlternateDays,
		&bs.RetentionPolicy.WeeklyDays,
		&bs.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pgstore: settings not found: %w", err)
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: GetSettings: %w", err)
	}
	return &bs, nil
}

// SaveSettings upserts the singleton settings row.
func (s *PgStore) SaveSettings(ctx context.Context, bs *backup.BackupSettings) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO backup_settings (
			id, enabled, cron_expression, folder_id, provider_name, provider_config,
			retention_full_days, retention_alternate_days, retention_weekly_days, updated_at
		) VALUES ('singleton', $1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO UPDATE SET
			enabled                  = EXCLUDED.enabled,
			cron_expression          = EXCLUDED.cron_expression,
			folder_id                = EXCLUDED.folder_id,
			provider_name            = EXCLUDED.provider_name,
			provider_config          = EXCLUDED.provider_config,
			retention_full_days      = EXCLUDED.retention_full_days,
			retention_alternate_days = EXCLUDED.retention_alternate_days,
			retention_weekly_days    = EXCLUDED.retention_weekly_days,
			updated_at               = EXCLUDED.updated_at`,
		bs.Enabled,
		bs.CronExpression,
		bs.FolderID,
		bs.ProviderName,
		bs.ProviderConfig,
		bs.RetentionPolicy.FullDays,
		bs.RetentionPolicy.AlternateDays,
		bs.RetentionPolicy.WeeklyDays,
		bs.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("pgstore: SaveSettings: %w", err)
	}
	return nil
}

// CreateBackupRecord inserts a new BackupRecord and returns its UUID.
func (s *PgStore) CreateBackupRecord(ctx context.Context, r *backup.BackupRecord) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO backup_history (status, filename, size_bytes, provider_name, file_id, file_url, error_message, started_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text`,
		r.Status, r.Filename, r.SizeBytes, r.ProviderName, r.FileID, r.FileURL, r.ErrorMessage, r.StartedAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("pgstore: CreateBackupRecord: %w", err)
	}
	return id, nil
}

// UpdateBackupRecord updates an existing record.
func (s *PgStore) UpdateBackupRecord(ctx context.Context, r *backup.BackupRecord) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE backup_history
		SET status = $1, filename = $2, size_bytes = $3, file_id = $4, file_url = $5,
		    error_message = $6, finished_at = $7
		WHERE id = $8::uuid`,
		r.Status, r.Filename, r.SizeBytes, r.FileID, r.FileURL, r.ErrorMessage, r.FinishedAt, r.ID,
	)
	if err != nil {
		return fmt.Errorf("pgstore: UpdateBackupRecord: %w", err)
	}
	return nil
}

// ListBackupRecords returns up to limit records ordered by started_at DESC.
func (s *PgStore) ListBackupRecords(ctx context.Context, limit int) ([]*backup.BackupRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, status, filename, size_bytes, provider_name, file_id, file_url, error_message, started_at, finished_at
		FROM backup_history
		ORDER BY started_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("pgstore: ListBackupRecords: %w", err)
	}
	defer rows.Close()
	var records []*backup.BackupRecord
	for rows.Next() {
		r := &backup.BackupRecord{}
		var finAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.Status, &r.Filename, &r.SizeBytes, &r.ProviderName,
			&r.FileID, &r.FileURL, &r.ErrorMessage, &r.StartedAt, &finAt); err != nil {
			return nil, fmt.Errorf("pgstore: scan: %w", err)
		}
		if finAt.Valid {
			t := finAt.Time
			r.FinishedAt = &t
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

// GetBackupRecord fetches a single record by ID.
func (s *PgStore) GetBackupRecord(ctx context.Context, id string) (*backup.BackupRecord, error) {
	r := &backup.BackupRecord{}
	var finAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id::text, status, filename, size_bytes, provider_name, file_id, file_url, error_message, started_at, finished_at
		FROM backup_history WHERE id = $1::uuid`, id,
	).Scan(&r.ID, &r.Status, &r.Filename, &r.SizeBytes, &r.ProviderName,
		&r.FileID, &r.FileURL, &r.ErrorMessage, &r.StartedAt, &finAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("pgstore: record %s not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: GetBackupRecord: %w", err)
	}
	if finAt.Valid {
		t := finAt.Time
		r.FinishedAt = &t
	}
	return r, nil
}

// DeleteBackupRecord removes a record by ID.
func (s *PgStore) DeleteBackupRecord(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM backup_history WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("pgstore: DeleteBackupRecord: %w", err)
	}
	return nil
}

// Ensure PgStore implements backup.Store at compile time.
var _ backup.Store = (*PgStore)(nil)

// NullTime is a helper for nullable time scanning.
type NullTime struct {
	Time  time.Time
	Valid bool
}

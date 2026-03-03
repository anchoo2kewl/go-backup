package backup

import (
	"context"
	"time"
)

// Store persists backup settings and history records.
// The root module defines the interface; implementations live in sub-modules (e.g. pgstore).
type Store interface {
	GetSettings(ctx context.Context) (*BackupSettings, error)
	SaveSettings(ctx context.Context, s *BackupSettings) error
	CreateBackupRecord(ctx context.Context, r *BackupRecord) (string, error)
	UpdateBackupRecord(ctx context.Context, r *BackupRecord) error
	ListBackupRecords(ctx context.Context, limit int) ([]*BackupRecord, error)
	GetBackupRecord(ctx context.Context, id string) (*BackupRecord, error)
	DeleteBackupRecord(ctx context.Context, id string) error
}

// BackupSettings is the singleton configuration row persisted in the store.
type BackupSettings struct {
	Enabled         bool
	CronExpression  string
	FolderID        string
	ProviderName    string
	ProviderConfig  []byte // AES-GCM encrypted JSON of provider tokens
	RetentionPolicy RetentionPolicy
	UpdatedAt       time.Time
}

// BackupRecord represents one backup run.
type BackupRecord struct {
	ID           string
	Status       string // "running" | "success" | "failed"
	Filename     string
	SizeBytes    int64
	ProviderName string
	FileID       string
	FileURL      string
	ErrorMessage string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

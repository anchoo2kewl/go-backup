// Package backup provides an embeddable HTTP handler that adds scheduled
// database backup functionality to any Go web application.
//
// Usage:
//
//	m, err := backup.New(
//	    backup.WithStore(pgstore.New(db)),
//	    backup.WithDumper(backup.NewPostgresDumper(databaseURL)),
//	    backup.WithProvider(gdriveProvider),
//	    backup.WithBasePath("/api/admin/backup"),
//	    backup.WithEncryptionKey(key),
//	)
//	m.Start()
//	defer m.Stop()
//	mux.Handle("/api/admin/backup/", m.Handler())
package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Manager is the central controller. Create it with New().
type Manager struct {
	store     Store
	dumper    DatabaseDumper
	provider  StorageProvider
	oauthProv OAuthConfigurableProvider

	basePath   string
	successURL string
	encKey     []byte

	sched      *backupScheduler
	running    int32      // atomic: 1 if a backup is in progress
	oauthState sync.Map   // state string → expiresAt time.Time
}

// New creates a new Manager from the provided options.
func New(opts ...Option) (*Manager, error) {
	m := &Manager{
		basePath: "/backup",
	}
	for _, o := range opts {
		o(m)
	}
	if m.store == nil {
		return nil, ErrNoStore
	}
	if m.dumper == nil {
		return nil, ErrNoDumper
	}
	if len(m.encKey) > 0 && len(m.encKey) != 32 {
		return nil, ErrInvalidKey
	}
	if len(m.encKey) == 0 {
		// Generate a random key for this process lifetime (tokens won't survive restarts).
		k := make([]byte, 32)
		if _, err := rand.Read(k); err != nil {
			return nil, fmt.Errorf("backup: failed to generate ephemeral key: %w", err)
		}
		m.encKey = k
		log.Println("[go-backup] WARNING: no encryption key provided; tokens will not survive restarts")
	}
	m.sched = newScheduler(m)
	return m, nil
}

// Handler returns an http.Handler mounted at BasePath.
// Mount it with a trailing slash wildcard, e.g.:
//
//	mux.Handle("/api/admin/backup/", manager.Handler())
func (m *Manager) Handler() http.Handler { return m.routes() }

// Start loads the saved cron expression and begins the scheduler.
func (m *Manager) Start() error {
	ctx := context.Background()
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		// No settings yet — default enabled=false, will be configured via UI.
		m.sched.start()
		return nil
	}

	// Try to hydrate the provider from stored config.
	if settings.Enabled && len(settings.ProviderConfig) > 0 && m.oauthProv != nil {
		plain, err := decrypt(m.encKey, settings.ProviderConfig)
		if err == nil {
			if lerr := m.oauthProv.LoadFromConfig(plain); lerr != nil {
				log.Printf("[go-backup] failed to load provider config: %v", lerr)
			}
		}
	}

	if settings.Enabled && settings.CronExpression != "" {
		if err := m.sched.reschedule(settings.CronExpression); err != nil {
			log.Printf("[go-backup] invalid cron expression %q: %v", settings.CronExpression, err)
		}
	}
	m.sched.start()
	return nil
}

// Stop gracefully shuts down the scheduler. It waits up to 30 s for any
// in-progress backup to finish before returning.
func (m *Manager) Stop() {
	m.sched.stop()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&m.running) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	log.Println("[go-backup] WARNING: Stop() timed out waiting for running backup")
}

// RunBackup executes a backup immediately.
// triggeredBy should be "manual" or "scheduled".
// Returns ErrBackupInProgress if a backup is already running.
func (m *Manager) RunBackup(ctx context.Context, triggeredBy string) (*BackupRecord, error) {
	if !atomic.CompareAndSwapInt32(&m.running, 0, 1) {
		return nil, ErrBackupInProgress
	}
	defer atomic.StoreInt32(&m.running, 0)

	if m.provider == nil {
		return nil, ErrNoProvider
	}

	// Load latest settings to get FolderID and possibly refresh provider config.
	settings, err := m.store.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup: failed to load settings: %w", err)
	}
	// Only scheduled runs respect the Enabled flag; manual runs always proceed.
	if triggeredBy != "manual" && !settings.Enabled {
		return nil, fmt.Errorf("backup: backups are disabled")
	}

	// Hydrate provider tokens from settings.
	if m.oauthProv != nil && len(settings.ProviderConfig) > 0 {
		plain, err := decrypt(m.encKey, settings.ProviderConfig)
		if err != nil {
			return nil, fmt.Errorf("backup: failed to decrypt provider config: %w", err)
		}
		if err := m.oauthProv.LoadFromConfig(plain); err != nil {
			return nil, fmt.Errorf("backup: failed to load provider config: %w", err)
		}
	}

	now := time.Now()
	filename := BackupFilename(m.dumper.DatabaseName(), now)

	rec := &BackupRecord{
		Status:       "running",
		TriggeredBy:  triggeredBy,
		Filename:     filename,
		ProviderName: m.provider.Name(),
		StartedAt:    now,
	}
	id, err := m.store.CreateBackupRecord(ctx, rec)
	if err != nil {
		return nil, fmt.Errorf("backup: failed to create record: %w", err)
	}
	rec.ID = id

	// Write dump to a temp file (so we know the size for the upload).
	tmp, err := os.CreateTemp("", "backup-*.dump.gz")
	if err != nil {
		return nil, fmt.Errorf("backup: failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := m.dumper.Dump(ctx, tmp); err != nil {
		tmp.Close()
		rec.Status = "failed"
		rec.ErrorMessage = err.Error()
		fin := time.Now()
		rec.FinishedAt = &fin
		_ = m.store.UpdateBackupRecord(ctx, rec)
		return rec, fmt.Errorf("backup: dump failed: %w", err)
	}
	sz, _ := tmp.Seek(0, 2) // seek to end to get size
	_, _ = tmp.Seek(0, 0)   // rewind

	result, err := m.provider.Upload(ctx, filename, tmp, sz, settings.FolderID)
	tmp.Close()
	if err != nil {
		rec.Status = "failed"
		rec.ErrorMessage = err.Error()
		fin := time.Now()
		rec.FinishedAt = &fin
		_ = m.store.UpdateBackupRecord(ctx, rec)
		return rec, fmt.Errorf("backup: upload failed: %w", err)
	}

	fin := time.Now()
	rec.Status = "success"
	rec.SizeBytes = result.Size
	rec.FileID = result.FileID
	rec.FileURL = result.FileURL
	rec.FinishedAt = &fin
	if err := m.store.UpdateBackupRecord(ctx, rec); err != nil {
		log.Printf("[go-backup] failed to update record: %v", err)
	}

	// Apply retention policy.
	go m.applyRetention(settings.RetentionPolicy)

	return rec, nil
}

func (m *Manager) applyRetention(policy RetentionPolicy) {
	ctx := context.Background()
	records, err := m.store.ListBackupRecords(ctx, 1000)
	if err != nil {
		log.Printf("[go-backup] retention: failed to list records: %v", err)
		return
	}
	toDelete := policy.Apply(records)
	for _, id := range toDelete {
		rec, err := m.store.GetBackupRecord(ctx, id)
		if err != nil {
			continue
		}
		if rec.FileID != "" {
			if err := m.provider.Delete(ctx, rec.FileID); err != nil {
				log.Printf("[go-backup] retention: failed to delete remote file %s: %v", rec.FileID, err)
			}
		}
		if err := m.store.DeleteBackupRecord(ctx, id); err != nil {
			log.Printf("[go-backup] retention: failed to delete record %s: %v", id, err)
		}
	}
}

// generateState creates a random CSRF state token and stores it for 10 min.
func (m *Manager) generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b)
	m.oauthState.Store(state, time.Now().Add(10*time.Minute))
	return state, nil
}

// validateState checks and consumes a CSRF state token.
func (m *Manager) validateState(state string) bool {
	v, ok := m.oauthState.LoadAndDelete(state)
	if !ok {
		return false
	}
	return time.Now().Before(v.(time.Time))
}

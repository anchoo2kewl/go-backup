package backup

import "errors"

var (
	ErrNotConfigured    = errors.New("backup: not configured")
	ErrBackupInProgress = errors.New("backup: backup already in progress")
	ErrNoProvider       = errors.New("backup: no storage provider configured")
	ErrNoDumper         = errors.New("backup: no database dumper configured")
	ErrNoStore          = errors.New("backup: no store configured")
	ErrInvalidKey       = errors.New("backup: encryption key must be exactly 32 bytes")
)

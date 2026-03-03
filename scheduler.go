package backup

import (
	"context"
	"log"

	"github.com/robfig/cron/v3"
)

type backupScheduler struct {
	c       *cron.Cron
	manager *Manager
	entryID cron.EntryID
}

func newScheduler(m *Manager) *backupScheduler {
	return &backupScheduler{
		c:       cron.New(),
		manager: m,
	}
}

// reschedule cancels any existing cron job and registers a new one.
func (s *backupScheduler) reschedule(expr string) error {
	if s.entryID != 0 {
		s.c.Remove(s.entryID)
		s.entryID = 0
	}
	if expr == "" {
		return nil
	}
	id, err := s.c.AddFunc(expr, func() {
		ctx := context.Background()
		rec, err := s.manager.RunBackup(ctx, "scheduled")
		if err != nil {
			log.Printf("[go-backup] scheduled backup failed: %v", err)
			return
		}
		log.Printf("[go-backup] scheduled backup completed: %s (%d bytes)", rec.Filename, rec.SizeBytes)
	})
	if err != nil {
		return err
	}
	s.entryID = id
	return nil
}

func (s *backupScheduler) start() { s.c.Start() }
func (s *backupScheduler) stop()  { s.c.Stop() }

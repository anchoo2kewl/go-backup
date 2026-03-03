package backup

import (
	"sort"
	"time"
)

// RetentionPolicy defines how long backups are kept at different granularities.
type RetentionPolicy struct {
	FullDays      int // keep ALL backups for this many days (default 30)
	AlternateDays int // keep every-other-day after FullDays up to this many days (default 60)
	WeeklyDays    int // keep one per ISO week after AlternateDays up to this many days (default 365)
}

func defaultRetention() RetentionPolicy {
	return RetentionPolicy{FullDays: 30, AlternateDays: 60, WeeklyDays: 365}
}

// Apply returns the IDs of BackupRecords that should be deleted according to
// the retention policy. It operates only on successful backups.
//
// Tiers (measured from now):
//
//	0            → FullDays      : keep ALL
//	FullDays     → AlternateDays : keep first per 2-day bucket
//	AlternateDays → WeeklyDays   : keep first per ISO week bucket
//	>= WeeklyDays               : delete ALL
func (p RetentionPolicy) Apply(records []*BackupRecord) []string {
	full := p.FullDays
	if full <= 0 {
		full = 30
	}
	alt := p.AlternateDays
	if alt <= 0 {
		alt = 60
	}
	weekly := p.WeeklyDays
	if weekly <= 0 {
		weekly = 365
	}

	now := time.Now()

	// Work on successful records only; sort ascending (oldest first).
	var ok []*BackupRecord
	for _, r := range records {
		if r.Status == "success" {
			ok = append(ok, r)
		}
	}
	sort.Slice(ok, func(i, j int) bool {
		return ok[i].StartedAt.Before(ok[j].StartedAt)
	})

	seen2day := map[int]bool{}  // bucket → first seen
	seenWeek := map[int]bool{}  // bucket → first seen
	var toDelete []string

	for _, r := range ok {
		ageDays := int(now.Sub(r.StartedAt).Hours() / 24)

		switch {
		case ageDays < full:
			// Tier 1: keep all

		case ageDays < alt:
			// Tier 2: one per 2-day bucket (keep the first/oldest in each bucket)
			bucket := ageDays / 2
			if seen2day[bucket] {
				toDelete = append(toDelete, r.ID)
			} else {
				seen2day[bucket] = true
			}

		case ageDays < weekly:
			// Tier 3: one per ISO week (keep the first/oldest in each week)
			y, w := r.StartedAt.ISOWeek()
			bucket := y*100 + w
			if seenWeek[bucket] {
				toDelete = append(toDelete, r.ID)
			} else {
				seenWeek[bucket] = true
			}

		default:
			// Expired: delete all
			toDelete = append(toDelete, r.ID)
		}
	}
	return toDelete
}

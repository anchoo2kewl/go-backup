package backup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type statusResponse struct {
	Running           bool       `json:"running"`
	Enabled           bool       `json:"enabled"`
	ProviderConnected bool       `json:"provider_connected"`
	ProviderName      string     `json:"provider_name,omitempty"`
	ConnectedEmail    string     `json:"connected_email,omitempty"`
	NextRun           *time.Time `json:"next_run,omitempty"`
	LastBackup        *briefRecord `json:"last_backup,omitempty"`
}

type briefRecord struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Filename   string     `json:"filename"`
	SizeBytes  int64      `json:"size_bytes"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (m *Manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := statusResponse{
		Running: atomic.LoadInt32(&m.running) == 1,
	}

	if m.provider != nil {
		resp.ProviderName = m.provider.Name()
	}
	if m.oauthProv != nil {
		resp.ConnectedEmail = m.oauthProv.ConnectedEmail()
		resp.ProviderConnected = resp.ConnectedEmail != ""
	} else if m.provider != nil {
		resp.ProviderConnected = m.provider.Ping(ctx) == nil
	}

	settings, err := m.store.GetSettings(ctx)
	if err == nil {
		resp.Enabled = settings.Enabled
		entry := m.sched.c.Entries()
		if len(entry) > 0 {
			t := entry[0].Next
			resp.NextRun = &t
		}
	}

	records, err := m.store.ListBackupRecords(ctx, 1)
	if err == nil && len(records) > 0 {
		last := records[0]
		resp.LastBackup = &briefRecord{
			ID:         last.ID,
			Status:     last.Status,
			Filename:   last.Filename,
			SizeBytes:  last.SizeBytes,
			StartedAt:  last.StartedAt,
			FinishedAt: last.FinishedAt,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (m *Manager) handleTrigger(w http.ResponseWriter, r *http.Request) {
	rec, err := m.RunBackup(r.Context(), "manual")
	if err != nil {
		if errors.Is(err, ErrBackupInProgress) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (m *Manager) handleListHistory(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}
	records, err := m.store.ListBackupRecords(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []*BackupRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (m *Manager) handleDeleteHistory(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	rec, err := m.store.GetBackupRecord(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "backup record not found")
		return
	}
	if rec.FileID != "" && m.provider != nil {
		if err := m.provider.Delete(ctx, rec.FileID); err != nil {
			// Log but don't fail — still remove local record.
		}
	}
	if err := m.store.DeleteBackupRecord(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) handleDownloadHistory(w http.ResponseWriter, r *http.Request, id string) {
	// For Google Drive, we redirect to the direct download URL.
	ctx := r.Context()
	rec, err := m.store.GetBackupRecord(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "backup record not found")
		return
	}
	if rec.FileURL == "" {
		writeError(w, http.StatusNotFound, "no download URL available")
		return
	}

	// Stream the file through our server so the admin doesn't need Drive access.
	if m.oauthProv != nil {
		client := m.oauthProv.(interface {
			HTTPClient(ctx context.Context) *http.Client
		})
		if client != nil {
			hc := client.HTTPClient(ctx)
			resp, err := hc.Get(rec.FileURL)
			if err != nil {
				writeError(w, http.StatusBadGateway, "failed to fetch from storage provider")
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Disposition", `attachment; filename="`+rec.Filename+`"`)
			w.Header().Set("Content-Type", "application/gzip")
			w.WriteHeader(http.StatusOK)
			io.Copy(w, resp.Body)
			return
		}
	}
	http.Redirect(w, r, rec.FileURL, http.StatusFound)
}

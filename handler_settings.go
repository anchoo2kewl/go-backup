package backup

import (
	"encoding/json"
	"net/http"
	"time"
)

type settingsResponse struct {
	Enabled        bool            `json:"enabled"`
	CronExpression string          `json:"cron_expression"`
	FolderID       string          `json:"folder_id"`
	ProviderName   string          `json:"provider_name"`
	Retention      RetentionPolicy `json:"retention"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type settingsRequest struct {
	Enabled        *bool           `json:"enabled"`
	CronExpression *string         `json:"cron_expression"`
	FolderID       *string         `json:"folder_id"`
	Retention      *RetentionPolicy `json:"retention"`
}

func (m *Manager) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s, err := m.store.GetSettings(ctx)
	if err != nil {
		// Return defaults when not configured yet.
		def := defaultRetention()
		writeJSON(w, http.StatusOK, settingsResponse{
			CronExpression: "0 3 * * *",
			Retention:      def,
		})
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{
		Enabled:        s.Enabled,
		CronExpression: s.CronExpression,
		FolderID:       s.FolderID,
		ProviderName:   s.ProviderName,
		Retention:      s.RetentionPolicy,
		UpdatedAt:      s.UpdatedAt,
	})
}

func (m *Manager) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	ctx := r.Context()
	s, err := m.store.GetSettings(ctx)
	if err != nil {
		def := defaultRetention()
		s = &BackupSettings{
			CronExpression: "0 3 * * *",
			RetentionPolicy: def,
		}
	}

	if req.Enabled != nil {
		s.Enabled = *req.Enabled
	}
	if req.CronExpression != nil {
		s.CronExpression = *req.CronExpression
	}
	if req.FolderID != nil {
		s.FolderID = *req.FolderID
	}
	if req.Retention != nil {
		s.RetentionPolicy = *req.Retention
	}
	s.UpdatedAt = time.Now()

	if m.provider != nil {
		s.ProviderName = m.provider.Name()
	}

	if err := m.store.SaveSettings(ctx, s); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Reschedule cron if needed.
	if s.Enabled && s.CronExpression != "" {
		if err := m.sched.reschedule(s.CronExpression); err != nil {
			writeError(w, http.StatusBadRequest, "invalid cron expression: "+err.Error())
			return
		}
	} else {
		_ = m.sched.reschedule("") // clear schedule
	}

	writeJSON(w, http.StatusOK, settingsResponse{
		Enabled:        s.Enabled,
		CronExpression: s.CronExpression,
		FolderID:       s.FolderID,
		ProviderName:   s.ProviderName,
		Retention:      s.RetentionPolicy,
		UpdatedAt:      s.UpdatedAt,
	})
}

func (m *Manager) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	if m.provider == nil {
		writeError(w, http.StatusServiceUnavailable, "no provider configured")
		return
	}
	if err := m.provider.Ping(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type folderResponse struct {
	Folders []*Folder `json:"folders"`
}

func (m *Manager) handleListFolders(w http.ResponseWriter, r *http.Request) {
	if m.provider == nil {
		writeError(w, http.StatusServiceUnavailable, "no provider configured")
		return
	}
	parentID := r.URL.Query().Get("parentId")
	folders, err := m.provider.ListFolders(r.Context(), parentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if folders == nil {
		folders = []*Folder{}
	}
	writeJSON(w, http.StatusOK, folderResponse{Folders: folders})
}

func (m *Manager) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	if m.provider == nil {
		writeError(w, http.StatusServiceUnavailable, "no provider configured")
		return
	}
	var req struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	folder, err := m.provider.CreateFolder(r.Context(), req.Name, req.ParentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, folder)
}

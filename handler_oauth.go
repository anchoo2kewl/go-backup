package backup

import (
	"context"
	"log"
	"net/http"
	"time"
)

func (m *Manager) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	if m.oauthProv == nil {
		writeError(w, http.StatusNotImplemented, "OAuth not supported by current provider")
		return
	}
	state, err := m.generateState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}
	url := m.oauthProv.BuildAuthURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

func (m *Manager) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if m.oauthProv == nil {
		writeError(w, http.StatusNotImplemented, "OAuth not supported by current provider")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if !m.validateState(state) {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tokenJSON, err := m.oauthProv.ExchangeCode(ctx, code)
	if err != nil {
		log.Printf("[go-backup] OAuth exchange failed: %v", err)
		http.Error(w, "OAuth exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Encrypt and save token in settings.
	encrypted, err := encrypt(m.encKey, tokenJSON)
	if err != nil {
		http.Error(w, "failed to encrypt token", http.StatusInternalServerError)
		return
	}

	s, err := m.store.GetSettings(ctx)
	if err != nil {
		def := defaultRetention()
		s = &BackupSettings{
			CronExpression:  "0 3 * * *",
			RetentionPolicy: def,
		}
	}
	s.ProviderConfig = encrypted
	s.ProviderName = m.oauthProv.Name()
	s.UpdatedAt = time.Now()
	if err := m.store.SaveSettings(ctx, s); err != nil {
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	redirectURL := m.successURL
	if redirectURL == "" {
		redirectURL = "/"
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (m *Manager) handleOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if m.oauthProv == nil {
		writeError(w, http.StatusNotImplemented, "OAuth not supported by current provider")
		return
	}

	ctx := context.Background()
	s, err := m.store.GetSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load settings")
		return
	}
	s.ProviderConfig = nil
	s.ProviderName = ""
	s.Enabled = false
	s.UpdatedAt = time.Now()
	if err := m.store.SaveSettings(ctx, s); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}

	// Clear the in-memory token.
	_ = m.oauthProv.LoadFromConfig(nil)
	_ = m.sched.reschedule("") // stop any scheduled backups

	writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

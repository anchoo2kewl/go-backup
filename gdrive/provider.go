package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	backup "github.com/anchoo2kewl/go-backup"
	"golang.org/x/oauth2"
)

// Provider implements backup.StorageProvider and backup.OAuthConfigurableProvider
// using Google Drive.
type Provider struct {
	auth  *Auth
	mu    sync.RWMutex
	token *oauth2.Token
	email string
}

// NewProvider creates a Provider from an Auth config.
func NewProvider(auth *Auth) *Provider {
	return &Provider{auth: auth}
}

// Name returns the provider name.
func (p *Provider) Name() string { return "google_drive" }

// ConnectedEmail returns the connected Google account email.
func (p *Provider) ConnectedEmail() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.email
}

// BuildAuthURL returns the Google OAuth2 consent URL.
func (p *Provider) BuildAuthURL(state string) string {
	return p.auth.BuildAuthURL(state)
}

// ExchangeCode exchanges an auth code for a token, stores it internally,
// and returns the JSON-serialised token for encrypted persistence.
func (p *Provider) ExchangeCode(ctx context.Context, code string) ([]byte, error) {
	tok, err := p.auth.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("gdrive: token exchange failed: %w", err)
	}

	// Fetch the authorised email.
	client := p.auth.Client(ctx, tok)
	email, err := UserEmail(ctx, client)
	if err != nil {
		// Non-fatal — store the token anyway.
		email = ""
	}

	p.mu.Lock()
	p.token = tok
	p.email = email
	p.mu.Unlock()

	// Serialise token + email for encrypted storage.
	stored := struct {
		Token *oauth2.Token `json:"token"`
		Email string        `json:"email"`
	}{Token: tok, Email: email}
	return json.Marshal(stored)
}

// LoadFromConfig loads a previously stored (decrypted) token JSON.
// Pass nil to clear the token (disconnect).
func (p *Provider) LoadFromConfig(config []byte) error {
	if config == nil {
		p.mu.Lock()
		p.token = nil
		p.email = ""
		p.mu.Unlock()
		return nil
	}
	var stored struct {
		Token *oauth2.Token `json:"token"`
		Email string        `json:"email"`
	}
	if err := json.Unmarshal(config, &stored); err != nil {
		return fmt.Errorf("gdrive: failed to parse stored config: %w", err)
	}
	p.mu.Lock()
	p.token = stored.Token
	p.email = stored.Email
	p.mu.Unlock()
	return nil
}

func (p *Provider) client(ctx context.Context) (*http.Client, error) {
	p.mu.RLock()
	tok := p.token
	p.mu.RUnlock()
	if tok == nil {
		return nil, fmt.Errorf("gdrive: not authenticated")
	}
	return p.auth.Client(ctx, tok), nil
}

// HTTPClient returns an authenticated HTTP client. Used by the download handler.
func (p *Provider) HTTPClient(ctx context.Context) *http.Client {
	c, _ := p.client(ctx)
	return c
}

// Upload uploads a backup file to the given Drive folder (or root if folderID is empty).
func (p *Provider) Upload(ctx context.Context, name string, r io.Reader, size int64, folderID string) (*backup.UploadResult, error) {
	c, err := p.client(ctx)
	if err != nil {
		return nil, err
	}
	fileID, _, err := uploadFile(ctx, c, name, folderID, r, size)
	if err != nil {
		return nil, err
	}
	// Build a direct download URL for streaming.
	downloadURL := "https://www.googleapis.com/drive/v3/files/" + fileID + "?alt=media"
	return &backup.UploadResult{
		FileID:     fileID,
		FileURL:    downloadURL,
		Size:       size,
		UploadedAt: time.Now(),
	}, nil
}

// Delete permanently deletes a Drive file.
func (p *Provider) Delete(ctx context.Context, fileID string) error {
	c, err := p.client(ctx)
	if err != nil {
		return err
	}
	return deleteFile(ctx, c, fileID)
}

// ListFolders lists Drive folders under parentID.
func (p *Provider) ListFolders(ctx context.Context, parentID string) ([]*backup.Folder, error) {
	c, err := p.client(ctx)
	if err != nil {
		return nil, err
	}
	files, err := listFolders(ctx, c, parentID)
	if err != nil {
		return nil, err
	}
	var out []*backup.Folder
	for _, f := range files {
		parent := ""
		if len(f.Parents) > 0 {
			parent = f.Parents[0]
		}
		out = append(out, &backup.Folder{ID: f.ID, Name: f.Name, ParentID: parent})
	}
	return out, nil
}

// CreateFolder creates a Drive folder.
func (p *Provider) CreateFolder(ctx context.Context, name, parentID string) (*backup.Folder, error) {
	c, err := p.client(ctx)
	if err != nil {
		return nil, err
	}
	f, err := createFolder(ctx, c, name, parentID)
	if err != nil {
		return nil, err
	}
	parent := ""
	if len(f.Parents) > 0 {
		parent = f.Parents[0]
	}
	return &backup.Folder{ID: f.ID, Name: f.Name, ParentID: parent}, nil
}

// Ping checks that the Drive API is reachable and the token is valid.
func (p *Provider) Ping(ctx context.Context) error {
	c, err := p.client(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, driveFilesURL+"?pageSize=1", nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("gdrive: ping failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gdrive: ping returned %d: %s", resp.StatusCode, body)
	}
	return nil
}

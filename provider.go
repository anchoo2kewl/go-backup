package backup

import (
	"context"
	"io"
	"time"
)

// StorageProvider is the interface that any remote storage backend must implement.
type StorageProvider interface {
	Upload(ctx context.Context, name string, r io.Reader, size int64, folderID string) (*UploadResult, error)
	Delete(ctx context.Context, fileID string) error
	ListFolders(ctx context.Context, parentID string) ([]*Folder, error)
	CreateFolder(ctx context.Context, name, parentID string) (*Folder, error)
	Ping(ctx context.Context) error
	Name() string
}

// OAuthConfigurableProvider extends StorageProvider for providers that use OAuth2.
type OAuthConfigurableProvider interface {
	StorageProvider
	// BuildAuthURL returns the OAuth2 authorization URL for the given state.
	BuildAuthURL(state string) string
	// ExchangeCode exchanges an authorization code for a token.
	// Returns raw JSON bytes of the token to be stored (encrypted) in settings.
	ExchangeCode(ctx context.Context, code string) ([]byte, error)
	// LoadFromConfig initialises the provider from previously stored (decrypted) token bytes.
	LoadFromConfig(config []byte) error
	// ConnectedEmail returns the authorized account email, or "" if not connected.
	ConnectedEmail() string
}

// UploadResult is returned by StorageProvider.Upload.
type UploadResult struct {
	FileID     string
	FileURL    string
	Size       int64
	UploadedAt time.Time
}

// Folder represents a remote folder.
type Folder struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}

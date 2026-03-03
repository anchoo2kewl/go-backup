package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var driveScopes = []string{
	"https://www.googleapis.com/auth/drive.file",
	"https://www.googleapis.com/auth/userinfo.email",
}

// Auth holds the OAuth2 client configuration.
type Auth struct {
	cfg *oauth2.Config
}

// NewAuth creates an Auth from a Google OAuth2 client ID and secret.
// redirectURL should be the full callback URL registered in Google Cloud Console.
func NewAuth(clientID, clientSecret, redirectURL string) *Auth {
	return &Auth{
		cfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       driveScopes,
			Endpoint:     google.Endpoint,
		},
	}
}

// BuildAuthURL returns the OAuth2 consent URL for the given state.
func (a *Auth) BuildAuthURL(state string) string {
	return a.cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

// Exchange exchanges an authorization code for a token.
func (a *Auth) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return a.cfg.Exchange(ctx, code)
}

// Client returns an HTTP client authorised with the given token.
func (a *Auth) Client(ctx context.Context, tok *oauth2.Token) *http.Client {
	return a.cfg.Client(ctx, tok)
}

// UserEmail returns the Google account email associated with a token.
func UserEmail(ctx context.Context, client *http.Client) (string, error) {
	resp, err := client.Get("https://www.googleapis.com/userinfo/v2/me")
	if err != nil {
		return "", fmt.Errorf("gdrive: failed to get user info: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var info struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return "", fmt.Errorf("gdrive: failed to parse user info: %w", err)
	}
	return info.Email, nil
}

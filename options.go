package backup

// Option is a functional option for Manager.
type Option func(*Manager)

// WithStore sets the persistence backend.
func WithStore(s Store) Option {
	return func(m *Manager) { m.store = s }
}

// WithDumper sets the database dumper.
func WithDumper(d DatabaseDumper) Option {
	return func(m *Manager) { m.dumper = d }
}

// WithProvider sets the storage provider. If the provider also implements
// OAuthConfigurableProvider, OAuth routes are automatically enabled.
func WithProvider(p StorageProvider) Option {
	return func(m *Manager) {
		m.provider = p
		if op, ok := p.(OAuthConfigurableProvider); ok {
			m.oauthProv = op
		}
	}
}

// WithBasePath sets the HTTP path prefix (default "/backup").
func WithBasePath(path string) Option {
	return func(m *Manager) { m.basePath = path }
}

// WithOAuthSuccessRedirect sets the URL the browser is sent to after
// a successful OAuth callback.
func WithOAuthSuccessRedirect(url string) Option {
	return func(m *Manager) { m.successURL = url }
}

// WithEncryptionKey sets the 32-byte AES-256-GCM key used to encrypt
// provider tokens at rest.
func WithEncryptionKey(key []byte) Option {
	return func(m *Manager) { m.encKey = key }
}

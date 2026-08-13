package provider

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/tltre/gogent/internal/credentials"
)

// CredentialStore is the abstraction for reading and writing provider API
// keys. Provider names are engine identifiers ("openai", "deepseek", ...).
//
// The store is app-scoped: the default FileCredentialStore writes to
// ~/.gogent/apps/<app-name>/credentials.yaml, keeping each agent app's
// provider credentials separate from the daemon-level credentials.yaml
// (which holds tool/sandbox secrets). App developers may inject their own
// implementation (Vault, keyring, ...) via app.WithCredentialStore.
type CredentialStore interface {
	// Get returns the API key for the named provider.
	Get(providerName string) (string, error)
	// Set stores (or updates) the API key for the named provider.
	Set(providerName, apiKey string) error
	// Delete removes the API key for the named provider.
	Delete(providerName string) error
	// List returns the names of all providers that have a configured key.
	List() ([]string, error)
}

// CredentialStoreAware is implemented by provider engines that accept a
// CredentialStore for runtime API-key resolution. The Builder injects the
// store after applying BuildOptions so WithCredentialStore overrides take
// effect before any Generate call.
type CredentialStoreAware interface {
	SetCredentialStore(s CredentialStore)
}

// DefaultAppCredentialPath returns the app-scoped credentials file path:
// ~/.gogent/apps/<app-name>/credentials.yaml
func DefaultAppCredentialPath(appName string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gogent", "apps", appName, "credentials.yaml")
}

// FileCredentialStore is the default CredentialStore backed by a YAML file
// at a fixed path (typically the app-scoped path from
// DefaultAppCredentialPath). It reuses internal/credentials for loading,
// merging, and deleting entries — only the path differs from the daemon file.
//
// File layout is flat with exact provider names as keys:
//
//	# ~/.gogent/apps/my-agent/credentials.yaml
//	openai: "sk-..."
//	deepseek: "sk-..."
type FileCredentialStore struct {
	path string
}

var _ CredentialStore = (*FileCredentialStore)(nil)

// NewFileCredentialStore creates a store backed by the file at path.
func NewFileCredentialStore(path string) *FileCredentialStore {
	return &FileCredentialStore{path: path}
}

// Path returns the backing file path.
func (s *FileCredentialStore) Path() string {
	return s.path
}

func (s *FileCredentialStore) Get(providerName string) (string, error) {
	if s.path == "" {
		return "", nil
	}
	creds, err := credentials.LoadCredentials(s.path)
	if err != nil {
		return "", err
	}
	if v, ok := creds[providerName]; ok && v != "" {
		return v, nil
	}
	return "", nil
}

func (s *FileCredentialStore) Set(providerName, apiKey string) error {
	if s.path == "" {
		return nil
	}
	return credentials.WriteCredentials(s.path, map[string]string{providerName: apiKey})
}

func (s *FileCredentialStore) Delete(providerName string) error {
	if s.path == "" {
		return nil
	}
	return credentials.DeleteCredentials(s.path, providerName)
}

func (s *FileCredentialStore) List() ([]string, error) {
	if s.path == "" {
		return nil, nil
	}
	creds, err := credentials.LoadCredentials(s.path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(creds))
	for name := range creds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

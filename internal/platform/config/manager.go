package config

import (
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

type ValidationError struct {
	err error
}

func (e *ValidationError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

type Manager struct {
	mu   sync.Mutex
	path string
}

func NewManager(path string) *Manager {
	if path == "" {
		path = DefaultPath
	}
	return &Manager{path: path}
}

func (m *Manager) Path() string {
	if m == nil || m.path == "" {
		return DefaultPath
	}
	return m.path
}

func (m *Manager) Load() (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return LoadFile(m.Path())
}

func (m *Manager) Update(mutator func(*Config) error) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := LoadFile(m.Path())
	if err != nil {
		return Config{}, err
	}
	if err := mutator(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, &ValidationError{err: err}
	}
	if err := writeFile(m.Path(), cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (m *Manager) Replace(cfg Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := cfg.Validate(); err != nil {
		return &ValidationError{err: err}
	}
	return writeFile(m.Path(), cfg)
}

func writeFile(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

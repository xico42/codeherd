package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml"
)

// Config holds all codeherd configuration.
type Config struct {
	Defaults DefaultsConfig           `toml:"defaults"`
	Projects map[string]ProjectConfig `toml:"projects"`
	Agents   map[string]AgentConfig   `toml:"agents"`

	path string // runtime only, not serialized
}

// DefaultsConfig holds default values applied to every session.
type DefaultsConfig struct {
	ProjectsDir     string `toml:"projects_dir"`
	Agent           string `toml:"agent"`
	ProfilesEnabled bool   `toml:"profiles_enabled"`
	ProfilesDir     string `toml:"profiles_dir"`
	MainProfile     string `toml:"main_profile"`
}

const defaultProjectsDir = "~/projects"

// expandTilde replaces a leading "~/" with the user's home directory.
func expandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expanding ~: %w", err)
	}
	return home + path[1:], nil
}

// expandPaths resolves ~ in all path fields and applies defaults.
func (c *Config) expandPaths() error {
	if c.Defaults.ProjectsDir == "" {
		c.Defaults.ProjectsDir = defaultProjectsDir
	}
	var err error
	if c.Defaults.ProjectsDir, err = expandTilde(c.Defaults.ProjectsDir); err != nil {
		return err
	}
	return nil
}

// Load reads config from path. If path is empty, uses ~/.config/codeherd/config.toml.
// A missing file returns an empty Config with defaults and nil error.
//
// When the loaded config has defaults.profiles_enabled = true, Load resolves
// the active profile (profileName arg → defaults.main_profile → error), parses
// it, and returns a populated *ProfileRegistry. Stray projects/agents keys in
// the main config produce a one-time warning and are discarded.
func Load(path, profileName string) (*Config, *ProfileRegistry, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("getting home dir: %w", err)
		}
		path = filepath.Join(home, ".config", "codeherd", "config.toml")
	}

	cfg := &Config{path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := cfg.expandPaths(); err != nil {
			return nil, nil, err
		}
		return cfg, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if !cfg.Defaults.ProfilesEnabled {
		if err := cfg.expandPaths(); err != nil {
			return nil, nil, err
		}
		return cfg, nil, nil
	}

	// Profile mode: cfg's projects/agents/projects_dir are warned about
	// and discarded. Returned *Config comes from the profile file.
	return loadProfileMode(cfg, path, profileName)
}

// Save writes the config back to its file, creating directories as needed.
func (c *Config) Save() (err error) {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("creating config file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing config file: %w", cerr)
		}
	}()
	if err := toml.NewEncoder(f).Encode(c); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return nil
}

// Path returns the config file path.
func (c *Config) Path() string { return c.path }

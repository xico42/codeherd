package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/pelletier/go-toml"
)

var configValidator = validator.New()

const defaultImage = "ubuntu-24-04-x64"

// Config holds all codeherd configuration.
type Config struct {
	Defaults DefaultsConfig           `toml:"defaults"`
	Profiles map[string]ProfileConfig `toml:"profiles"`
	Projects map[string]ProjectConfig `toml:"projects"`
	Agents   map[string]AgentConfig   `toml:"agents"`

	path string // runtime only, not serialized
}

// DefaultsConfig holds default values applied to every droplet.
type DefaultsConfig struct {
	Token            string `toml:"token"             validate:"omitempty" secret:"true"`
	SSHKeyID         string `toml:"ssh_key_id"        validate:"omitempty"`
	Region           string `toml:"region"            validate:"omitempty"`
	Size             string `toml:"size"              validate:"omitempty"`
	TailscaleAuthKey string `toml:"tailscale_auth_key" validate:"omitempty" secret:"true"`
	Image            string `toml:"image"             validate:"omitempty"`
	ProjectsDir      string `toml:"projects_dir"      validate:"omitempty"`
	GitIdentityFile  string `toml:"git_identity_file" validate:"omitempty"`
	Agent            string `toml:"agent"             validate:"omitempty"`
}

// ProfileConfig holds per-profile overrides.
type ProfileConfig struct {
	Size   string `toml:"size"   validate:"omitempty"`
	Region string `toml:"region" validate:"omitempty"`
	Image  string `toml:"image"  validate:"omitempty"`
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
	if c.Defaults.GitIdentityFile, err = expandTilde(c.Defaults.GitIdentityFile); err != nil {
		return err
	}
	return nil
}

// Load reads config from path. If path is empty, uses ~/.config/codeherd/config.toml.
// A missing file returns an empty Config with defaults and nil error.
func Load(path string) (*Config, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home dir: %w", err)
		}
		path = filepath.Join(home, ".config", "codeherd", "config.toml")
	}

	cfg := &Config{path: path}
	cfg.Defaults.Image = defaultImage

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := cfg.expandPaths(); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if cfg.Defaults.Image == "" {
		cfg.Defaults.Image = defaultImage
	}
	if err := cfg.expandPaths(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Profile returns the named profile or an error if not found.
func (c *Config) Profile(name string) (ProfileConfig, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return ProfileConfig{}, fmt.Errorf("profile %q not found", name)
	}
	return p, nil
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

// ApplyEnv overlays DIGITALOCEAN_TOKEN, CODEHERD_REGION, CODEHERD_SIZE, CODEHERD_IMAGE,
// and TAILSCALE_AUTH_KEY environment variables onto the config.
func (c *Config) ApplyEnv() {
	if v := os.Getenv("DIGITALOCEAN_TOKEN"); v != "" {
		c.Defaults.Token = v
	}
	if v := os.Getenv("CODEHERD_REGION"); v != "" {
		c.Defaults.Region = v
	}
	if v := os.Getenv("CODEHERD_SIZE"); v != "" {
		c.Defaults.Size = v
	}
	if v := os.Getenv("CODEHERD_IMAGE"); v != "" {
		c.Defaults.Image = v
	}
	if v := os.Getenv("TAILSCALE_AUTH_KEY"); v != "" {
		c.Defaults.TailscaleAuthKey = v
	}
}

// ApplyFlags overlays CLI flag values; empty string means "not set".
func (c *Config) ApplyFlags(token string) {
	if token != "" {
		c.Defaults.Token = token
	}
}

// Validate runs struct validation on the config.
func (c *Config) Validate() error {
	if err := configValidator.Struct(c); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}
	return nil
}

// Redact masks a secret string, showing the first 9 and last 4 characters.
// Strings of 13 characters or fewer are fully masked.
func Redact(s string) string {
	if len(s) <= 13 {
		return "****"
	}
	return s[:9] + "****" + s[len(s)-4:]
}

// Path returns the config file path.
func (c *Config) Path() string { return c.path }

var (
	keyInitOnce  sync.Once
	defaultsKeys map[string]bool
	profileKeys  map[string]bool
	projectKeys  map[string]bool
)

func initKeyMaps() {
	keyInitOnce.Do(func() {
		defaultsKeys = tomlFields(reflect.TypeOf(DefaultsConfig{}))
		profileKeys = tomlFields(reflect.TypeOf(ProfileConfig{}))
		projectKeys = tomlFields(reflect.TypeOf(ProjectConfig{}))
	})
}

func tomlFields(t reflect.Type) map[string]bool {
	fields := make(map[string]bool)
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("toml")
		if tag != "" && tag != "-" {
			fields[tag] = true
		}
	}
	return fields
}

// SetKey sets a single config value by dot-notation key, preserving file comments.
// It re-reads the file before writing to avoid clobbering concurrent changes.
func (c *Config) SetKey(dotPath, value string) (err error) {
	if !IsValidKeyPath(dotPath) {
		return fmt.Errorf("unknown config key %q. Run 'ch config show' to see valid keys", dotPath)
	}

	data, err := os.ReadFile(c.path)
	var tree *toml.Tree
	var preamble string
	if errors.Is(err, fs.ErrNotExist) || len(data) == 0 {
		tree, err = toml.Load("")
		if err != nil {
			return fmt.Errorf("creating empty config tree: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("reading config: %w", err)
	} else {
		preamble = extractPreamble(string(data))
		tree, err = toml.LoadBytes(data)
		if err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	tree.Set(dotPath, value)

	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing config file: %w", cerr)
		}
	}()
	if preamble != "" {
		if _, err := fmt.Fprint(f, preamble); err != nil {
			return fmt.Errorf("writing preamble: %w", err)
		}
	}
	if _, err := tree.WriteTo(f); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	updated, err := Load(c.path)
	if err != nil {
		return err
	}
	return updated.Validate()
}

// extractPreamble returns all leading comment and blank lines from a TOML file
// (i.e. every line before the first non-comment, non-blank line).
func extractPreamble(content string) string {
	var sb strings.Builder
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			sb.WriteString(line)
		} else {
			break
		}
	}
	return sb.String()
}

// DeleteSection removes a dot-notation section from the config file.
// It is a no-op (returns nil) if the section does not exist or the file is missing.
func (c *Config) DeleteSection(dotPath string) (err error) {
	data, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // nothing to delete
	}
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	tree, err := toml.LoadBytes(data)
	if err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	if err := tree.Delete(dotPath); err != nil {
		// go-toml returns error for missing keys; treat as no-op.
		return nil
	}

	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing config file: %w", cerr)
		}
	}()
	if _, err = tree.WriteTo(f); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return nil
}

// IsValidKeyPath reports whether a dot-notation key path is settable via SetKey.
func IsValidKeyPath(dotPath string) bool {
	if dotPath == "" {
		return false
	}
	initKeyMaps()
	parts := strings.SplitN(dotPath, ".", 3)
	switch parts[0] {
	case "defaults":
		return len(parts) == 2 && defaultsKeys[parts[1]]
	case "profiles":
		return len(parts) == 3 && profileKeys[parts[2]]
	case "projects":
		return len(parts) == 3 && projectKeys[parts[2]]
	default:
		return false
	}
}

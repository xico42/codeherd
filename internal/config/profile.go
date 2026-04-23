package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml"
)

var warningSink io.Writer = os.Stderr

// SetWarningSink replaces the destination used by Load for one-time
// warnings. Pass nil to reset to os.Stderr. Intended for tests.
func SetWarningSink(w io.Writer) {
	if w == nil {
		warningSink = os.Stderr
		return
	}
	warningSink = w
}

// ProfileRegistry summarizes the set of discovered profiles and which
// one is active. Returned by Load alongside *Config when profile mode
// is on. Commands ignore it; the TUI uses it for cycling.
type ProfileRegistry struct {
	Active      string
	Names       []string
	ProfilesDir string
}

// LoadProfile parses <profilesDir>/<name>.toml and returns its Config.
// Errors when the file is missing or malformed.
func LoadProfile(profilesDir, name string) (*Config, error) {
	path := filepath.Join(profilesDir, name+".toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("profile %q not found at %s", name, path)
		}
		return nil, fmt.Errorf("reading profile %s: %w", path, err)
	}
	cfg := &Config{path: path}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing profile %s: %w", path, err)
	}
	if err := cfg.expandPaths(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// loadProfileMode resolves the active profile, parses it, warns about
// stray keys in the main config, and returns a clean *Config scoped to
// that profile plus a populated *ProfileRegistry.
func loadProfileMode(main *Config, mainPath, profileName string) (*Config, *ProfileRegistry, error) {
	profilesDir := main.Defaults.ProfilesDir
	if profilesDir == "" {
		profilesDir = filepath.Join(filepath.Dir(mainPath), "profiles")
	} else {
		expanded, err := expandTilde(profilesDir)
		if err != nil {
			return nil, nil, err
		}
		profilesDir = expanded
	}
	if st, err := os.Stat(profilesDir); err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("profiles_enabled=true but profiles_dir %q does not exist", profilesDir)
	}

	active := profileName
	if active == "" {
		active = main.Defaults.MainProfile
	}
	if active == "" {
		return nil, nil, fmt.Errorf("profiles_enabled=true but no profile specified: set defaults.main_profile in %s or pass -p/--profile", mainPath)
	}

	warnStrayKeys(main, mainPath)

	profCfg, err := LoadProfile(profilesDir, active)
	if err != nil {
		return nil, nil, err
	}
	// Profile-meta fields inside a profile file are ignored silently —
	// zero them out so callers can never accidentally consult them.
	profCfg.Defaults.ProfilesEnabled = false
	profCfg.Defaults.ProfilesDir = ""
	profCfg.Defaults.MainProfile = ""

	names, err := DiscoverProfiles(profilesDir)
	if err != nil {
		return nil, nil, err
	}
	reg := &ProfileRegistry{Active: active, Names: names, ProfilesDir: profilesDir}
	return profCfg, reg, nil
}

func warnStrayKeys(main *Config, mainPath string) {
	var stray []string
	if main.Defaults.ProjectsDir != "" {
		stray = append(stray, "defaults.projects_dir")
	}
	if main.Defaults.Agent != "" {
		stray = append(stray, "defaults.agent")
	}
	if len(main.Projects) > 0 {
		stray = append(stray, "projects")
	}
	if len(main.Agents) > 0 {
		stray = append(stray, "agents")
	}
	if len(stray) == 0 {
		return
	}
	fmt.Fprintf(warningSink, "warning: %s sets profiles_enabled=true; ignoring %s in main config\n", mainPath, strings.Join(stray, ", "))
}

// DiscoverProfiles lists profile names (filenames minus ".toml") in
// profilesDir. Non-TOML files are ignored. Result is sorted.
func DiscoverProfiles(profilesDir string) ([]string, error) {
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil, fmt.Errorf("reading profiles dir %s: %w", profilesDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		names = append(names, strings.TrimSuffix(name, ".toml"))
	}
	sort.Strings(names)
	return names, nil
}

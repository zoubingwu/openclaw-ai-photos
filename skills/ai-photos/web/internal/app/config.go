package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultHost = "127.0.0.1"
)

type Options struct {
	ProfileRef  string
	Host        string
	Port        int
	CacheDir    string
	OpenBrowser bool
}

type Config struct {
	BackendKind  string
	ConfigSource string
	ProfilePath  string
	DB9Target    string
	TiDBTarget   TiDBTarget
	Host         string
	Port         int
	CacheDir     string
	OpenBrowser  bool
}

type TiDBTarget struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
}

type profileDisk struct {
	Backend backendDisk `json:"backend"`
}

type backendDisk struct {
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	TargetFile string `json:"target_file"`
}

type envConfig struct {
	BackendKind string
	DB9Target   string
	TiDBTarget  TiDBTarget
}

func LoadConfig(opts Options) (Config, error) {
	cfg := Config{
		Host:        defaultOr(opts.Host, defaultHost),
		Port:        opts.Port,
		CacheDir:    defaultCacheDir(opts.CacheDir),
		OpenBrowser: opts.OpenBrowser,
	}

	profile, profilePath, err := loadProfile(opts.ProfileRef)
	if err != nil {
		return Config{}, err
	}
	env := loadEnvConfig()

	switch {
	case profile != nil:
		cfg.ProfilePath = profilePath
		cfg.BackendKind = profile.Backend.Kind
		cfg.ConfigSource = "profile"
		if profile.Backend.Kind == "db9" {
			cfg.DB9Target = profile.Backend.Target
			if cfg.DB9Target == "" && env.DB9Target != "" {
				cfg.DB9Target = env.DB9Target
				cfg.ConfigSource = "profile+env"
			}
		}
		if profile.Backend.Kind == "tidb" {
			target, err := loadTiDBTarget(profile.Backend.TargetFile)
			if err == nil {
				cfg.TiDBTarget = target
			}
			cfg.TiDBTarget = mergeTiDBTarget(cfg.TiDBTarget, env.TiDBTarget)
			if profile.Backend.TargetFile == "" || tidbTargetHasGap(cfg.TiDBTarget) {
				cfg.ConfigSource = "profile+env"
			}
		}
	case env.BackendKind != "":
		cfg.BackendKind = env.BackendKind
		cfg.ConfigSource = "env"
		cfg.DB9Target = env.DB9Target
		cfg.TiDBTarget = env.TiDBTarget
	default:
		return Config{}, errors.New("default album profile not found and env config is incomplete")
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultCacheDir(override string) string {
	if override != "" {
		return expandPath(override)
	}
	return filepath.Join(os.TempDir(), "ai-photos-web-cache")
}

func defaultOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultAlbumProfilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "ai-photos", "albums", "default.json")
}

func resolveProfilePath(ref string) string {
	if strings.TrimSpace(ref) == "" {
		return defaultAlbumProfilePath()
	}
	if isPathLike(ref) {
		path := expandPath(ref)
		if filepath.Ext(path) != ".json" {
			path += ".json"
		}
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "ai-photos", "albums", slugify(ref)+".json")
}

func isPathLike(value string) bool {
	return strings.Contains(value, "/") || strings.Contains(value, `\`) || strings.HasPrefix(value, "~") || strings.HasSuffix(value, ".json")
}

func slugify(value string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	slug := strings.Trim(re.ReplaceAllString(strings.TrimSpace(value), "-"), "-")
	if slug == "" {
		return "album"
	}
	return strings.ToLower(slug)
}

func expandPath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Clean(filepath.Join(home, strings.TrimPrefix(path, "~")))
		}
	}
	return filepath.Clean(path)
}

func loadProfile(ref string) (*profileDisk, string, error) {
	path := resolveProfilePath(ref)
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if ref != "" {
			return nil, "", fmt.Errorf("load profile: %w", err)
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("load default profile: %w", err)
	}

	var profile profileDisk
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, "", fmt.Errorf("parse profile: %w", err)
	}
	return &profile, path, nil
}

func loadEnvConfig() envConfig {
	return envConfig{
		BackendKind: strings.TrimSpace(os.Getenv("AI_PHOTOS_BACKEND")),
		DB9Target:   strings.TrimSpace(os.Getenv("AI_PHOTOS_DB9_TARGET")),
		TiDBTarget: TiDBTarget{
			Host:     strings.TrimSpace(os.Getenv("AI_PHOTOS_TIDB_HOST")),
			Username: strings.TrimSpace(os.Getenv("AI_PHOTOS_TIDB_USERNAME")),
			Password: os.Getenv("AI_PHOTOS_TIDB_PASSWORD"),
			Database: strings.TrimSpace(os.Getenv("AI_PHOTOS_TIDB_DATABASE")),
		},
	}
}

func loadTiDBTarget(path string) (TiDBTarget, error) {
	if strings.TrimSpace(path) == "" {
		return TiDBTarget{}, errors.New("empty TiDB target file")
	}
	data, err := os.ReadFile(expandPath(path))
	if err != nil {
		return TiDBTarget{}, err
	}
	var target TiDBTarget
	if err := json.Unmarshal(data, &target); err != nil {
		return TiDBTarget{}, err
	}
	return target, nil
}

func mergeTiDBTarget(profile, env TiDBTarget) TiDBTarget {
	if profile.Host == "" {
		profile.Host = env.Host
	}
	if profile.Username == "" {
		profile.Username = env.Username
	}
	if profile.Password == "" {
		profile.Password = env.Password
	}
	if profile.Database == "" {
		profile.Database = env.Database
	}
	return profile
}

func tidbTargetHasGap(target TiDBTarget) bool {
	return target.Host == "" || target.Username == "" || target.Password == "" || target.Database == ""
}

func validateConfig(cfg Config) error {
	switch cfg.BackendKind {
	case "db9":
		if strings.TrimSpace(cfg.DB9Target) == "" {
			return errors.New("db9 backend requires AI_PHOTOS_DB9_TARGET or a saved profile target")
		}
	case "tidb":
		if tidbTargetHasGap(cfg.TiDBTarget) {
			return errors.New("tidb backend requires host, username, password, and database")
		}
	default:
		return fmt.Errorf("unsupported backend %q", cfg.BackendKind)
	}

	if cfg.Port < 0 || cfg.Port > 65535 {
		return fmt.Errorf("invalid port %d", cfg.Port)
	}
	return nil
}

func (cfg Config) PortString() string {
	return strconv.Itoa(cfg.Port)
}

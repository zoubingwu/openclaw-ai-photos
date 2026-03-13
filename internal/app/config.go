package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultHost = "127.0.0.1"

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

type BackendSpec struct {
	Kind       string
	DB9Target  string
	TiDBTarget TiDBTarget
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

	profile, _, err := loadOptionalProfile(opts.ProfileRef)
	if err != nil {
		return Config{}, err
	}
	env := loadEnvConfig()

	switch {
	case profile != nil:
		cfg.ProfilePath = profile.Path
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

func (cfg Config) BackendSpec() BackendSpec {
	return BackendSpec{
		Kind:       cfg.BackendKind,
		DB9Target:  cfg.DB9Target,
		TiDBTarget: cfg.TiDBTarget,
	}
}

func (cfg Config) PortString() string {
	return strconv.Itoa(cfg.Port)
}

func defaultCacheDir(override string) string {
	if override != "" {
		return ExpandPath(override)
	}
	return filepath.Join(os.TempDir(), "ai-photos-cache")
}

func defaultOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
	return nil
}

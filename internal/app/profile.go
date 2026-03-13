package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	profileVersion     = 1
	defaultProfileName = "default"
)

type TiDBTarget struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
}

type BackendProfile struct {
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	TargetFile string `json:"target_file,omitempty"`
}

type MaintenanceProfile struct {
	Mode string `json:"mode"`
}

type AlbumProfile struct {
	Version     int                `json:"version"`
	AlbumID     string             `json:"album_id"`
	DisplayName string             `json:"display_name"`
	Sources     []string           `json:"sources"`
	Backend     BackendProfile     `json:"backend"`
	Maintenance MaintenanceProfile `json:"maintenance"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
	Path        string             `json:"-"`
}

func defaultAlbumProfilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openclaw", "ai-photos", "albums", defaultProfileName+".json")
}

func storagePaths() (baseDir string, albumsDir string, targetsDir string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", err
	}
	baseDir = filepath.Join(home, ".openclaw", "ai-photos")
	albumsDir = filepath.Join(baseDir, "albums")
	targetsDir = filepath.Join(baseDir, "targets")
	return baseDir, albumsDir, targetsDir, nil
}

func ensureStorageDirs() error {
	_, albumsDir, targetsDir, err := storagePaths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(albumsDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(targetsDir, 0o755)
}

func IsPathLike(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	return strings.Contains(value, "/") || strings.Contains(value, `\`) || strings.HasPrefix(value, "~") || strings.HasSuffix(value, ".json")
}

func Slugify(value string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	slug := strings.Trim(re.ReplaceAllString(strings.TrimSpace(value), "-"), "-")
	if slug == "" {
		return "album"
	}
	return strings.ToLower(slug)
}

func ExpandPath(path string) string {
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

func ResolveProfilePath(ref string) string {
	if strings.TrimSpace(ref) == "" {
		return defaultAlbumProfilePath()
	}
	if IsPathLike(ref) {
		path := ExpandPath(ref)
		if filepath.Ext(path) != ".json" {
			path += ".json"
		}
		return path
	}
	_, albumsDir, _, err := storagePaths()
	if err != nil {
		return ""
	}
	return filepath.Join(albumsDir, Slugify(ref)+".json")
}

func NormalizeSources(sources []string) ([]string, error) {
	normalized := make([]string, 0, len(sources))
	for _, source := range sources {
		path, err := filepath.Abs(ExpandPath(source))
		if err != nil {
			return nil, err
		}
		if slices.Contains(normalized, path) {
			continue
		}

		skip := false
		kept := make([]string, 0, len(normalized)+1)
		for _, existing := range normalized {
			if rel, err := filepath.Rel(existing, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				skip = true
				kept = append(kept, existing)
				continue
			}
			if rel, err := filepath.Rel(path, existing); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			kept = append(kept, existing)
		}
		if skip {
			normalized = kept
			continue
		}
		normalized = append(kept, path)
	}
	if len(normalized) == 0 {
		return nil, errors.New("at least one source is required")
	}
	return normalized, nil
}

func loadOptionalProfile(ref string) (*AlbumProfile, string, error) {
	path := ResolveProfilePath(ref)
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, path, nil
		}
		if ref != "" {
			return nil, path, fmt.Errorf("load profile: %w", err)
		}
		return nil, path, fmt.Errorf("load default profile: %w", err)
	}

	var profile AlbumProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, path, fmt.Errorf("parse profile: %w", err)
	}
	if profile.Version == 0 {
		return nil, path, fmt.Errorf("missing 'version' in profile: %s", path)
	}
	if strings.TrimSpace(profile.AlbumID) == "" {
		return nil, path, fmt.Errorf("missing 'album_id' in profile: %s", path)
	}
	if len(profile.Sources) == 0 {
		return nil, path, fmt.Errorf("missing or empty 'sources' in profile: %s", path)
	}
	switch profile.Backend.Kind {
	case "db9":
		if strings.TrimSpace(profile.Backend.Target) == "" {
			return nil, path, fmt.Errorf("missing 'target' in profile: %s", path)
		}
	case "tidb":
		if strings.TrimSpace(profile.Backend.TargetFile) == "" {
			return nil, path, fmt.Errorf("missing 'target_file' in profile: %s", path)
		}
		targetFile, err := filepath.Abs(ExpandPath(profile.Backend.TargetFile))
		if err != nil {
			return nil, path, err
		}
		if _, err := os.Stat(targetFile); err != nil {
			return nil, path, fmt.Errorf("missing TiDB target file for profile: %s", targetFile)
		}
		profile.Backend.TargetFile = targetFile
	default:
		return nil, path, fmt.Errorf("unsupported backend in profile: %s", profile.Backend.Kind)
	}

	sources, err := NormalizeSources(profile.Sources)
	if err != nil {
		return nil, path, err
	}
	profile.Sources = sources
	profile.Path = path
	return &profile, path, nil
}

func LoadProfile(ref string) (*AlbumProfile, error) {
	profile, path, err := loadOptionalProfile(ref)
	if err != nil {
		return nil, err
	}
	if profile != nil {
		return profile, nil
	}
	if ref != "" {
		return nil, fmt.Errorf("profile not found: %s", path)
	}
	return nil, fmt.Errorf("default album profile not found: %s", path)
}

func backendTargetFromProfile(profile *AlbumProfile) (string, string) {
	if profile.Backend.Kind == "db9" {
		return profile.Backend.Kind, profile.Backend.Target
	}
	return profile.Backend.Kind, profile.Backend.TargetFile
}

func targetMatches(backend, given, resolved string) bool {
	if backend == "tidb" {
		left, errLeft := filepath.Abs(ExpandPath(given))
		right, errRight := filepath.Abs(ExpandPath(resolved))
		return errLeft == nil && errRight == nil && left == right
	}
	return given == resolved
}

func ResolveBackendTarget(target, backend, profileRef string) (string, string, *AlbumProfile, error) {
	var (
		profile *AlbumProfile
		err     error
	)
	switch {
	case profileRef != "":
		profile, err = LoadProfile(profileRef)
	case strings.TrimSpace(target) == "":
		profile, err = LoadProfile("")
	}
	if err != nil {
		return "", "", nil, err
	}
	if profile == nil {
		if strings.TrimSpace(target) == "" {
			return "", "", nil, errors.New("target, --profile, or a saved default album profile is required")
		}
		if strings.TrimSpace(backend) == "" {
			backend = "db9"
		}
		return backend, target, nil, nil
	}

	profileBackend, profileTarget := backendTargetFromProfile(profile)
	if backend != "" && backend != profileBackend {
		return "", "", nil, fmt.Errorf("--backend %s does not match profile backend %s", backend, profileBackend)
	}
	if target != "" && !targetMatches(profileBackend, target, profileTarget) {
		return "", "", nil, errors.New("target does not match the selected profile")
	}
	return profileBackend, profileTarget, profile, nil
}

func ResolveSources(sources []string, profile *AlbumProfile) ([]string, error) {
	if profile == nil {
		return NormalizeSources(sources)
	}
	if len(sources) == 0 {
		return slices.Clone(profile.Sources), nil
	}
	resolved, err := NormalizeSources(sources)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(resolved, profile.Sources) {
		return nil, errors.New("sources do not match the selected profile")
	}
	return resolved, nil
}

func loadTiDBTarget(path string) (TiDBTarget, error) {
	if strings.TrimSpace(path) == "" {
		return TiDBTarget{}, errors.New("empty TiDB target file")
	}
	data, err := os.ReadFile(ExpandPath(path))
	if err != nil {
		return TiDBTarget{}, err
	}
	var target TiDBTarget
	if err := json.Unmarshal(data, &target); err != nil {
		return TiDBTarget{}, err
	}
	return target, nil
}

func SaveProfile(profileRef string, sources []string, backend, target, displayName, maintenanceMode string) (string, *AlbumProfile, error) {
	if err := ensureStorageDirs(); err != nil {
		return "", nil, err
	}
	profilePath := ResolveProfilePath(profileRef)
	if profilePath == "" {
		return "", nil, errors.New("profile path unavailable")
	}

	profileName := filepath.Base(strings.TrimSuffix(profilePath, filepath.Ext(profilePath)))
	if profileRef != "" && !IsPathLike(profileRef) {
		profileName = profileRef
	}
	albumID := Slugify(profileName)
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)

	existingCreatedAt := now
	existingDisplayName := ""
	if data, err := os.ReadFile(profilePath); err == nil {
		var existing AlbumProfile
		if json.Unmarshal(data, &existing) == nil {
			if existing.CreatedAt != "" {
				existingCreatedAt = existing.CreatedAt
			}
			existingDisplayName = existing.DisplayName
		}
	}

	sourcePaths, err := NormalizeSources(sources)
	if err != nil {
		return "", nil, err
	}

	backendConfig := BackendProfile{Kind: backend}
	if backend == "db9" {
		backendConfig.Target = target
	} else {
		tidbTarget, err := loadTiDBTarget(target)
		if err != nil {
			return "", nil, err
		}
		for _, key := range []string{tidbTarget.Host, tidbTarget.Username, tidbTarget.Password, tidbTarget.Database} {
			if strings.TrimSpace(key) == "" {
				return "", nil, errors.New("missing required TiDB target fields")
			}
		}
		_, _, targetsDir, err := storagePaths()
		if err != nil {
			return "", nil, err
		}
		targetPath := filepath.Join(targetsDir, albumID+".tidb.json")
		data, err := json.MarshalIndent(tidbTarget, "", "  ")
		if err != nil {
			return "", nil, err
		}
		data = append(data, '\n')
		if err := os.WriteFile(targetPath, data, 0o600); err != nil {
			return "", nil, err
		}
		backendConfig.TargetFile = targetPath
	}

	if maintenanceMode == "" {
		maintenanceMode = "heartbeat"
	}
	profile := &AlbumProfile{
		Version:     profileVersion,
		AlbumID:     albumID,
		DisplayName: firstNonEmpty(displayName, existingDisplayName, albumID),
		Sources:     sourcePaths,
		Backend:     backendConfig,
		Maintenance: MaintenanceProfile{Mode: maintenanceMode},
		CreatedAt:   existingCreatedAt,
		UpdatedAt:   now,
		Path:        profilePath,
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", nil, err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(profilePath, data, 0o600); err != nil {
		return "", nil, err
	}
	return profilePath, profile, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

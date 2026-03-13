package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var supportedImageExts = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".webp": {},
	".heic": {},
}

type ManifestRecord struct {
	FilePath  string         `json:"file_path"`
	Filename  string         `json:"filename"`
	SHA256    string         `json:"sha256"`
	MimeType  *string        `json:"mime_type"`
	SizeBytes int64          `json:"size_bytes"`
	Width     *int           `json:"width"`
	Height    *int           `json:"height"`
	TakenAt   *string        `json:"taken_at"`
	Exif      map[string]any `json:"exif"`
}

type ManifestSummary struct {
	OK      bool     `json:"ok"`
	Sources []string `json:"sources"`
	Output  string   `json:"output"`
	Count   int      `json:"count"`
}

type SyncSummary struct {
	OK                  bool     `json:"ok"`
	Backend             string   `json:"backend"`
	Target              string   `json:"target"`
	Sources             []string `json:"sources"`
	ManifestJSONL       string   `json:"manifest_jsonl"`
	IncrementalManifest string   `json:"incremental_manifest_jsonl"`
	TotalScanned        int      `json:"total_scanned"`
	Unchanged           int      `json:"unchanged"`
	ToCaption           int      `json:"to_caption"`
	BackendStatus       string   `json:"backend_status"`
	NextStep            string   `json:"next_step"`
}

func BuildManifest(sources []string, outputPath string) (ManifestSummary, error) {
	normalized, err := NormalizeSources(sources)
	if err != nil {
		return ManifestSummary{}, err
	}
	if outputPath == "" {
		return ManifestSummary{}, fmt.Errorf("output path is required")
	}
	outputPath = ExpandPath(outputPath)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return ManifestSummary{}, err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return ManifestSummary{}, err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	count := 0
	for _, source := range normalized {
		if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".") {
				return nil
			}
			if _, ok := supportedImageExts[strings.ToLower(filepath.Ext(entry.Name()))]; !ok {
				return nil
			}

			record, err := buildManifestRecord(path)
			if err != nil {
				return err
			}
			data, err := json.Marshal(record)
			if err != nil {
				return err
			}
			if _, err := writer.Write(append(data, '\n')); err != nil {
				return err
			}
			count++
			return nil
		}); err != nil {
			return ManifestSummary{}, err
		}
	}

	return ManifestSummary{
		OK:      true,
		Sources: normalized,
		Output:  outputPath,
		Count:   count,
	}, nil
}

func SyncPhotos(ctx context.Context, target, backend string, sources []string, profileRef, manifestOut string) (SyncSummary, error) {
	resolvedBackend, resolvedTarget, profile, err := ResolveBackendTarget(target, backend, profileRef)
	if err != nil {
		return SyncSummary{}, err
	}
	resolvedSources, err := ResolveSources(sources, profile)
	if err != nil {
		return SyncSummary{}, err
	}

	if manifestOut == "" {
		manifestOut = filepath.Join(os.TempDir(), "ai-photos.manifest.jsonl")
	}
	manifestOut = ExpandPath(manifestOut)
	incrementalPath := strings.TrimSuffix(manifestOut, filepath.Ext(manifestOut)) + ".incremental.jsonl"

	manifestSummary, err := BuildManifest(resolvedSources, manifestOut)
	if err != nil {
		return SyncSummary{}, err
	}
	allRecords, err := loadManifestMaps(manifestSummary.Output)
	if err != nil {
		return SyncSummary{}, err
	}

	existing := map[string]string{}
	backendStatus := "ok"
	store, err := OpenBackend(resolvedBackend, resolvedTarget)
	if err == nil {
		existing, err = fetchExistingRecords(ctx, store)
	}
	if err != nil {
		backendStatus = "fallback-full-scan: " + err.Error()
		existing = map[string]string{}
	}

	incremental := make([]map[string]any, 0, len(allRecords))
	unchanged := 0
	for _, record := range allRecords {
		path := stringValue(record["file_path"])
		if existing[path] == stringValue(record["sha256"]) {
			unchanged++
			continue
		}
		incremental = append(incremental, record)
	}
	if err := saveManifestMaps(incrementalPath, incremental); err != nil {
		return SyncSummary{}, err
	}

	return SyncSummary{
		OK:                  true,
		Backend:             resolvedBackend,
		Target:              resolvedTarget,
		Sources:             resolvedSources,
		ManifestJSONL:       manifestSummary.Output,
		IncrementalManifest: incrementalPath,
		TotalScanned:        len(allRecords),
		Unchanged:           unchanged,
		ToCaption:           len(incremental),
		BackendStatus:       backendStatus,
		NextStep:            "Use a vision-capable OpenClaw model to turn incremental_manifest_jsonl into captioned_jsonl that matches the caption schema, then run import.",
	}, nil
}

func buildManifestRecord(path string) (ManifestRecord, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return ManifestRecord{}, err
	}
	digest, err := sha256File(path)
	if err != nil {
		return ManifestRecord{}, err
	}
	width, height, takenAt := readPhotoMetadata(path)
	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	var mimePtr *string
	if mimeType != "" {
		mimePtr = &mimeType
	}
	return ManifestRecord{
		FilePath:  path,
		Filename:  filepath.Base(path),
		SHA256:    digest,
		MimeType:  mimePtr,
		SizeBytes: stat.Size(),
		Width:     width,
		Height:    height,
		TakenAt:   takenAt,
		Exif:      map[string]any{},
	}, nil
}

func readPhotoMetadata(path string) (*int, *int, *string) {
	if width, height, err := readDimensionsBuiltin(path); err == nil {
		return intPtr(width), intPtr(height), readDarwinContentCreationDate(path)
	}
	if sips, err := exec.LookPath("sips"); err == nil {
		backend := imageBackend{name: "sips", identifyCmd: []string{sips}, convertCmd: []string{sips}}
		if width, height, err := readDimensions(backend, path); err == nil {
			return intPtr(width), intPtr(height), readDarwinContentCreationDate(path)
		}
	}
	return nil, nil, readDarwinContentCreationDate(path)
}

func readDarwinContentCreationDate(path string) *string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	mdls, err := exec.LookPath("mdls")
	if err != nil {
		return nil
	}
	out, err := exec.Command(mdls, "-raw", "-name", "kMDItemContentCreationDate", path).CombinedOutput()
	if err != nil {
		return nil
	}
	value := strings.TrimSpace(string(out))
	if value == "" || value == "(null)" {
		return nil
	}
	layouts := []string{
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05 -0700 MST",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			formatted := parsed.UTC().Format(time.RFC3339)
			return &formatted
		}
	}
	return &value
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func loadManifestMaps(path string) ([]map[string]any, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records := make([]map[string]any, 0)
	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 1024*1024)
	scanner.Buffer(buffer, 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func saveManifestMaps(path string, records []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func fetchExistingRecords(ctx context.Context, backend Backend) (map[string]string, error) {
	rows, err := backend.Query(ctx, "SELECT file_path, sha256 FROM photos;")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		out[parseString(valueAt(row, 0))] = parseString(valueAt(row, 1))
	}
	return out, nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

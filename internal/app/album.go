package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const db9SchemaSQL = `
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS photos (
  id BIGSERIAL PRIMARY KEY,
  file_path TEXT NOT NULL UNIQUE,
  filename TEXT NOT NULL,
  sha256 VARCHAR(64) NOT NULL,
  mime_type TEXT,
  size_bytes BIGINT,
  width INT,
  height INT,
  taken_at TIMESTAMPTZ NULL,
  exif JSONB NOT NULL DEFAULT '{}'::jsonb,
  caption TEXT,
  tags JSONB NOT NULL DEFAULT '[]'::jsonb,
  scene TEXT,
  objects JSONB NOT NULL DEFAULT '[]'::jsonb,
  text_in_image TEXT,
  search_text TEXT NOT NULL DEFAULT '',
  embedding VECTOR(1536),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  indexed_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_photos_taken_at ON photos (taken_at);
CREATE INDEX IF NOT EXISTS idx_photos_sha256 ON photos (sha256);
CREATE INDEX IF NOT EXISTS idx_photos_tags ON photos USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_photos_objects ON photos USING GIN (objects);
CREATE INDEX IF NOT EXISTS idx_photos_fts ON photos USING GIN (to_tsvector('english', search_text));
`

const tidbSchemaSQL = `
CREATE TABLE IF NOT EXISTS photos (
  id BIGINT PRIMARY KEY AUTO_RANDOM,
  file_path TEXT NOT NULL,
  filename TEXT NOT NULL,
  sha256 VARCHAR(64) NOT NULL,
  mime_type TEXT,
  size_bytes BIGINT,
  width INT,
  height INT,
  taken_at TIMESTAMP NULL,
  exif JSON NOT NULL,
  caption TEXT,
  tags JSON NOT NULL,
  scene TEXT,
  objects JSON NOT NULL,
  text_in_image TEXT,
  search_text TEXT NOT NULL,
  embedding VECTOR(1536),
  metadata JSON NOT NULL,
  indexed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_file_path (file_path(255)),
  KEY idx_taken_at (taken_at),
  KEY idx_sha256 (sha256),
  FULLTEXT KEY ftx_search_text (search_text)
);
`

type InitSummary struct {
	OK      bool   `json:"ok"`
	Backend string `json:"backend"`
	Target  string `json:"target"`
}

type ImportSummary struct {
	OK       bool   `json:"ok"`
	Backend  string `json:"backend"`
	Target   string `json:"target"`
	Imported int    `json:"imported"`
}

type SetupSummary struct {
	OK                bool          `json:"ok"`
	ProfilePath       string        `json:"profile_path"`
	Profile           *AlbumProfile `json:"profile"`
	Init              InitSummary   `json:"init"`
	Sync              SyncSummary   `json:"sync"`
	CaptionInputJSONL string        `json:"caption_input_jsonl,omitempty"`
	NextStep          string        `json:"next_step,omitempty"`
}

const importBatchSize = 50

func InitSchema(ctx context.Context, target, backend, profileRef string) (InitSummary, error) {
	resolvedBackend, resolvedTarget, _, err := ResolveBackendTarget(target, backend, profileRef)
	if err != nil {
		return InitSummary{}, err
	}
	store, err := OpenBackend(resolvedBackend, resolvedTarget)
	if err != nil {
		return InitSummary{}, err
	}
	sql := db9SchemaSQL
	if resolvedBackend == "tidb" {
		sql = tidbSchemaSQL
	}
	if err := store.Exec(ctx, sql); err != nil {
		return InitSummary{}, err
	}
	return InitSummary{
		OK:      true,
		Backend: resolvedBackend,
		Target:  resolvedTarget,
	}, nil
}

func ImportRecords(ctx context.Context, target, jsonlPath, backend, profileRef string) (ImportSummary, error) {
	resolvedBackend, resolvedTarget, _, err := ResolveBackendTarget(target, backend, profileRef)
	if err != nil {
		return ImportSummary{}, err
	}
	store, err := OpenBackend(resolvedBackend, resolvedTarget)
	if err != nil {
		return ImportSummary{}, err
	}

	file, err := os.Open(ExpandPath(jsonlPath))
	if err != nil {
		return ImportSummary{}, err
	}
	defer file.Close()

	imported := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	batch := make([]map[string]any, 0, importBatchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := store.Exec(ctx, buildImportSQL(resolvedBackend, batch)); err != nil {
			return err
		}
		imported += len(batch)
		batch = batch[:0]
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		record := map[string]any{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return ImportSummary{}, err
		}
		normalizeRecord(record)
		batch = append(batch, record)
		if len(batch) == importBatchSize {
			if err := flushBatch(); err != nil {
				return ImportSummary{}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return ImportSummary{}, err
	}
	if err := flushBatch(); err != nil {
		return ImportSummary{}, err
	}

	return ImportSummary{
		OK:       true,
		Backend:  resolvedBackend,
		Target:   resolvedTarget,
		Imported: imported,
	}, nil
}

func SetupAlbum(ctx context.Context, profileRef string, sources []string, backend, target, displayName, maintenanceMode, manifestOut string) (SetupSummary, error) {
	profilePath, profile, err := SaveProfile(profileRef, sources, backend, target, displayName, maintenanceMode)
	if err != nil {
		return SetupSummary{}, err
	}
	initSummary, err := InitSchema(ctx, "", "", profilePath)
	if err != nil {
		return SetupSummary{}, fmt.Errorf("%w\nprofile saved at %s", err, profilePath)
	}
	syncSummary, err := SyncPhotos(ctx, "", "", nil, profilePath, manifestOut)
	if err != nil {
		return SetupSummary{}, fmt.Errorf("%w\nprofile saved at %s", err, profilePath)
	}
	return SetupSummary{
		OK:                true,
		ProfilePath:       profilePath,
		Profile:           profile,
		Init:              initSummary,
		Sync:              syncSummary,
		CaptionInputJSONL: syncSummary.IncrementalManifest,
		NextStep:          syncSummary.NextStep,
	}, nil
}

func normalizeRecord(record map[string]any) {
	if record["tags"] == nil {
		record["tags"] = []any{}
	}
	if record["objects"] == nil {
		record["objects"] = []any{}
	}
	if record["metadata"] == nil {
		record["metadata"] = map[string]any{}
	}
	if record["exif"] == nil {
		record["exif"] = map[string]any{}
	}
	if stringValue(record["search_text"]) == "" {
		record["search_text"] = buildSearchText(record)
	}
}

func buildSearchText(record map[string]any) string {
	parts := make([]string, 0, 8)
	for _, key := range []string{"caption", "scene", "text_in_image"} {
		if text := stringValue(record[key]); text != "" {
			parts = append(parts, text)
		}
	}
	for _, key := range []string{"tags", "objects"} {
		switch values := record[key].(type) {
		case []any:
			for _, value := range values {
				if text := stringValue(value); text != "" {
					parts = append(parts, text)
				}
			}
		case []string:
			parts = append(parts, values...)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func buildImportSQL(backend string, records []map[string]any) string {
	values := make([]string, 0, len(records))
	for _, record := range records {
		values = append(values, buildImportValues(backend, record))
	}

	if backend == "db9" {
		return fmt.Sprintf(`
INSERT INTO photos (
  file_path, filename, sha256, mime_type, size_bytes, width, height, taken_at,
  exif, caption, tags, scene, objects, text_in_image, search_text, metadata, indexed_at, updated_at
) VALUES
%s
ON CONFLICT (file_path) DO UPDATE SET
  sha256 = EXCLUDED.sha256,
  mime_type = EXCLUDED.mime_type,
  size_bytes = EXCLUDED.size_bytes,
  width = EXCLUDED.width,
  height = EXCLUDED.height,
  taken_at = EXCLUDED.taken_at,
  exif = EXCLUDED.exif,
  caption = EXCLUDED.caption,
  tags = EXCLUDED.tags,
  scene = EXCLUDED.scene,
  objects = EXCLUDED.objects,
  text_in_image = EXCLUDED.text_in_image,
  search_text = EXCLUDED.search_text,
  metadata = EXCLUDED.metadata,
  indexed_at = now(),
  updated_at = now();
`,
			strings.Join(values, ",\n"),
		)
	}

	return fmt.Sprintf(`
INSERT INTO photos (
  file_path, filename, sha256, mime_type, size_bytes, width, height, taken_at,
  exif, caption, tags, scene, objects, text_in_image, search_text, metadata, indexed_at, updated_at
) VALUES
%s
ON DUPLICATE KEY UPDATE
  sha256 = VALUES(sha256), mime_type = VALUES(mime_type), size_bytes = VALUES(size_bytes),
  width = VALUES(width), height = VALUES(height), taken_at = VALUES(taken_at), exif = VALUES(exif),
  caption = VALUES(caption), tags = VALUES(tags), scene = VALUES(scene), objects = VALUES(objects),
  text_in_image = VALUES(text_in_image), search_text = VALUES(search_text), metadata = VALUES(metadata),
  indexed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP;
`,
		strings.Join(values, ",\n"),
	)
}

func buildImportValues(backend string, record map[string]any) string {
	jsonCast := ""
	timestampValue := "CURRENT_TIMESTAMP"
	if backend == "db9" {
		jsonCast = "::jsonb"
		timestampValue = "now()"
	}
	return fmt.Sprintf(`(
  %s,
  %s,
  %s,
  %s,
  %s,
  %s,
  %s,
  %s,
  %s%s,
  %s,
  %s%s,
  %s,
  %s%s,
  %s,
  %s,
  %s%s,
  %s,
  %s
)`,
		sqlLiteral(record["file_path"]),
		sqlLiteral(record["filename"]),
		sqlLiteral(record["sha256"]),
		sqlLiteral(record["mime_type"]),
		sqlLiteral(record["size_bytes"]),
		sqlLiteral(record["width"]),
		sqlLiteral(record["height"]),
		sqlLiteral(record["taken_at"]),
		sqlLiteral(record["exif"]),
		jsonCast,
		sqlLiteral(record["caption"]),
		sqlLiteral(record["tags"]),
		jsonCast,
		sqlLiteral(record["scene"]),
		sqlLiteral(record["objects"]),
		jsonCast,
		sqlLiteral(record["text_in_image"]),
		sqlLiteral(record["search_text"]),
		sqlLiteral(record["metadata"]),
		jsonCast,
		timestampValue,
		timestampValue,
	)
}

func sqlLiteral(value any) string {
	switch typed := value.(type) {
	case nil:
		return "NULL"
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64:
		return fmt.Sprint(typed)
	case float32, float64:
		return fmt.Sprint(typed)
	case json.Number:
		return typed.String()
	case []any, []string, map[string]any:
		data, _ := json.Marshal(typed)
		return "'" + strings.ReplaceAll(string(data), "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprint(typed), "'", "''") + "'"
	}
}

func saveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

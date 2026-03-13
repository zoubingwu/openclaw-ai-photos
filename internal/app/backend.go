package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSearch = errors.New("invalid search parameters")
	ErrPhotoNotFound = errors.New("photo not found")
)

type Backend interface {
	Kind() string
	Health(context.Context) error
	Query(context.Context, string) ([][]any, error)
	Exec(context.Context, string) error
	Search(context.Context, SearchParams) (SearchResult, error)
}

type SearchParams struct {
	Text     string
	Tag      string
	Date     string
	Recent   bool
	Page     int
	PageSize int
}

type SearchResult struct {
	Items    []PhotoSummary `json:"items"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Total    int            `json:"total"`
	HasMore  bool           `json:"has_more"`
}

type PhotoSummary struct {
	ID          int64          `json:"id"`
	FilePath    string         `json:"file_path"`
	Filename    string         `json:"filename"`
	Caption     string         `json:"caption"`
	MimeType    string         `json:"mime_type"`
	TakenAt     string         `json:"taken_at"`
	Tags        []string       `json:"tags"`
	Scene       string         `json:"scene"`
	TextInImage string         `json:"text_in_image"`
	Exif        map[string]any `json:"exif"`
	Width       int            `json:"width"`
	Height      int            `json:"height"`
}

type db9Backend struct {
	target string
}

type tidbBackend struct {
	target TiDBTarget
	client *http.Client
}

func NewBackend(spec BackendSpec) (Backend, error) {
	switch spec.Kind {
	case "db9":
		if _, err := exec.LookPath("db9"); err != nil {
			return nil, fmt.Errorf("db9 backend selected but db9 CLI is not installed")
		}
		return &db9Backend{target: spec.DB9Target}, nil
	case "tidb":
		return &tidbBackend{
			target: spec.TiDBTarget,
			client: &http.Client{Timeout: 45 * time.Second},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported backend %q", spec.Kind)
	}
}

func OpenBackend(kind, target string) (Backend, error) {
	spec := BackendSpec{Kind: kind}
	switch kind {
	case "db9":
		spec.DB9Target = target
	case "tidb":
		tidbTarget, err := loadTiDBTarget(target)
		if err != nil {
			return nil, err
		}
		spec.TiDBTarget = tidbTarget
	default:
		return nil, fmt.Errorf("unsupported backend %q", kind)
	}
	return NewBackend(spec)
}

func (b *db9Backend) Kind() string {
	return "db9"
}

func (b *tidbBackend) Kind() string {
	return "tidb"
}

func (b *db9Backend) Health(ctx context.Context) error {
	return b.Exec(ctx, "SELECT 1;")
}

func (b *tidbBackend) Health(ctx context.Context) error {
	return b.Exec(ctx, "SELECT 1;")
}

func (b *db9Backend) Query(ctx context.Context, sql string) ([][]any, error) {
	data, err := b.runRaw(ctx, sql)
	if err != nil {
		return nil, err
	}
	var response struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse db9 response: %w", err)
	}
	return response.Rows, nil
}

func (b *db9Backend) Exec(ctx context.Context, sql string) error {
	_, err := b.runRaw(ctx, sql)
	return err
}

func (b *db9Backend) Search(ctx context.Context, params SearchParams) (SearchResult, error) {
	rows, total, err := searchWithRunner(ctx, b.Kind(), b.Query, params)
	if err != nil {
		return SearchResult{}, err
	}
	return buildSearchResult(params, total, rows), nil
}

func (b *db9Backend) runRaw(ctx context.Context, sql string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "db9", "--json", "db", "sql", b.target, "-q", sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("db9 query failed: %s", strings.TrimSpace(string(out)))
	}
	return out, nil
}

func (b *tidbBackend) Query(ctx context.Context, sql string) ([][]any, error) {
	data, err := b.runRaw(ctx, sql)
	if err != nil {
		return nil, err
	}
	var response struct {
		Rows []struct {
			Values []any `json:"values"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("parse tidb response: %w", err)
	}
	rows := make([][]any, 0, len(response.Rows))
	for _, row := range response.Rows {
		rows = append(rows, row.Values)
	}
	return rows, nil
}

func (b *tidbBackend) Exec(ctx context.Context, sql string) error {
	_, err := b.runRaw(ctx, sql)
	return err
}

func (b *tidbBackend) Search(ctx context.Context, params SearchParams) (SearchResult, error) {
	rows, total, err := searchWithRunner(ctx, b.Kind(), b.Query, params)
	if err != nil {
		return SearchResult{}, err
	}
	return buildSearchResult(params, total, rows), nil
}

func (b *tidbBackend) runRaw(ctx context.Context, sql string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"query": sql})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deriveTiDBHTTPHost(b.target.Host), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ai-photos/0.1")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(b.target.Username+":"+b.target.Password)))
	req.Header.Set("TiDB-Database", b.target.Database)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tidb query failed: %s", strings.TrimSpace(string(data)))
	}
	return data, nil
}

func deriveTiDBHTTPHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return strings.TrimRight(host, "/")
	}
	return "https://http-" + host + "/v1beta/sql"
}

func searchWithRunner(ctx context.Context, kind string, runner func(context.Context, string) ([][]any, error), params SearchParams) ([]PhotoSummary, int, error) {
	if err := validateSearchParams(params); err != nil {
		return nil, 0, err
	}
	listSQL, countSQL := buildSearchQueries(kind, normalizeSearchParams(params))
	totalRows, err := runner(ctx, countSQL)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	if len(totalRows) > 0 && len(totalRows[0]) > 0 {
		total = parseInt(totalRows[0][0])
	}

	rows, err := runner(ctx, listSQL)
	if err != nil {
		return nil, 0, err
	}
	items := make([]PhotoSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, parsePhotoSummary(row))
	}
	return items, total, nil
}

func validateSearchParams(params SearchParams) error {
	if params.Recent && (params.Text != "" || params.Tag != "" || params.Date != "") {
		return fmt.Errorf("%w: recent cannot be combined with text, tag, or date", ErrInvalidSearch)
	}
	return nil
}

func normalizeSearchParams(params SearchParams) SearchParams {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 18
	}
	if params.PageSize > 60 {
		params.PageSize = 60
	}
	return params
}

func buildSearchQueries(kind string, params SearchParams) (string, string) {
	where := buildWhereClause(kind, params)
	orderBy := "COALESCE(taken_at, created_at) DESC"
	if params.Recent {
		orderBy = "indexed_at DESC, created_at DESC"
	}
	offset := (params.Page - 1) * params.PageSize
	listSQL := fmt.Sprintf(
		"SELECT id, file_path, filename, caption, mime_type, taken_at, tags, scene, text_in_image, exif, width, height FROM photos WHERE %s ORDER BY %s LIMIT %d OFFSET %d;",
		where,
		orderBy,
		params.PageSize,
		offset,
	)
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM photos WHERE %s;", where)
	return listSQL, countSQL
}

func buildWhereClause(kind string, params SearchParams) string {
	parts := make([]string, 0, 3)
	if params.Date != "" {
		if kind == "db9" {
			parts = append(parts, fmt.Sprintf("taken_at::text LIKE '%s%%'", escapeSQL(params.Date)))
		} else {
			parts = append(parts, fmt.Sprintf("CAST(taken_at AS CHAR) LIKE '%s%%'", escapeSQL(params.Date)))
		}
	}
	if params.Text != "" {
		if kind == "db9" {
			parts = append(parts, fmt.Sprintf("to_tsvector('english', search_text) @@ websearch_to_tsquery('english', '%s')", escapeSQL(params.Text)))
		} else {
			parts = append(parts, fmt.Sprintf("search_text LIKE '%%%s%%'", escapeSQL(params.Text)))
		}
	}
	if params.Tag != "" {
		if kind == "db9" {
			parts = append(parts, fmt.Sprintf("tags @> jsonb_build_array('%s')", escapeSQL(params.Tag)))
		} else {
			parts = append(parts, fmt.Sprintf("JSON_CONTAINS(tags, JSON_ARRAY('%s'))", escapeSQL(params.Tag)))
		}
	}
	if len(parts) == 0 {
		if kind == "db9" {
			return "true"
		}
		return "1=1"
	}
	return strings.Join(parts, " AND ")
}

func escapeSQL(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func parsePhotoSummary(row []any) PhotoSummary {
	return PhotoSummary{
		ID:          parseInt64(valueAt(row, 0)),
		FilePath:    parseString(valueAt(row, 1)),
		Filename:    parseString(valueAt(row, 2)),
		Caption:     parseString(valueAt(row, 3)),
		MimeType:    parseString(valueAt(row, 4)),
		TakenAt:     parseString(valueAt(row, 5)),
		Tags:        parseStringSlice(valueAt(row, 6)),
		Scene:       parseString(valueAt(row, 7)),
		TextInImage: parseString(valueAt(row, 8)),
		Exif:        parseStringMap(valueAt(row, 9)),
		Width:       parseInt(valueAt(row, 10)),
		Height:      parseInt(valueAt(row, 11)),
	}
}

func buildSearchResult(params SearchParams, total int, items []PhotoSummary) SearchResult {
	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 {
		pageSize = 18
	}
	return SearchResult{
		Items:    items,
		Page:     page,
		PageSize: pageSize,
		Total:    total,
		HasMore:  page*pageSize < total,
	}
}

func valueAt(row []any, index int) any {
	if index < 0 || index >= len(row) {
		return nil
	}
	return row[index]
}

func parseString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func parseInt64(value any) int64 {
	if value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		out, _ := typed.Int64()
		return out
	case string:
		out, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return out
	default:
		out, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(typed)), 10, 64)
		return out
	}
}

func parseInt(value any) int {
	return int(parseInt64(value))
}

func parseStringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := parseString(item)
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		text := strings.TrimSpace(typed)
		if text == "" || text == "null" {
			return nil
		}
		if strings.HasPrefix(text, "[") {
			var decoded []string
			if err := json.Unmarshal([]byte(text), &decoded); err == nil {
				return decoded
			}
			var mixed []any
			if err := json.Unmarshal([]byte(text), &mixed); err == nil {
				return parseStringSlice(mixed)
			}
		}
		return []string{text}
	default:
		return []string{parseString(typed)}
	}
}

func parseStringMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	case string:
		text := strings.TrimSpace(typed)
		if text == "" || text == "null" {
			return map[string]any{}
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(text), &decoded); err == nil && decoded != nil {
			return decoded
		}
		return map[string]any{}
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return map[string]any{}
		}
		var decoded map[string]any
		if err := json.Unmarshal(data, &decoded); err == nil && decoded != nil {
			return decoded
		}
		return map[string]any{}
	}
}

func LocalURL(addr any) string {
	return "http://localhost:" + strconv.Itoa(PortFromAddr(addr))
}

func PortFromAddr(addr any) int {
	if typed, ok := addr.(*url.URL); ok {
		port, _ := strconv.Atoi(typed.Port())
		return port
	}
	value := fmt.Sprint(addr)
	parts := strings.Split(value, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])
	return port
}

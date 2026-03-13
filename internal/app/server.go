package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"ai-photos/frontend"
)

type Server struct {
	cfg      Config
	backend  Backend
	media    *MediaService
	staticFS fs.FS
}

func NewServer(cfg Config) (*Server, error) {
	backend, err := NewBackend(cfg.BackendSpec())
	if err != nil {
		return nil, err
	}
	media, err := NewMediaService(cfg.CacheDir)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:      cfg,
		backend:  backend,
		media:    media,
		staticFS: frontend.Files(),
	}, nil
}

func (s *Server) CheckReady(ctx context.Context) error {
	return s.backend.Health(ctx)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/media/thumb", s.handleMediaThumbPath)
	mux.HandleFunc("GET /api/media/preview", s.handleMediaPreviewPath)
	mux.HandleFunc("POST /api/open", s.handlePhotoOpenPath)
	mux.Handle("/", s.handleFrontend())
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"backend":       s.backend.Kind(),
		"config_source": s.cfg.ConfigSource,
		"profile_path":  s.cfg.ProfilePath,
		"cache_dir":     s.cfg.CacheDir,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	params := SearchParams{
		Text:     strings.TrimSpace(r.URL.Query().Get("text")),
		Tag:      strings.TrimSpace(r.URL.Query().Get("tag")),
		Date:     strings.TrimSpace(r.URL.Query().Get("date")),
		Recent:   parseBoolish(r.URL.Query().Get("recent")),
		Page:     parseIntWithFallback(r.URL.Query().Get("page"), 1),
		PageSize: parseIntWithFallback(r.URL.Query().Get("page_size"), 18),
	}

	result, err := s.backend.Search(r.Context(), params)
	if err != nil {
		s.writeError(w, err)
		return
	}

	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{
			"id":            item.ID,
			"file_path":     item.FilePath,
			"filename":      item.Filename,
			"caption":       item.Caption,
			"mime_type":     item.MimeType,
			"taken_at":      item.TakenAt,
			"tags":          item.Tags,
			"scene":         item.Scene,
			"text_in_image": item.TextInImage,
			"exif":          item.Exif,
			"width":         item.Width,
			"height":        item.Height,
			"thumb_url":     mediaURL("thumb", item.FilePath),
			"preview_url":   mediaURL("preview", item.FilePath),
			"open_url":      openURL(item.FilePath),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"page":      result.Page,
		"page_size": result.PageSize,
		"total":     result.Total,
		"has_more":  result.HasMore,
	})
}

func (s *Server) handleMediaThumbPath(w http.ResponseWriter, r *http.Request) {
	s.handleMediaPath(w, r, "thumb")
}

func (s *Server) handleMediaPreviewPath(w http.ResponseWriter, r *http.Request) {
	s.handleMediaPath(w, r, "preview")
}

func (s *Server) handleMediaPath(w http.ResponseWriter, r *http.Request, variant string) {
	filePath, err := parseRequestedPath(r)
	if err != nil {
		s.writeError(w, err)
		return
	}

	asset, err := s.media.ResolvePath(filePath, variant)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", asset.ContentType)
	if asset.Derived {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeFile(w, r, asset.Path)
}

func (s *Server) handlePhotoOpenPath(w http.ResponseWriter, r *http.Request) {
	filePath, err := parseRequestedPath(r)
	if err != nil {
		s.writeError(w, err)
		return
	}
	if _, err := filepath.Abs(filePath); err != nil {
		s.writeError(w, err)
		return
	}
	if err := OpenLocalFile(filePath); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"opened": filePath,
	})
}

func (s *Server) handleFrontend() http.Handler {
	fileServer := http.FileServer(http.FS(s.staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" {
			http.ServeFileFS(w, r, s.staticFS, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := err.Error()

	switch {
	case errors.Is(err, ErrInvalidSearch):
		status = http.StatusBadRequest
		code = "invalid_search"
	case errors.Is(err, ErrPhotoNotFound):
		status = http.StatusNotFound
		code = "photo_not_found"
	case strings.Contains(err.Error(), "backend"):
		status = http.StatusBadGateway
		code = "backend_unavailable"
	}

	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func parseRequestedPath(r *http.Request) (string, error) {
	rawPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if rawPath == "" {
		return "", fmt.Errorf("%w: missing file path", ErrInvalidSearch)
	}
	path, err := filepath.Abs(ExpandPath(rawPath))
	if err != nil {
		return "", err
	}
	return path, nil
}

func mediaURL(variant, filePath string) string {
	query := url.Values{}
	query.Set("path", filePath)
	return "/api/media/" + variant + "?" + query.Encode()
}

func openURL(filePath string) string {
	query := url.Values{}
	query.Set("path", filePath)
	return "/api/open?" + query.Encode()
}

func parseIntWithFallback(value string, fallback int) int {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	out, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return out
}

func parseBoolish(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

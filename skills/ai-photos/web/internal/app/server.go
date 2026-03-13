package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"ai-photos-web/frontend"
)

type Server struct {
	cfg      Config
	backend  Backend
	media    *MediaService
	staticFS fs.FS
}

func NewServer(cfg Config) (*Server, error) {
	backend, err := NewBackend(cfg)
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
	mux.HandleFunc("GET /api/photos/{id}", s.handlePhotoDetail)
	mux.HandleFunc("GET /api/photos/{id}/thumb", s.handlePhotoThumb)
	mux.HandleFunc("GET /api/photos/{id}/preview", s.handlePhotoPreview)
	mux.HandleFunc("POST /api/photos/{id}/open", s.handlePhotoOpen)
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
			"id":          item.ID,
			"filename":    item.Filename,
			"caption":     item.Caption,
			"taken_at":    item.TakenAt,
			"tags":        item.Tags,
			"width":       item.Width,
			"height":      item.Height,
			"thumb_url":   fmt.Sprintf("/api/photos/%d/thumb", item.ID),
			"preview_url": fmt.Sprintf("/api/photos/%d/preview", item.ID),
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

func (s *Server) handlePhotoDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseRouteID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}

	detail, err := s.backend.GetPhoto(r.Context(), id)
	if err != nil {
		s.writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":            detail.ID,
		"filename":      detail.Filename,
		"caption":       detail.Caption,
		"taken_at":      detail.TakenAt,
		"tags":          detail.Tags,
		"width":         detail.Width,
		"height":        detail.Height,
		"scene":         detail.Scene,
		"objects":       detail.Objects,
		"text_in_image": detail.TextInImage,
		"indexed_at":    detail.IndexedAt,
		"created_at":    detail.CreatedAt,
		"file_path":     detail.FilePath,
		"preview_url":   fmt.Sprintf("/api/photos/%d/preview", detail.ID),
		"open_url":      fmt.Sprintf("/api/photos/%d/open", detail.ID),
	})
}

func (s *Server) handlePhotoThumb(w http.ResponseWriter, r *http.Request) {
	s.handleMedia(w, r, "thumb")
}

func (s *Server) handlePhotoPreview(w http.ResponseWriter, r *http.Request) {
	s.handleMedia(w, r, "preview")
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request, variant string) {
	id, err := parseRouteID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}

	detail, err := s.backend.GetPhoto(r.Context(), id)
	if err != nil {
		s.writeError(w, err)
		return
	}

	asset, err := s.media.Resolve(detail, variant)
	if err != nil {
		s.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", asset.ContentType)
	if asset.Derived {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeFile(w, r, asset.Path)
}

func (s *Server) handlePhotoOpen(w http.ResponseWriter, r *http.Request) {
	id, err := parseRouteID(r)
	if err != nil {
		s.writeError(w, err)
		return
	}

	detail, err := s.backend.GetPhoto(r.Context(), id)
	if err != nil {
		s.writeError(w, err)
		return
	}
	if err := OpenLocalFile(detail.FilePath); err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"opened":   detail.FilePath,
		"filename": detail.Filename,
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

func parseRouteID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: invalid photo id", ErrInvalidSearch)
	}
	return id, nil
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

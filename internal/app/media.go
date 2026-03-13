package app

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type MediaService struct {
	cacheDir string
}

type MediaAsset struct {
	Path        string
	ContentType string
	Derived     bool
}

func NewMediaService(cacheDir string) (*MediaService, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &MediaService{cacheDir: cacheDir}, nil
}

func (m *MediaService) ResolvePath(filePath, variant string) (MediaAsset, error) {
	if _, err := os.Stat(filePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MediaAsset{}, ErrPhotoNotFound
		}
		return MediaAsset{}, err
	}

	var spec PrepareSpec
	switch variant {
	case "thumb":
		spec = MediaThumbSpec()
	case "preview":
		spec = MediaPreviewSpec()
	default:
		return MediaAsset{}, fmt.Errorf("unsupported variant %q", variant)
	}
	spec.OutputDir = m.cacheDir

	result, err := PrepareImage(filePath, spec)
	if err != nil {
		return MediaAsset{}, err
	}
	if result.OutputPath == filePath {
		return MediaAsset{
			Path:        filePath,
			ContentType: detectContentType(filePath),
			Derived:     false,
		}, nil
	}
	return MediaAsset{
		Path:        result.OutputPath,
		ContentType: "image/jpeg",
		Derived:     true,
	}, nil
}

func detectContentType(path string) string {
	ext := filepath.Ext(path)
	if contentType := mime.TypeByExtension(ext); contentType != "" {
		return contentType
	}
	file, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer file.Close()
	buffer := make([]byte, 512)
	n, _ := file.Read(buffer)
	return http.DetectContentType(buffer[:n])
}

func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Run()
	case "linux":
		return exec.Command("xdg-open", url).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Run()
	default:
		return nil
	}
}

func OpenLocalFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Run()
	case "linux":
		return exec.Command("xdg-open", path).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Run()
	default:
		return errors.New("open action is unsupported on this platform")
	}
}

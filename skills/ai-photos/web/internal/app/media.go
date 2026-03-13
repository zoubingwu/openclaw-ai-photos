package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

type MediaService struct {
	cacheDir string
	sipsPath string
}

type MediaAsset struct {
	Path        string
	ContentType string
	Derived     bool
}

type variantSpec struct {
	name    string
	maxEdge int
	quality string
}

var variants = map[string]variantSpec{
	"thumb": {
		name:    "thumb",
		maxEdge: 480,
		quality: "72",
	},
	"preview": {
		name:    "preview",
		maxEdge: 1800,
		quality: "82",
	},
}

func NewMediaService(cacheDir string) (*MediaService, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	sipsPath, _ := exec.LookPath("sips")
	return &MediaService{
		cacheDir: cacheDir,
		sipsPath: sipsPath,
	}, nil
}

func (m *MediaService) Resolve(detail PhotoDetail, variant string) (MediaAsset, error) {
	spec, ok := variants[variant]
	if !ok {
		return MediaAsset{}, fmt.Errorf("unsupported variant %q", variant)
	}

	stat, err := os.Stat(detail.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MediaAsset{}, ErrPhotoNotFound
		}
		return MediaAsset{}, err
	}

	if m.sipsPath == "" {
		return MediaAsset{
			Path:        detail.FilePath,
			ContentType: detectContentType(detail.FilePath),
			Derived:     false,
		}, nil
	}

	key := fmt.Sprintf("%s:%d:%d:%s", detail.FilePath, stat.Size(), stat.ModTime().UnixNano(), spec.name)
	sum := sha256.Sum256([]byte(key))
	outPath := filepath.Join(m.cacheDir, hex.EncodeToString(sum[:])+".jpg")
	if cachedFresh(outPath, stat.ModTime().UnixNano()) {
		return MediaAsset{
			Path:        outPath,
			ContentType: "image/jpeg",
			Derived:     true,
		}, nil
	}

	tmpPath := outPath + ".tmp"
	_ = os.Remove(tmpPath)
	cmd := exec.Command(
		m.sipsPath,
		"-s", "format", "jpeg",
		"-s", "formatOptions", spec.quality,
		"-Z", strconv.Itoa(spec.maxEdge),
		detail.FilePath,
		"--out", tmpPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return MediaAsset{}, fmt.Errorf("derive %s: %s", variant, string(output))
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return MediaAsset{}, err
	}
	return MediaAsset{
		Path:        outPath,
		ContentType: "image/jpeg",
		Derived:     true,
	}, nil
}

func cachedFresh(path string, sourceVersion int64) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0 && info.ModTime().UnixNano() >= sourceVersion
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

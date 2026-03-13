package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var directCaptionExts = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".webp": {},
}

var derivedImageLocks sync.Map

type PrepareSpec struct {
	Name                 string
	MaxEdge              int
	PassthroughEdge      int
	Quality              string
	OutputDir            string
	AllowDirectFallback  bool
	AllowOriginalOnError bool
}

type PrepareResult struct {
	OK              bool   `json:"ok"`
	UsedOriginal    bool   `json:"used_original"`
	InputPath       string `json:"input_path"`
	OutputPath      string `json:"output_path"`
	Backend         string `json:"backend"`
	Mode            string `json:"mode"`
	Width           *int   `json:"width"`
	Height          *int   `json:"height"`
	MaxEdge         int    `json:"max_edge"`
	PassthroughEdge int    `json:"passthrough_edge"`
	Quality         string `json:"quality"`
	Degraded        bool   `json:"degraded,omitempty"`
	Warning         string `json:"warning,omitempty"`
	Platform        string `json:"platform,omitempty"`
}

type imageBackend struct {
	name        string
	identifyCmd []string
	convertCmd  []string
}

func CaptionPrepareSpec() PrepareSpec {
	return PrepareSpec{
		Name:                "caption",
		MaxEdge:             1536,
		PassthroughEdge:     1600,
		Quality:             "75",
		AllowDirectFallback: true,
	}
}

func PreviewPrepareSpec() PrepareSpec {
	return PrepareSpec{
		Name:                 "preview",
		MaxEdge:              2048,
		PassthroughEdge:      2200,
		Quality:              "80",
		AllowOriginalOnError: false,
	}
}

func MediaThumbSpec() PrepareSpec {
	return PrepareSpec{
		Name:                 "thumb",
		MaxEdge:              640,
		PassthroughEdge:      1280,
		Quality:              "72",
		AllowOriginalOnError: true,
	}
}

func MediaPreviewSpec() PrepareSpec {
	return PrepareSpec{
		Name:                 "preview",
		MaxEdge:              1800,
		PassthroughEdge:      2400,
		Quality:              "82",
		AllowOriginalOnError: true,
	}
}

func PrepareImage(path string, spec PrepareSpec) (PrepareResult, error) {
	source, err := filepath.Abs(ExpandPath(path))
	if err != nil {
		return PrepareResult{}, err
	}
	info, err := os.Stat(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PrepareResult{}, fmt.Errorf("image file not found: %s", source)
		}
		return PrepareResult{}, err
	}
	if info.IsDir() {
		return PrepareResult{}, fmt.Errorf("image file not found: %s", source)
	}

	if width, height, err := readDimensionsBuiltin(source); err == nil {
		if max(width, height) <= spec.PassthroughEdge {
			return buildPrepareResult(source, source, "builtin", spec, width, height, true), nil
		}
	}

	backends := availableImageBackends()
	failures := make([]string, 0, len(backends))
	for _, backend := range backends {
		result, err := prepareWithBackend(source, spec, backend)
		if err == nil {
			return result, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", backend.name, err))
	}

	if spec.AllowDirectFallback && canFallbackToOriginal(source) {
		message := "no local image backend found; using the original file path for captioning"
		if len(backends) > 0 {
			message = "all local image backends failed; using the original file path for captioning"
		}
		return PrepareResult{
			OK:              true,
			UsedOriginal:    true,
			InputPath:       source,
			OutputPath:      source,
			Backend:         "direct",
			Mode:            spec.Name,
			MaxEdge:         spec.MaxEdge,
			PassthroughEdge: spec.PassthroughEdge,
			Quality:         spec.Quality,
			Degraded:        true,
			Warning:         message,
			Platform:        runtime.GOOS,
		}, nil
	}

	if spec.AllowOriginalOnError {
		return PrepareResult{
			OK:              true,
			UsedOriginal:    true,
			InputPath:       source,
			OutputPath:      source,
			Backend:         "original",
			Mode:            spec.Name,
			MaxEdge:         spec.MaxEdge,
			PassthroughEdge: spec.PassthroughEdge,
			Quality:         spec.Quality,
			Degraded:        true,
			Warning:         "no local image backend available; using the original file",
			Platform:        runtime.GOOS,
		}, nil
	}

	if len(backends) == 0 {
		return PrepareResult{}, errors.New("no supported image backend found; install ImageMagick or run on macOS with sips")
	}
	return PrepareResult{}, fmt.Errorf("could not prepare image with any supported backend: %s", strings.Join(failures, "; "))
}

func availableImageBackends() []imageBackend {
	backends := make([]imageBackend, 0, 3)
	if sips, err := exec.LookPath("sips"); err == nil {
		backends = append(backends, imageBackend{
			name:        "sips",
			identifyCmd: []string{sips},
			convertCmd:  []string{sips},
		})
	}
	if magick, err := exec.LookPath("magick"); err == nil {
		backends = append(backends, imageBackend{
			name:        "magick",
			identifyCmd: []string{magick, "identify"},
			convertCmd:  []string{magick},
		})
	}
	identify, identifyErr := exec.LookPath("identify")
	convert, convertErr := exec.LookPath("convert")
	if identifyErr == nil && convertErr == nil {
		backends = append(backends, imageBackend{
			name:        "imagemagick",
			identifyCmd: []string{identify},
			convertCmd:  []string{convert},
		})
	}
	return backends
}

func prepareWithBackend(source string, spec PrepareSpec, backend imageBackend) (PrepareResult, error) {
	width, height, err := readDimensions(backend, source)
	if err != nil {
		return PrepareResult{}, err
	}
	if max(width, height) <= spec.PassthroughEdge {
		return buildPrepareResult(source, source, backend.name, spec, width, height, true), nil
	}

	output := deriveOutputPath(source, spec)
	return withDerivedImageLock(output, func() (PrepareResult, error) {
		if outWidth, outHeight, ok := readCachedDerivedImage(output); ok {
			return buildPrepareResult(source, output, backend.name, spec, outWidth, outHeight, false), nil
		}
		if err := writeDerivedImage(backend, source, output, spec); err != nil {
			return PrepareResult{}, err
		}
		outWidth, outHeight, err := readDimensions(backend, output)
		if err != nil {
			return PrepareResult{}, err
		}
		return buildPrepareResult(source, output, backend.name, spec, outWidth, outHeight, false), nil
	})
}

func readCachedDerivedImage(path string) (int, int, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return 0, 0, false
	}
	width, height, err := readDimensionsBuiltin(path)
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}

func withDerivedImageLock(path string, fn func() (PrepareResult, error)) (PrepareResult, error) {
	lockValue, _ := derivedImageLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}

func deriveOutputPath(path string, spec PrepareSpec) string {
	size := int64(0)
	modTime := int64(0)
	if stat, err := os.Stat(path); err == nil {
		size = stat.Size()
		modTime = stat.ModTime().UnixNano()
	}
	key := fmt.Sprintf("%s:%d:%d:%s", path, size, modTime, spec.Name)
	sum := sha256.Sum256([]byte(key))
	outputDir := spec.OutputDir
	if outputDir == "" {
		outputDir = filepath.Join(os.TempDir(), "ai-photos-derived")
	}
	return filepath.Join(outputDir, hex.EncodeToString(sum[:8])+"."+spec.Name+".jpg")
}

func writeDerivedImage(backend imageBackend, source, output string, spec PrepareSpec) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	tmpPath := output + ".tmp"
	_ = os.Remove(tmpPath)

	var cmd *exec.Cmd
	switch backend.name {
	case "sips":
		cmd = exec.Command(
			backend.convertCmd[0],
			"-s", "format", "jpeg",
			"-s", "formatOptions", spec.Quality,
			"-Z", strconv.Itoa(spec.MaxEdge),
			source,
			"--out", tmpPath,
		)
	case "magick":
		cmd = exec.Command(
			backend.convertCmd[0],
			source,
			"-auto-orient",
			"-strip",
			"-resize", fmt.Sprintf("%dx%d>", spec.MaxEdge, spec.MaxEdge),
			"-quality", spec.Quality,
			tmpPath,
		)
	default:
		cmd = exec.Command(
			backend.convertCmd[0],
			source,
			"-auto-orient",
			"-strip",
			"-resize", fmt.Sprintf("%dx%d>", spec.MaxEdge, spec.MaxEdge),
			"-quality", spec.Quality,
			tmpPath,
		)
	}

	outputBytes, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("derive image: %s", strings.TrimSpace(string(outputBytes)))
	}
	return os.Rename(tmpPath, output)
}

func readDimensions(backend imageBackend, path string) (int, int, error) {
	switch backend.name {
	case "sips":
		out, err := exec.Command(backend.identifyCmd[0], "-g", "pixelWidth", "-g", "pixelHeight", path).CombinedOutput()
		if err != nil {
			return 0, 0, fmt.Errorf("read dimensions: %s", strings.TrimSpace(string(out)))
		}
		widthMatch := regexp.MustCompile(`pixelWidth:\s*(\d+)`).FindStringSubmatch(string(out))
		heightMatch := regexp.MustCompile(`pixelHeight:\s*(\d+)`).FindStringSubmatch(string(out))
		if len(widthMatch) != 2 || len(heightMatch) != 2 {
			return 0, 0, fmt.Errorf("could not read image dimensions: %s", path)
		}
		width, _ := strconv.Atoi(widthMatch[1])
		height, _ := strconv.Atoi(heightMatch[1])
		return width, height, nil
	default:
		cmdArgs := append(append([]string{}, backend.identifyCmd...), "-format", "%w %h", path)
		out, err := exec.Command(cmdArgs[0], cmdArgs[1:]...).CombinedOutput()
		if err != nil {
			return 0, 0, fmt.Errorf("read dimensions: %s", strings.TrimSpace(string(out)))
		}
		parts := strings.Fields(string(out))
		if len(parts) != 2 {
			return 0, 0, fmt.Errorf("could not read image dimensions: %s", path)
		}
		width, _ := strconv.Atoi(parts[0])
		height, _ := strconv.Atoi(parts[1])
		return width, height, nil
	}
}

func readDimensionsBuiltin(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}
	return cfg.Width, cfg.Height, nil
}

func canFallbackToOriginal(path string) bool {
	_, ok := directCaptionExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

func buildPrepareResult(source, output, backend string, spec PrepareSpec, width, height int, usedOriginal bool) PrepareResult {
	return PrepareResult{
		OK:              true,
		UsedOriginal:    usedOriginal,
		InputPath:       source,
		OutputPath:      output,
		Backend:         backend,
		Mode:            spec.Name,
		Width:           intPtr(width),
		Height:          intPtr(height),
		MaxEdge:         spec.MaxEdge,
		PassthroughEdge: spec.PassthroughEdge,
		Quality:         spec.Quality,
	}
}

func intPtr(value int) *int {
	return &value
}

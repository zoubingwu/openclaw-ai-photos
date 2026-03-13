#!/usr/bin/env python3
import argparse
import hashlib
import json
import platform
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

try:
    from PIL import Image, ImageOps
except Exception:
    Image = None
    ImageOps = None

MODES = {
    "caption": {
        "max_edge": 1536,
        "passthrough_edge": 1600,
        "quality": "75",
    },
    "preview": {
        "max_edge": 2048,
        "passthrough_edge": 2200,
        "quality": "80",
    },
}

DIRECT_CAPTION_EXTS = {".jpg", ".jpeg", ".png", ".webp"}


def run(cmd):
    """Run a subprocess command and return stdout or raise a readable error."""
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True)
    except FileNotFoundError as e:
        raise RuntimeError(f"command not found: {cmd[0]}") from e
    if proc.returncode != 0:
        message = proc.stderr.strip() or proc.stdout.strip() or f"command failed: {' '.join(cmd)}"
        raise RuntimeError(message)
    return proc.stdout


def available_backends():
    """Return the supported image backends in preference order."""
    backends = []
    sips_bin = shutil.which("sips")
    if sips_bin:
        backends.append({"name": "sips", "identify_cmd": [sips_bin], "convert_cmd": [sips_bin]})
    if Image is not None:
        backends.append({"name": "pillow"})
    magick_bin = shutil.which("magick")
    if magick_bin:
        backends.append({"name": "magick", "identify_cmd": [magick_bin, "identify"], "convert_cmd": [magick_bin]})
    identify_bin = shutil.which("identify")
    convert_bin = shutil.which("convert")
    if not magick_bin and identify_bin and convert_bin:
        backends.append({"name": "imagemagick", "identify_cmd": [identify_bin], "convert_cmd": [convert_bin]})
    return backends


def read_dimensions_with_sips(sips_bin, path):
    """Read raster dimensions from an image file through sips."""
    out = run([sips_bin, "-g", "pixelWidth", "-g", "pixelHeight", str(path)])
    width_match = re.search(r"pixelWidth:\s*(\d+)", out)
    height_match = re.search(r"pixelHeight:\s*(\d+)", out)
    if not width_match or not height_match:
        raise RuntimeError(f"could not read image dimensions: {path}")
    return int(width_match.group(1)), int(height_match.group(1))


def read_dimensions_with_pillow(path):
    """Read raster dimensions through Pillow."""
    try:
        with Image.open(path) as img:
            return img.size
    except Exception as e:
        raise RuntimeError(f"could not read image dimensions: {path}: {e}") from e


def read_dimensions_with_imagemagick(cmd, path):
    """Read raster dimensions through ImageMagick."""
    out = run([*cmd, "-format", "%w %h", str(path)]).strip()
    parts = out.split()
    if len(parts) != 2:
        raise RuntimeError(f"could not read image dimensions: {path}")
    return int(parts[0]), int(parts[1])


def read_dimensions(backend, path):
    """Read raster dimensions using the selected backend."""
    if backend["name"] == "sips":
        return read_dimensions_with_sips(backend["identify_cmd"][0], path)
    if backend["name"] == "pillow":
        return read_dimensions_with_pillow(path)
    return read_dimensions_with_imagemagick(backend["identify_cmd"], path)


def derive_output_path(path, mode):
    """Build a stable temporary JPEG path for a derived image mode."""
    stat = path.stat()
    key = f"{path}:{stat.st_size}:{stat.st_mtime_ns}:{mode}"
    digest = hashlib.sha256(key.encode("utf-8")).hexdigest()[:16]
    out_dir = Path(tempfile.gettempdir()) / "ai-photos-derived"
    out_dir.mkdir(parents=True, exist_ok=True)
    return out_dir / f"{digest}.{mode}.jpg"


def write_jpeg_with_sips(sips_bin, source, output, spec):
    """Create a resized JPEG derivative through sips."""
    run([
        sips_bin,
        "-s",
        "format",
        "jpeg",
        "-s",
        "formatOptions",
        spec["quality"],
        "-Z",
        str(spec["max_edge"]),
        str(source),
        "--out",
        str(output),
    ])


def pillow_resample():
    """Return the best available Pillow resampling filter."""
    return getattr(getattr(Image, "Resampling", Image), "LANCZOS")


def write_jpeg_with_pillow(source, output, spec):
    """Create a resized JPEG derivative through Pillow."""
    try:
        with Image.open(source) as img:
            if ImageOps and hasattr(ImageOps, "exif_transpose"):
                img = ImageOps.exif_transpose(img)
            if img.mode != "RGB":
                img = img.convert("RGB")
            img.thumbnail((spec["max_edge"], spec["max_edge"]), pillow_resample())
            img.save(output, format="JPEG", quality=int(spec["quality"]))
    except Exception as e:
        raise RuntimeError(f"failed to prepare image with Pillow: {e}") from e


def write_jpeg_with_imagemagick(cmd, source, output, spec):
    """Create a resized JPEG derivative through ImageMagick."""
    run([
        *cmd,
        str(source),
        "-auto-orient",
        "-strip",
        "-resize",
        f"{spec['max_edge']}x{spec['max_edge']}>",
        "-quality",
        spec["quality"],
        str(output),
    ])


def write_jpeg(backend, source, output, spec):
    """Create a JPEG derivative using the selected backend."""
    if backend["name"] == "sips":
        write_jpeg_with_sips(backend["convert_cmd"][0], source, output, spec)
        return
    if backend["name"] == "pillow":
        write_jpeg_with_pillow(source, output, spec)
        return
    write_jpeg_with_imagemagick(backend["convert_cmd"], source, output, spec)


def prepare_with_backend(source, mode, backend):
    """Prepare one image with one concrete backend."""
    spec = MODES[mode]
    width, height = read_dimensions(backend, source)
    longest_edge = max(width, height)
    if longest_edge <= spec["passthrough_edge"]:
        return {
            "ok": True,
            "used_original": True,
            "input_path": str(source),
            "output_path": str(source),
            "backend": backend["name"],
            "mode": mode,
            "width": width,
            "height": height,
            "max_edge": spec["max_edge"],
            "passthrough_edge": spec["passthrough_edge"],
            "quality": spec["quality"],
        }

    output = derive_output_path(source, mode)
    write_jpeg(backend, source, output, spec)
    out_width, out_height = read_dimensions(backend, output)
    return {
        "ok": True,
        "used_original": False,
        "input_path": str(source),
        "output_path": str(output),
        "backend": backend["name"],
        "mode": mode,
        "width": out_width,
        "height": out_height,
        "max_edge": spec["max_edge"],
        "passthrough_edge": spec["passthrough_edge"],
        "quality": spec["quality"],
    }


def can_fallback_to_original(source, mode):
    """Allow caption mode to use the original file for formats the model can consume directly."""
    return mode == "caption" and source.suffix.lower() in DIRECT_CAPTION_EXTS


def build_original_fallback_result(source, mode, reason):
    """Return a successful direct-file fallback when no local image backend is available."""
    spec = MODES[mode]
    return {
        "ok": True,
        "used_original": True,
        "input_path": str(source),
        "output_path": str(source),
        "backend": "direct",
        "mode": mode,
        "width": None,
        "height": None,
        "max_edge": spec["max_edge"],
        "passthrough_edge": spec["passthrough_edge"],
        "quality": spec["quality"],
        "degraded": True,
        "warning": reason,
        "platform": platform.system(),
    }


def prepare_image(path, mode):
    """Return either the original path or a compressed JPEG derivative for the requested mode."""
    source = Path(path).expanduser().resolve()
    if not source.is_file():
        raise ValueError(f"image file not found: {source}")

    backends = available_backends()
    errors = []
    for backend in backends:
        try:
            return prepare_with_backend(source, mode, backend)
        except RuntimeError as e:
            errors.append(f"{backend['name']}: {e}")
    if can_fallback_to_original(source, mode):
        if not backends:
            return build_original_fallback_result(
                source,
                mode,
                "no local image backend found; using the original file path for captioning",
            )
        return build_original_fallback_result(
            source,
            mode,
            "all local image backends failed; using the original file path for captioning",
        )
    if not backends:
        raise RuntimeError(
            "no supported image backend found; install Pillow or ImageMagick on Linux, or run on macOS with sips"
        )
    raise RuntimeError("could not prepare image with any supported backend: " + "; ".join(errors))


def main():
    """Prepare an image for captioning or preview delivery using any supported local image backend."""
    ap = argparse.ArgumentParser(description="Prepare a smaller temporary image for captioning or preview delivery")
    ap.add_argument("image", help="source image path")
    ap.add_argument("--mode", choices=sorted(MODES), required=True, help="derivation mode: caption or preview")
    args = ap.parse_args()
    try:
        result = prepare_image(args.image, args.mode)
    except (RuntimeError, ValueError) as e:
        sys.stderr.write(f"{e}\n")
        sys.exit(1)
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

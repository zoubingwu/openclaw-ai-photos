#!/usr/bin/env python3
import argparse
import hashlib
import json
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

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


def require_sips():
    """Resolve the system sips binary or fail with a clear error."""
    sips_bin = shutil.which("sips")
    if not sips_bin:
        raise RuntimeError("sips is required but was not found on this system")
    return sips_bin


def read_dimensions(sips_bin, path):
    """Read raster dimensions from an image file through sips."""
    out = run([sips_bin, "-g", "pixelWidth", "-g", "pixelHeight", str(path)])
    width_match = re.search(r"pixelWidth:\s*(\d+)", out)
    height_match = re.search(r"pixelHeight:\s*(\d+)", out)
    if not width_match or not height_match:
        raise RuntimeError(f"could not read image dimensions: {path}")
    return int(width_match.group(1)), int(height_match.group(1))


def derive_output_path(path, mode):
    """Build a stable temporary JPEG path for a derived image mode."""
    stat = path.stat()
    key = f"{path}:{stat.st_size}:{stat.st_mtime_ns}:{mode}"
    digest = hashlib.sha256(key.encode("utf-8")).hexdigest()[:16]
    out_dir = Path(tempfile.gettempdir()) / "ai-photos-derived"
    out_dir.mkdir(parents=True, exist_ok=True)
    return out_dir / f"{digest}.{mode}.jpg"


def prepare_image(path, mode):
    """Return either the original path or a compressed JPEG derivative for the requested mode."""
    spec = MODES[mode]
    sips_bin = require_sips()
    source = Path(path).expanduser().resolve()
    if not source.is_file():
        raise ValueError(f"image file not found: {source}")

    width, height = read_dimensions(sips_bin, source)
    longest_edge = max(width, height)
    if longest_edge <= spec["passthrough_edge"]:
        return {
            "ok": True,
            "used_original": True,
            "input_path": str(source),
            "output_path": str(source),
            "mode": mode,
            "width": width,
            "height": height,
            "max_edge": spec["max_edge"],
            "passthrough_edge": spec["passthrough_edge"],
            "quality": spec["quality"],
        }

    output = derive_output_path(source, mode)
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
    out_width, out_height = read_dimensions(sips_bin, output)
    return {
        "ok": True,
        "used_original": False,
        "input_path": str(source),
        "output_path": str(output),
        "mode": mode,
        "width": out_width,
        "height": out_height,
        "max_edge": spec["max_edge"],
        "passthrough_edge": spec["passthrough_edge"],
        "quality": spec["quality"],
    }


def main():
    """Prepare an image for captioning or preview delivery using macOS sips."""
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

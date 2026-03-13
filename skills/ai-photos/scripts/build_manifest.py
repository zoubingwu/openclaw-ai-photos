#!/usr/bin/env python3
import argparse
import hashlib
import json
import mimetypes
import os
import sys
from datetime import datetime, timezone
from pathlib import Path

try:
    from PIL import Image, ExifTags
except Exception:
    Image = None
    ExifTags = None

sys.path.insert(0, str(Path(__file__).resolve().parent))
from album_profile import normalize_sources  # noqa: E402

EXTS = {".jpg", ".jpeg", ".png", ".webp", ".heic"}


def sha256_file(path):
    """Compute the content hash used to detect new or changed files."""
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def extract_exif(path):
    """Read basic image dimensions and EXIF metadata when Pillow is available."""
    info = {"width": None, "height": None, "taken_at": None, "exif": {}}
    if Image is None:
        return info
    try:
        with Image.open(path) as img:
            info["width"], info["height"] = img.size
            raw = img.getexif() or {}
            mapped = {}
            for k, v in raw.items():
                name = ExifTags.TAGS.get(k, str(k)) if ExifTags else str(k)
                if isinstance(v, bytes):
                    continue
                mapped[name] = v
            info["exif"] = mapped
            dt = mapped.get("DateTimeOriginal") or mapped.get("DateTime")
            if isinstance(dt, str):
                try:
                    parsed = datetime.strptime(dt, "%Y:%m:%d %H:%M:%S").replace(tzinfo=timezone.utc)
                    info["taken_at"] = parsed.isoformat()
                except Exception:
                    pass
    except Exception:
        pass
    return info


def iter_files(root):
    """Yield files under a source root in a stable recursive order."""
    for dirpath, _, filenames in os.walk(root):
        for name in sorted(filenames):
            yield os.path.join(dirpath, name)


def main():
    """Scan source folders and emit a manifest JSONL for downstream indexing."""
    ap = argparse.ArgumentParser(description="Build photo manifest from one or more source folders")
    ap.add_argument("sources", nargs="+", help="one or more photo source folders")
    ap.add_argument("-o", "--output", required=True, help="output JSONL path")
    args = ap.parse_args()

    sources = normalize_sources(args.sources)
    records = 0
    with open(args.output, "w", encoding="utf-8") as out:
        for source in sources:
            for path in iter_files(source):
                if not os.path.isfile(path):
                    continue
                if os.path.basename(path).startswith("."):
                    continue
                ext = os.path.splitext(path)[1].lower()
                if ext not in EXTS:
                    continue
                stat = os.stat(path)
                exif = extract_exif(path)
                rec = {
                    "file_path": path,
                    "filename": os.path.basename(path),
                    "sha256": sha256_file(path),
                    "mime_type": mimetypes.guess_type(path)[0],
                    "size_bytes": stat.st_size,
                    "width": exif["width"],
                    "height": exif["height"],
                    "taken_at": exif["taken_at"],
                    "exif": exif["exif"],
                }
                out.write(json.dumps(rec, ensure_ascii=False) + "\n")
                records += 1
    print(json.dumps({"ok": True, "sources": sources, "output": args.output, "count": records}, ensure_ascii=False))

if __name__ == "__main__":
    main()

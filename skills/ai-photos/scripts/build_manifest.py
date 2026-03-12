#!/usr/bin/env python3
import argparse
import hashlib
import json
import mimetypes
import os
from datetime import datetime, timezone

try:
    from PIL import Image, ExifTags
except Exception:
    Image = None
    ExifTags = None

EXTS = {".jpg", ".jpeg", ".png", ".webp", ".heic"}


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def extract_exif(path):
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


def iter_files(root, recursive):
    if recursive:
        for dirpath, _, filenames in os.walk(root):
            for name in sorted(filenames):
                yield os.path.join(dirpath, name)
    else:
        for name in sorted(os.listdir(root)):
            yield os.path.join(root, name)


def main():
    ap = argparse.ArgumentParser(description="Build photo manifest from a folder")
    ap.add_argument("source", help="photo folder")
    ap.add_argument("-o", "--output", required=True, help="output JSONL path")
    ap.add_argument("--no-recursive", action="store_true")
    args = ap.parse_args()

    source = os.path.abspath(os.path.expanduser(args.source))
    records = 0
    with open(args.output, "w", encoding="utf-8") as out:
        for path in iter_files(source, recursive=not args.no_recursive):
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
    print(json.dumps({"ok": True, "source": source, "output": args.output, "count": records}, ensure_ascii=False))

if __name__ == "__main__":
    main()

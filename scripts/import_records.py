#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from tidb_http_sql import run_query, load_target  # noqa: E402


def sql_literal(value):
    if value is None:
        return 'NULL'
    if isinstance(value, bool):
        return 'true' if value else 'false'
    if isinstance(value, (int, float)):
        return str(value)
    if isinstance(value, (list, dict)):
        value = json.dumps(value, ensure_ascii=False)
    value = str(value).replace("'", "''")
    return f"'{value}'"


def build_search_text(rec):
    parts = []
    for key in ("caption", "scene", "text_in_image"):
        if rec.get(key):
            parts.append(str(rec[key]))
    for key in ("tags", "objects"):
        if isinstance(rec.get(key), list):
            parts.extend(str(x) for x in rec[key])
    return " ".join(parts).strip()


def run_db9(target, sql):
    cmd = ["db9", "db", "sql", target, "-q", sql]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
        sys.exit(proc.returncode)


def run_tidb(target, sql):
    t = load_target(target)
    run_query(t["host"], t["username"], t["password"], t["database"], sql)


def main():
    ap = argparse.ArgumentParser(description="Import caption records into db9 or TiDB")
    ap.add_argument("target", help="db9 database name/id or path to TiDB HTTP target JSON")
    ap.add_argument("jsonl", help="JSONL records with manifest + caption fields")
    ap.add_argument("--backend", choices=["db9", "tidb"], default="db9")
    args = ap.parse_args()
    runner = run_db9 if args.backend == "db9" else run_tidb

    count = 0
    with open(args.jsonl, encoding="utf-8") as f:
        for line in f:
            if not line.strip():
                continue
            rec = json.loads(line)
            rec.setdefault("tags", [])
            rec.setdefault("objects", [])
            rec.setdefault("metadata", {})
            rec.setdefault("exif", {})
            rec["search_text"] = rec.get("search_text") or build_search_text(rec)
            if args.backend == 'db9':
                sql = f"""
INSERT INTO photos (
  file_path, filename, sha256, mime_type, size_bytes, width, height, taken_at,
  exif, caption, tags, scene, objects, text_in_image, search_text, metadata, indexed_at, updated_at
) VALUES (
  {sql_literal(rec.get('file_path'))},
  {sql_literal(rec.get('filename'))},
  {sql_literal(rec.get('sha256'))},
  {sql_literal(rec.get('mime_type'))},
  {sql_literal(rec.get('size_bytes'))},
  {sql_literal(rec.get('width'))},
  {sql_literal(rec.get('height'))},
  {sql_literal(rec.get('taken_at'))},
  {sql_literal(rec.get('exif'))}::jsonb,
  {sql_literal(rec.get('caption'))},
  {sql_literal(rec.get('tags'))}::jsonb,
  {sql_literal(rec.get('scene'))},
  {sql_literal(rec.get('objects'))}::jsonb,
  {sql_literal(rec.get('text_in_image'))},
  {sql_literal(rec.get('search_text'))},
  {sql_literal(rec.get('metadata'))}::jsonb,
  now(),
  now()
)
ON CONFLICT (file_path) DO UPDATE SET
  sha256 = EXCLUDED.sha256,
  mime_type = EXCLUDED.mime_type,
  size_bytes = EXCLUDED.size_bytes,
  width = EXCLUDED.width,
  height = EXCLUDED.height,
  taken_at = EXCLUDED.taken_at,
  exif = EXCLUDED.exif,
  caption = EXCLUDED.caption,
  tags = EXCLUDED.tags,
  scene = EXCLUDED.scene,
  objects = EXCLUDED.objects,
  text_in_image = EXCLUDED.text_in_image,
  search_text = EXCLUDED.search_text,
  metadata = EXCLUDED.metadata,
  indexed_at = now(),
  updated_at = now();
"""
            else:
                sql = f"""
INSERT INTO photos (
  file_path, filename, sha256, mime_type, size_bytes, width, height, taken_at,
  exif, caption, tags, scene, objects, text_in_image, search_text, metadata, indexed_at, updated_at
) VALUES (
  {sql_literal(rec.get('file_path'))},
  {sql_literal(rec.get('filename'))},
  {sql_literal(rec.get('sha256'))},
  {sql_literal(rec.get('mime_type'))},
  {sql_literal(rec.get('size_bytes'))},
  {sql_literal(rec.get('width'))},
  {sql_literal(rec.get('height'))},
  {sql_literal(rec.get('taken_at'))},
  {sql_literal(rec.get('exif'))},
  {sql_literal(rec.get('caption'))},
  {sql_literal(rec.get('tags'))},
  {sql_literal(rec.get('scene'))},
  {sql_literal(rec.get('objects'))},
  {sql_literal(rec.get('text_in_image'))},
  {sql_literal(rec.get('search_text'))},
  {sql_literal(rec.get('metadata'))},
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
)
ON DUPLICATE KEY UPDATE
  sha256 = VALUES(sha256), mime_type = VALUES(mime_type), size_bytes = VALUES(size_bytes),
  width = VALUES(width), height = VALUES(height), taken_at = VALUES(taken_at), exif = VALUES(exif),
  caption = VALUES(caption), tags = VALUES(tags), scene = VALUES(scene), objects = VALUES(objects),
  text_in_image = VALUES(text_in_image), search_text = VALUES(search_text), metadata = VALUES(metadata),
  indexed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP;
"""
            runner(args.target, sql)
            count += 1
    print(json.dumps({"ok": True, "backend": args.backend, "target": args.target, "imported": count}, ensure_ascii=False))

if __name__ == "__main__":
    main()

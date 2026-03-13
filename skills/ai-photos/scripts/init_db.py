#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from album_profile import resolve_backend_target  # noqa: E402
from tidb_http_sql import run_query, load_target  # noqa: E402

DB9_SCHEMA_SQL = r'''
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS photos (
  id BIGSERIAL PRIMARY KEY,
  file_path TEXT NOT NULL UNIQUE,
  filename TEXT NOT NULL,
  sha256 VARCHAR(64) NOT NULL,
  mime_type TEXT,
  size_bytes BIGINT,
  width INT,
  height INT,
  taken_at TIMESTAMPTZ NULL,
  exif JSONB NOT NULL DEFAULT '{}'::jsonb,
  caption TEXT,
  tags JSONB NOT NULL DEFAULT '[]'::jsonb,
  scene TEXT,
  objects JSONB NOT NULL DEFAULT '[]'::jsonb,
  text_in_image TEXT,
  search_text TEXT NOT NULL DEFAULT '',
  embedding VECTOR(1536),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  indexed_at TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_photos_taken_at ON photos (taken_at);
CREATE INDEX IF NOT EXISTS idx_photos_sha256 ON photos (sha256);
CREATE INDEX IF NOT EXISTS idx_photos_tags ON photos USING GIN (tags);
CREATE INDEX IF NOT EXISTS idx_photos_objects ON photos USING GIN (objects);
CREATE INDEX IF NOT EXISTS idx_photos_fts ON photos USING GIN (to_tsvector('english', search_text));
'''

TIDB_SCHEMA_SQL = r'''
CREATE TABLE IF NOT EXISTS photos (
  id BIGINT PRIMARY KEY AUTO_RANDOM,
  file_path TEXT NOT NULL,
  filename TEXT NOT NULL,
  sha256 VARCHAR(64) NOT NULL,
  mime_type TEXT,
  size_bytes BIGINT,
  width INT,
  height INT,
  taken_at TIMESTAMP NULL,
  exif JSON NOT NULL,
  caption TEXT,
  tags JSON NOT NULL,
  scene TEXT,
  objects JSON NOT NULL,
  text_in_image TEXT,
  search_text TEXT NOT NULL,
  embedding VECTOR(1536),
  metadata JSON NOT NULL,
  indexed_at TIMESTAMP NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_file_path (file_path(255)),
  KEY idx_taken_at (taken_at),
  KEY idx_sha256 (sha256),
  FULLTEXT KEY ftx_search_text (search_text)
);
'''

def run_db9(target, sql):
    cmd = ["db9", "--json", "db", "sql", target, "-q", sql]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
        sys.exit(proc.returncode)
    return proc.stdout


def run_tidb(target, sql):
    t = load_target(target)
    return run_query(t["host"], t["username"], t["password"], t["database"], sql)


def main():
    ap = argparse.ArgumentParser(description="Initialize photo album schema for db9 or TiDB")
    ap.add_argument("target", nargs="?", help="db9 database name/id or path to TiDB HTTP target JSON")
    ap.add_argument("--backend", choices=["db9", "tidb"])
    ap.add_argument("--profile", help="profile name or path to profile JSON")
    args = ap.parse_args()
    try:
        backend, target, _ = resolve_backend_target(target=args.target, backend=args.backend, profile_ref=args.profile)
    except ValueError as e:
        sys.stderr.write(f"{e}\n")
        sys.exit(2)
    sql = DB9_SCHEMA_SQL if backend == "db9" else TIDB_SCHEMA_SQL
    out = run_db9(target, sql) if backend == "db9" else run_tidb(target, sql)
    try:
        parsed = json.loads(out)
        print(json.dumps({"ok": True, "backend": backend, "target": target, "result": parsed}, ensure_ascii=False, indent=2))
    except Exception:
        print(out)

if __name__ == "__main__":
    main()

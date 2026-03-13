#!/usr/bin/env python3
import argparse
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from album_profile import resolve_backend_target  # noqa: E402
from tidb_http_sql import run_query, load_target  # noqa: E402


def run_db9(target, sql):
    """Execute a search query against a db9 target and print the raw rows."""
    cmd = ["db9", "db", "sql", target, "-q", sql]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
        sys.exit(proc.returncode)
    print(proc.stdout)


def run_tidb(target, sql):
    """Execute a search query against a TiDB target and print the raw response."""
    t = load_target(target)
    print(run_query(t["host"], t["username"], t["password"], t["database"], sql))


def esc(s):
    """Escape single quotes for the simple SQL fragments used by this script."""
    return s.replace("'", "''")


def main():
    """Run a date, text, tag, or recent search against the indexed photo backend."""
    ap = argparse.ArgumentParser(description="Search photos in db9 or TiDB by date, text, tag, or recent import order")
    ap.add_argument("target", nargs="?", help="db9 database name/id or path to TiDB HTTP target JSON")
    ap.add_argument("--backend", choices=["db9", "tidb"])
    ap.add_argument("--profile", help="profile name or path to profile JSON")
    ap.add_argument("--date")
    ap.add_argument("--text")
    ap.add_argument("--tag")
    ap.add_argument("--recent", action="store_true")
    ap.add_argument("--limit", type=int, default=20)
    args = ap.parse_args()
    try:
        backend, target, _ = resolve_backend_target(target=args.target, backend=args.backend, profile_ref=args.profile)
    except ValueError as e:
        sys.stderr.write(f"{e}\n")
        sys.exit(2)
    runner = run_db9 if backend == "db9" else run_tidb

    if args.recent and any([args.date, args.text, args.tag]):
        sys.stderr.write("--recent cannot be combined with --date, --text, or --tag\n")
        sys.exit(2)
    if args.recent:
        runner(target, f"SELECT file_path, filename, taken_at, indexed_at, caption, tags FROM photos ORDER BY indexed_at DESC, created_at DESC LIMIT {args.limit};")
        return
    where = []
    if args.date:
        if backend == 'db9':
            where.append(f"taken_at::text LIKE '{esc(args.date)}%'")
        else:
            where.append(f"CAST(taken_at AS CHAR) LIKE '{esc(args.date)}%'")
    if args.text:
        if backend == 'db9':
            where.append(f"to_tsvector('english', search_text) @@ websearch_to_tsquery('english', '{esc(args.text)}')")
        else:
            where.append(f"search_text LIKE '%{esc(args.text)}%'")
    if args.tag:
        if backend == 'db9':
            where.append(f"tags @> jsonb_build_array('{esc(args.tag)}')")
        else:
            where.append(f"JSON_CONTAINS(tags, JSON_ARRAY('{esc(args.tag)}'))")
    clause = ' AND '.join(where) if where else ('true' if backend == 'db9' else '1=1')
    runner(target, f"SELECT file_path, filename, taken_at, caption, tags FROM photos WHERE {clause} ORDER BY COALESCE(taken_at, created_at) DESC LIMIT {args.limit};")

if __name__ == '__main__':
    main()

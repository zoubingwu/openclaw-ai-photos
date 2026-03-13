#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from album_profile import resolve_backend_target, resolve_sources  # noqa: E402
from tidb_http_sql import run_query, load_target  # noqa: E402


def run(cmd):
    """Execute a subprocess command and return stdout or raise a readable error."""
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True)
    except FileNotFoundError:
        raise RuntimeError(f"command not found: {cmd[0]}")
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.strip() or f"command failed: {' '.join(cmd)}")
    return proc.stdout


def fetch_existing_db9(target):
    """Read existing file hashes from a db9 backend for incremental comparison."""
    out = run(["db9", "db", "sql", target, "-q", "SELECT file_path, sha256 FROM photos;"])
    rows = {}
    for line in out.splitlines():
        line = line.strip()
        if not line or line.startswith("file_path") or line.startswith("("):
            continue
        parts = line.split("\t")
        if len(parts) >= 2:
            rows[parts[0]] = parts[1]
    return rows


def fetch_existing_tidb(target):
    """Read existing file hashes from a TiDB backend for incremental comparison."""
    t = load_target(target)
    out = run_query(t["host"], t["username"], t["password"], t["database"], "SELECT file_path, sha256 FROM photos;")
    data = json.loads(out)
    rows = {}
    for row in data.get("rows", []):
        vals = row.get("values") or []
        if len(vals) >= 2:
            rows[str(vals[0])] = str(vals[1])
    return rows


def load_manifest(path):
    """Load a JSONL manifest file into memory."""
    with open(path, encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]


def save_manifest(path, records):
    """Write manifest records back to JSONL format."""
    with open(path, 'w', encoding='utf-8') as out:
        for rec in records:
            out.write(json.dumps(rec, ensure_ascii=False) + '\n')


def main():
    """Build the next incremental sync manifest for the selected album sources."""
    ap = argparse.ArgumentParser(description="Run an incremental sync flow for the AI photo album")
    ap.add_argument("target", nargs="?", help="db9 database name/id or path to TiDB HTTP target JSON")
    ap.add_argument("sources", nargs="*", help="one or more photo source folders")
    ap.add_argument("--backend", choices=["db9", "tidb"])
    ap.add_argument("--profile", help="profile name or path to profile JSON")
    ap.add_argument("--manifest-out")
    args = ap.parse_args()
    try:
        backend, target, profile = resolve_backend_target(target=args.target, backend=args.backend, profile_ref=args.profile)
        sources = resolve_sources(sources=args.sources, profile=profile)
    except ValueError as e:
        sys.stderr.write(f"{e}\n")
        sys.exit(2)

    manifest_path = args.manifest_out or str(Path(tempfile.gettempdir()) / 'ai-photos.manifest.jsonl')
    incremental_path = str(Path(manifest_path).with_suffix('.incremental.jsonl'))
    run([sys.executable, str(Path(__file__).with_name('build_manifest.py')), *sources, '-o', manifest_path])
    all_records = load_manifest(manifest_path)
    try:
        existing = fetch_existing_db9(target) if backend == 'db9' else fetch_existing_tidb(target)
        backend_status = 'ok'
    except Exception as e:
        existing = {}
        backend_status = f'fallback-full-scan: {e}'
    incremental = []
    unchanged = 0
    for rec in all_records:
        if existing.get(rec.get('file_path')) == rec.get('sha256'):
            unchanged += 1
        else:
            incremental.append(rec)
    save_manifest(incremental_path, incremental)
    print(json.dumps({
        'ok': True,
        'backend': backend,
        'target': target,
        'sources': sources,
        'manifest_jsonl': manifest_path,
        'incremental_manifest_jsonl': incremental_path,
        'total_scanned': len(all_records),
        'unchanged': unchanged,
        'to_caption': len(incremental),
        'backend_status': backend_status,
        'next_step': 'Use a vision-capable OpenClaw model to turn incremental_manifest_jsonl into captioned_jsonl following references/caption-schema.md, then run import_records.py.'
    }, ensure_ascii=False, indent=2))

if __name__ == '__main__':
    main()

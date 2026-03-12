#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from tidb_http_sql import run_query, load_target  # noqa: E402


def run(cmd):
    try:
        proc = subprocess.run(cmd, capture_output=True, text=True)
    except FileNotFoundError:
        raise RuntimeError(f"command not found: {cmd[0]}")
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.strip() or f"command failed: {' '.join(cmd)}")
    return proc.stdout


def fetch_existing_db9(target):
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
    with open(path, encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]


def save_manifest(path, records):
    with open(path, 'w', encoding='utf-8') as out:
        for rec in records:
            out.write(json.dumps(rec, ensure_ascii=False) + '\n')


def main():
    ap = argparse.ArgumentParser(description="Run an incremental sync flow for the AI photo album")
    ap.add_argument("target", help="db9 database name/id or path to TiDB HTTP target JSON")
    ap.add_argument("source", help="photo folder")
    ap.add_argument("--backend", choices=["db9", "tidb"], default="db9")
    ap.add_argument("--manifest-out")
    args = ap.parse_args()

    manifest_path = args.manifest_out or str(Path(tempfile.gettempdir()) / 'ai-photos.manifest.jsonl')
    incremental_path = str(Path(manifest_path).with_suffix('.incremental.jsonl'))
    run([sys.executable, str(Path(__file__).with_name('build_manifest.py')), args.source, '-o', manifest_path])
    all_records = load_manifest(manifest_path)
    try:
        existing = fetch_existing_db9(args.target) if args.backend == 'db9' else fetch_existing_tidb(args.target)
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
        'backend': args.backend,
        'target': args.target,
        'source': str(Path(args.source).expanduser().resolve()),
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

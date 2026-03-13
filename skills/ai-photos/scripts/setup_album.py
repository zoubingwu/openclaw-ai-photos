#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
from pathlib import Path

from album_profile import save_profile


def run_script(script_name, args):
    cmd = [sys.executable, str(Path(__file__).with_name(script_name)), *args]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        message = proc.stderr.strip() or proc.stdout.strip() or f"command failed: {' '.join(cmd)}"
        raise RuntimeError(message)
    text = proc.stdout.strip()
    if not text:
        return None
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return {"raw": text}


def main():
    ap = argparse.ArgumentParser(
        description="Save an album profile, initialize the backend, and build the first incremental manifest"
    )
    ap.add_argument("profile", help="profile name or path to profile JSON")
    ap.add_argument("--source", action="append", required=True, help="photo source folder; repeat to add multiple roots")
    ap.add_argument("--backend", choices=["db9", "tidb"], required=True)
    ap.add_argument("--target", required=True, help="db9 database name/id or path to TiDB target JSON")
    ap.add_argument("--display-name")
    ap.add_argument("--maintenance-mode", default="heartbeat")
    ap.add_argument("--manifest-out")
    args = ap.parse_args()

    try:
        profile_path, profile = save_profile(
            profile_ref=args.profile,
            sources=args.source,
            backend=args.backend,
            target=args.target,
            display_name=args.display_name,
            maintenance_mode=args.maintenance_mode,
        )
    except ValueError as e:
        sys.stderr.write(f"{e}\n")
        sys.exit(2)

    try:
        init_result = run_script("init_db.py", ["--profile", str(profile_path)])
        sync_args = ["--profile", str(profile_path)]
        if args.manifest_out:
            sync_args.extend(["--manifest-out", args.manifest_out])
        sync_result = run_script("sync_photos.py", sync_args)
    except RuntimeError as e:
        sys.stderr.write(f"{e}\n")
        sys.stderr.write(f"profile saved at {profile_path}\n")
        sys.exit(1)

    output = {
        "ok": True,
        "profile_path": str(profile_path),
        "profile": profile,
        "init": init_result,
        "sync": sync_result,
    }
    if isinstance(sync_result, dict):
        output["caption_input_jsonl"] = sync_result.get("incremental_manifest_jsonl")
        output["next_step"] = sync_result.get("next_step")
    print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

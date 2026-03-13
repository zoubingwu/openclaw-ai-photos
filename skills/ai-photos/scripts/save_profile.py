#!/usr/bin/env python3
import argparse
import json

from album_profile import save_profile


def main():
    """Create or update a managed album profile from CLI arguments."""
    ap = argparse.ArgumentParser(description="Create or update an ai-photos album profile")
    ap.add_argument("profile", help="profile name or path to profile JSON")
    ap.add_argument("--source", action="append", required=True, help="photo source path; repeat to add multiple roots")
    ap.add_argument("--backend", choices=["db9", "tidb"], required=True)
    ap.add_argument("--target", required=True, help="db9 database name/id or path to TiDB target JSON")
    ap.add_argument("--display-name")
    ap.add_argument("--maintenance-mode", default="heartbeat")
    args = ap.parse_args()

    path, profile = save_profile(
        profile_ref=args.profile,
        sources=args.source,
        backend=args.backend,
        target=args.target,
        display_name=args.display_name,
        maintenance_mode=args.maintenance_mode,
    )
    print(json.dumps({"ok": True, "profile_path": str(path), "profile": profile}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

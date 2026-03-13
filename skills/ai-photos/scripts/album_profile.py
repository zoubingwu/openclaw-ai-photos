#!/usr/bin/env python3
import json
import os
import re
from datetime import datetime, timezone
from pathlib import Path

from tidb_http_sql import load_target

BASE_DIR = Path.home() / ".openclaw" / "ai-photos"
ALBUMS_DIR = BASE_DIR / "albums"
TARGETS_DIR = BASE_DIR / "targets"
PROFILE_VERSION = 1


def ensure_storage_dirs():
    """Create the default directories used for album profiles and saved targets."""
    ALBUMS_DIR.mkdir(parents=True, exist_ok=True)
    TARGETS_DIR.mkdir(parents=True, exist_ok=True)


def slugify_album_id(value):
    """Convert a profile or album name into a stable filesystem-safe id."""
    slug = re.sub(r"[^a-zA-Z0-9._-]+", "-", value.strip()).strip("-").lower()
    return slug or "album"


def is_path_like(value):
    """Detect whether a profile reference already looks like a filesystem path."""
    return any(ch in value for ch in ("/", "\\")) or value.startswith("~") or value.endswith(".json")


def resolve_profile_path(ref):
    """Resolve a profile name or path into the concrete profile JSON path."""
    ensure_storage_dirs()
    if is_path_like(ref):
        path = Path(ref).expanduser()
        if path.suffix != ".json":
            path = path.with_suffix(".json")
        return path.resolve()
    return (ALBUMS_DIR / f"{slugify_album_id(ref)}.json").resolve()


def _require(mapping, key, path):
    """Read a required key from a mapping and raise a clear profile error if missing."""
    if key not in mapping:
        raise ValueError(f"missing '{key}' in profile: {path}")
    return mapping[key]


def normalize_sources(sources):
    """Normalize photo source paths into unique absolute roots without nested duplicates."""
    normalized = []
    for source in sources:
        path = str(Path(source).expanduser().resolve())
        if path in normalized:
            continue
        skip = False
        kept = []
        for existing in normalized:
            try:
                Path(path).relative_to(existing)
                skip = True
                kept.append(existing)
                continue
            except ValueError:
                pass
            try:
                Path(existing).relative_to(path)
                continue
            except ValueError:
                kept.append(existing)
        if skip:
            normalized = kept
            continue
        kept.append(path)
        normalized = kept
    if not normalized:
        raise ValueError("at least one source is required")
    return normalized


def load_profile(ref):
    """Load a saved album profile and validate its required fields."""
    path = resolve_profile_path(ref)
    with open(path, encoding="utf-8") as f:
        profile = json.load(f)
    _require(profile, "version", path)
    _require(profile, "album_id", path)
    sources = _require(profile, "sources", path)
    backend = _require(profile, "backend", path)
    if not isinstance(sources, list) or not sources:
        raise ValueError(f"missing or empty 'sources' in profile: {path}")
    kind = _require(backend, "kind", path)
    if kind == "db9":
        _require(backend, "target", path)
    elif kind == "tidb":
        target_file = Path(_require(backend, "target_file", path)).expanduser().resolve()
        if not target_file.exists():
            raise ValueError(f"missing TiDB target file for profile: {target_file}")
        backend["target_file"] = str(target_file)
    else:
        raise ValueError(f"unsupported backend in profile: {kind}")
    profile["_path"] = str(path)
    profile["sources"] = normalize_sources(sources)
    return profile


def backend_target_from_profile(profile):
    """Extract the backend kind and concrete target from a loaded profile."""
    backend = profile["backend"]["kind"]
    if backend == "db9":
        return backend, profile["backend"]["target"]
    return backend, profile["backend"]["target_file"]


def sources_from_profile(profile):
    """Return the normalized source roots stored in a loaded profile."""
    return normalize_sources(profile["sources"])


def _target_matches(backend, given, resolved):
    """Compare a provided target with the target resolved from a profile."""
    if backend == "tidb":
        return str(Path(given).expanduser().resolve()) == str(Path(resolved).expanduser().resolve())
    return str(given) == str(resolved)


def resolve_backend_target(target=None, backend=None, profile_ref=None):
    """Resolve the backend kind and target from raw args or a saved profile."""
    profile = load_profile(profile_ref) if profile_ref else None
    if profile is None:
        if target is None:
            raise ValueError("target or --profile is required")
        return backend or "db9", target, None

    profile_backend, profile_target = backend_target_from_profile(profile)
    if backend and backend != profile_backend:
        raise ValueError(f"--backend {backend} does not match profile backend {profile_backend}")
    if target and not _target_matches(profile_backend, target, profile_target):
        raise ValueError("target does not match the selected profile")
    return profile_backend, profile_target, profile


def resolve_sources(sources=None, profile=None):
    """Resolve source roots from raw args or validate them against a saved profile."""
    sources = sources or []
    if profile is None:
        return normalize_sources(sources)

    profile_sources = sources_from_profile(profile)
    if not sources:
        return profile_sources
    resolved = normalize_sources(sources)
    if resolved != profile_sources:
        raise ValueError("sources do not match the selected profile")
    return resolved


def save_profile(profile_ref, sources, backend, target, display_name=None, maintenance_mode="heartbeat"):
    """Persist an album profile and, for TiDB, copy its target JSON into managed storage."""
    ensure_storage_dirs()
    profile_path = resolve_profile_path(profile_ref)
    album_id = slugify_album_id(profile_path.stem if is_path_like(profile_ref) else profile_ref)
    now = datetime.now(timezone.utc).replace(microsecond=0).isoformat()

    existing_created_at = now
    existing_display_name = None
    if profile_path.exists():
        try:
            with open(profile_path, encoding="utf-8") as f:
                existing = json.load(f)
                existing_created_at = existing.get("created_at") or now
                existing_display_name = existing.get("display_name")
        except Exception:
            pass

    source_paths = normalize_sources(sources)
    backend_config = {"kind": backend}
    if backend == "db9":
        backend_config["target"] = target
    else:
        target_data = load_target(str(Path(target).expanduser().resolve()))
        for key in ("host", "username", "password", "database"):
            if key not in target_data:
                raise ValueError(f"missing '{key}' in TiDB target JSON")
        target_path = (TARGETS_DIR / f"{album_id}.tidb.json").resolve()
        with open(target_path, "w", encoding="utf-8") as out:
            json.dump(target_data, out, ensure_ascii=False, indent=2)
            out.write("\n")
        os.chmod(target_path, 0o600)
        backend_config["target_file"] = str(target_path)

    profile = {
        "version": PROFILE_VERSION,
        "album_id": album_id,
        "display_name": display_name or existing_display_name or album_id,
        "sources": source_paths,
        "backend": backend_config,
        "maintenance": {
            "mode": maintenance_mode,
        },
        "created_at": existing_created_at,
        "updated_at": now,
    }

    profile_path.parent.mkdir(parents=True, exist_ok=True)
    with open(profile_path, "w", encoding="utf-8") as out:
        json.dump(profile, out, ensure_ascii=False, indent=2)
        out.write("\n")
    os.chmod(profile_path, 0o600)
    return profile_path, profile

# Repository Guide

## Architecture

- The repository root is the single Go module for the `ai-photos` program.
- `cmd/ai-photos` is the only binary entrypoint.
- `internal/app` holds shared profile, backend, sync, import, image-prep, and HTTP server logic.
- `frontend` contains the embedded browser UI served by `ai-photos serve`.
- `skills/ai-photos` is skill content only. Do not place Go source code there.
- `skills/ai-photos/SKILL.md` is the single source of truth for skill-side instructions and schema details.

## Rules

- Do not reintroduce Python runtime dependencies for core album commands.
- Keep CLI and web behavior on the same shared Go codepaths.
- Prefer extending `internal/app` over creating a second backend or web module.
- External tools such as `db9`, `sips`, or ImageMagick are acceptable when already part of the runtime contract.

## Release CI

The repository includes a GitHub Actions workflow at `.github/workflows/release-ai-photos.yml` that publishes cross-platform binaries for `ai-photos`.

Release flow:

- Push a tag like `v0.1.0` to build and publish a GitHub Release
- Or run the workflow manually with a `version` input like `v0.1.0`

Published assets use these names:

- `ai-photos_<version>_linux_amd64.tar.gz`
- `ai-photos_<version>_linux_arm64.tar.gz`
- `ai-photos_<version>_darwin_amd64.tar.gz`
- `ai-photos_<version>_darwin_arm64.tar.gz`
- `ai-photos_<version>_windows_amd64.zip`

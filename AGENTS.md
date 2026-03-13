# Repository Guide

## Architecture

- The repository root is the single Go module for the `ai-photos` program.
- `cmd/ai-photos` is the only binary entrypoint.
- `internal/app` holds shared profile, backend, sync, import, image-prep, and HTTP server logic.
- `frontend` contains the embedded browser UI served by `ai-photos serve`.
- `skills/ai-photos` is skill content only. Do not place Go source code there.
- `skills/ai-photos/references` keeps skill-side reference material.

## Rules

- Do not reintroduce Python runtime dependencies for core album commands.
- Keep CLI and web behavior on the same shared Go codepaths.
- Prefer extending `internal/app` over creating a second backend or web module.
- External tools such as `db9`, `sips`, or ImageMagick are acceptable when already part of the runtime contract.

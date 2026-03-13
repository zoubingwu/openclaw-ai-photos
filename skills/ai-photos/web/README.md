# AI Photos Web

Local HTTP service for browsing an indexed `ai-photos` album in the browser.

## Run

From this directory:

```bash
go run ./cmd/ai-photos-web
```

Optional flags:

```bash
go run ./cmd/ai-photos-web --profile default --host 127.0.0.1 --port 0
```

Startup prints one JSON line with the local URL, backend, and resolved profile path.

## Config

The service prefers the saved album profile and uses environment variables only to fill missing backend fields.

Supported environment variables:

- `AI_PHOTOS_BACKEND`
- `AI_PHOTOS_DB9_TARGET`
- `AI_PHOTOS_TIDB_HOST`
- `AI_PHOTOS_TIDB_USERNAME`
- `AI_PHOTOS_TIDB_PASSWORD`
- `AI_PHOTOS_TIDB_DATABASE`

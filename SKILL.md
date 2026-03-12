---
name: ai-photos
description: Build and maintain a personal AI photo album from a local folder of images, using either db9 or TiDB Cloud Zero/Starter as the backend. Use when OpenClaw should guide the user through setup, initialize a photo database, recursively scan a photo folder, extract EXIF and hashes, generate captions with a vision-capable model, keep the index fresh during heartbeats, answer photo-search requests from the backend, and send matching images back to the user.
---

# ai-photos

Turn a normal image folder into a personal AI photo album backed by either db9 or TiDB.

This skill is for an end-to-end user experience, not just a technical pipeline. After installation, the agent should help the user:
- choose a local folder as the photo library
- choose or auto-select the backend
- initialize the database and first import
- enable automatic maintenance through heartbeats
- search the library later by text, tags, dates, or caption-derived semantics
- send matching images back to the user, not just text summaries

## Agent behavior

Treat this as a guided setup-and-use skill.

The agent should:
- lead the user through first-time setup with short, concrete questions
- prefer sensible defaults and keep the number of choices small
- explain what it is doing in plain language
- confirm when indexing is complete
- teach the user how to search after setup
- send actual image results when the user asks for matches and local file access permits it

The agent should not:
- dump raw SQL unless the user explicitly asks
- leave the album in a manual-only state without warning the user
- silently choose a non-working image model
- assume macOS Photos integration; use ordinary folders for now

## Happy-path conversation flow

When the user installs this skill and wants to start using it, follow this flow.

### 1. Ask for the photo library folder

First ask the user to choose a local folder that will act as the photo library.

Good prompt style:
- ask for one folder path
- explain that the skill will index that folder recursively
- make it clear that ordinary folders are supported and system photo apps are out of scope for now

Do not continue setup until the user has provided a folder.

### 2. Validate the vision model first

Before indexing anything, verify that image analysis actually works in the current OpenClaw runtime.

Requirements:
- `agents.defaults.imageModel` must point to a vision-capable model
- do not trust the model name alone; test with a real image if needed

If image analysis is not working:
- tell the user setup is blocked
- explain that the image model must be fixed first
- do not proceed with indexing fake or filename-derived captions

### 3. Choose the backend

Prefer this backend decision rule:
- if `db9` CLI is installed and usable, prefer `db9`
- otherwise fall back to `TiDB Cloud Zero`

When using TiDB Zero:
- remind the user that Zero is temporary
- explicitly tell the user they should claim it if they want to keep the database
- preserve enough local config to reconnect later

## Backend choices

### db9

Prefer db9 when available because it fits this skill especially well:
- database-per-user model
- OpenClaw-friendly CLI flow
- Postgres + JSONB + vector + full-text search
- simple operational model for a personal AI album

### TiDB Cloud Zero / Starter

Use TiDB when db9 is unavailable or when the user wants TiDB specifically.

Good fit:
- disposable demo backend via Zero
- longer-lived backend after claim to Starter
- HTTP SQL API path without requiring a local MySQL CLI

Important caveat for TiDB:
- TiDB auto embedding is mainly for text
- do not assume it directly generates embeddings from raw image files
- for image retrieval, use caption text or external image embeddings

## Credential handling

Be explicit about backend credentials.

### db9 credentials

There are two db9 credential layers:
- `db9` account auth for the CLI
- direct database connection details for external tools

For this skill's scripts, only the CLI auth is normally required.

Default behavior:
- `db9 login`, `db9 claim`, or `db9 login --api-key ...` stores auth in `~/.db9/credentials`
- scripts identify the target database by database name or id
- the skill does not need to copy raw Postgres passwords into extra files for routine use

### TiDB credentials

For TiDB Zero or Starter, prefer one of these:
- HTTP SQL API credentials
- MySQL-compatible connection details when already available

Preferred practical rule:
- if no local MySQL client is available, use the HTTP SQL API
- avoid copying passwords into shell history or plaintext config files outside the skill's working config

For TiDB Zero specifically, preserve enough local config to reconnect:
- database host
- username
- password
- database name
- claim URL
- expiration time

If the backend is TiDB Zero, remind the user that setup is not truly durable until they claim it.

## Initialization flow

Use this flow the first time the user sets up the album.

### 1. Create the backend database

#### db9 path

```bash
# db9 create --name my-album
python3 scripts/init_db.py <db> --backend db9
```

#### TiDB path

Initialize the chosen TiDB database:

```bash
python3 scripts/init_db.py /path/to/tidb-target.json --backend tidb
```

The target JSON should contain:

```json
{
  "host": "gateway03.us-west-2.prod.aws.tidbcloud.com",
  "username": "<user>",
  "password": "<password>",
  "database": "ai_photos"
}
```

The HTTP SQL API endpoint is derived automatically from `host` as:
- database host: `gateway03.us-west-2.prod.aws.tidbcloud.com`
- HTTP API: `https://http-gateway03.us-west-2.prod.aws.tidbcloud.com/v1beta/sql`

### 2. Build the initial manifest

```bash
python3 scripts/build_manifest.py <photo-folder> -o /tmp/photos.manifest.jsonl
```

The manifest includes:
- file path
- filename
- sha256
- mime type
- size
- width and height when available
- taken-at timestamp when available
- raw EXIF JSON

### 3. Generate initial captions with a vision-capable model

Read `references/caption-schema.md` before generating records.

For each manifest record:
- inspect the image with a vision-capable model
- preserve the original fields
- add:
  - `caption`
  - `tags`
  - `scene`
  - `objects`
  - `text_in_image`
- write one JSON object per line into a captioned JSONL file

Caption style requirements:
- short
- factual
- retrieval-oriented
- visually grounded

Avoid:
- naming unknown people
- inferring sensitive traits
- speculative storytelling
- filename-based fake descriptions

### 4. Import the initial records

#### db9 path

```bash
python3 scripts/import_records.py <db> /tmp/photos.captioned.jsonl --backend db9
```

#### TiDB path

```bash
python3 scripts/import_records.py /path/to/tidb-target.json /tmp/photos.captioned.jsonl --backend tidb
```

### 5. Tell the user setup is complete

After the first import succeeds, send a short completion message.

The agent should tell the user:
- the album is indexed
- future updates will be maintained automatically via heartbeat
- they can now search by examples such as:
  - keyword or natural language, for example `找弹吉他的` or `find cats on the sofa`
  - date, for example `2026-03`
  - tags, for example `cat`, `mountain`, `beach`

Do not end setup without telling the user that the album is now usable.

## Heartbeat maintenance flow

Heartbeat-driven maintenance is the default operating mode after initialization.

Do not consider setup complete until one of these is true:
- heartbeat is configured to maintain the album
- another periodic trigger is configured to run the same incremental flow

If neither is configured, tell the user setup is incomplete.

### Heartbeat behavior

When a heartbeat arrives for a configured album:

1. Run incremental sync:

```bash
python3 scripts/sync_photos.py <target> <photo-folder> --backend <db9|tidb>
```

2. Read the JSON output.
3. If `to_caption` is `0`, return `HEARTBEAT_OK`.
4. If `to_caption` is greater than `0`:
   - read `incremental_manifest_jsonl`
   - caption only those new or changed records with a vision-capable model
   - write a captioned JSONL file
   - import it with `import_records.py`
5. Stay quiet unless:
   - indexing failed
   - user attention is needed
   - the user explicitly asked for progress updates

### Incremental rule

Incrementality is determined by:
- `file_path`
- `sha256`

If both match an existing record, skip the image.
If either differs, treat it as new or changed and reprocess it.

## User search flow

When the user asks to find photos, search the indexed backend instead of reprocessing the library.

Use the fixed search script for common queries.

### db9 path

```bash
python3 scripts/search_photos.py <db> --backend db9 --text "cat on sofa"
python3 scripts/search_photos.py <db> --backend db9 --tag cat
python3 scripts/search_photos.py <db> --backend db9 --date 2026-03
python3 scripts/search_photos.py <db> --backend db9 --recent
```

### TiDB path

```bash
python3 scripts/search_photos.py /path/to/tidb-target.json --backend tidb --text "cat on sofa"
python3 scripts/search_photos.py /path/to/tidb-target.json --backend tidb --tag cat
python3 scripts/search_photos.py /path/to/tidb-target.json --backend tidb --date 2026-03
python3 scripts/search_photos.py /path/to/tidb-target.json --backend tidb --recent
```

### How the agent should answer search requests

When responding to the user:
- summarize the matching photos clearly
- mention filenames, dates, or captions when useful
- do not dump raw SQL unless explicitly requested
- if the user wants the photo itself, send the matching image file back, not just text

Preferred interaction style:
- if there is one clear match, send that image directly with a short explanation
- if there are multiple strong matches, summarize them first and send the top 1-3 images
- if results are weak or ambiguous, say so and suggest a better query

This skill should feel like an AI photo librarian, not a SQL wrapper.

## Scripts

### `scripts/init_db.py`
Initialize the album schema for either db9 or TiDB.

### `scripts/build_manifest.py`
Recursively scan a folder and emit a manifest JSONL with EXIF and hash data.

### `scripts/import_records.py`
Load captioned JSONL records into db9 or TiDB with upsert behavior.

### `scripts/search_photos.py`
Run the fixed search flow for the album:
- date search
- text search
- tag search
- recent imports

### `scripts/sync_photos.py`
Run the incremental sync entrypoint for heartbeat-driven indexing.

### `scripts/tidb_http_sql.py`
Run SQL through the TiDB HTTP SQL API when a local MySQL client is unavailable.

## Reference files

### `references/caption-schema.md`
Read this before generating the captioned JSONL. It defines the expected output shape for the vision step.

## Notes on search

This skill fixes the ingestion path first. It does not assume db9 has built-in image embedding generation, and it does not assume TiDB can directly embed raw image files.

For the first usable album, search works well enough via:
- caption text
- tags
- scene labels
- objects
- visible text in image

If the user later wants stronger semantic retrieval, extend the same workflow with a text or image embedding step after captions are generated.

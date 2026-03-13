---
name: ai-photos
description: |
  Personal AI photo album for OpenClaw.

  Use when users say:
  - "index my photos"
  - "set up an AI photo album"
  - "search my photo library"
  - "reconnect my photo album"
  - "find photos of ..."
metadata:
  version: 1.1.0
---

# ai-photos

ai-photos turns a normal local folder into a searchable AI photo album for OpenClaw.

This skill guides a normal OpenClaw user through folder selection, indexing, search, and ongoing sync.

When talking to end users:
- say "photo library folder", "album", "indexing", and "auto sync"
- keep prompts short and concrete
- explain what is happening in plain language
- send actual image results when the user asks for matches and local file access permits it
- use ordinary folders only; system photo apps are out of scope for now

Do not:
- start with SQL, JSONL, or backend internals
- silently choose a non-working image model
- leave the album in a manual-only state without warning the user
- say setup is complete before search has been verified

---

## Trigger phrases

Use this skill when the user wants OpenClaw to set up, maintain, reconnect, or search a personal photo album.

Common triggers include:
- "index my photos"
- "build a photo album"
- "search my pictures"
- "find beach photos"
- "show my recent photos"
- "reconnect my photo album"

---

## When to use this skill

Use this skill when:
- the user wants to index a local photo folder into a searchable album
- the user wants natural-language, date, or tag search over their photos
- the user wants OpenClaw to keep the album fresh automatically
- the user wants matching image files returned during search
- the user wants to reconnect an existing album backend

---

## Out of scope

- one-off image captioning for a single image
- system photo app integrations
- raw SQL debugging as the main task

---

## What the user gets

- first-time indexing from a normal local folder
- searchable captions, tags, scene labels, objects, and visible text
- text, tag, date, and recent-photo search
- automatic incremental maintenance through heartbeat or an equivalent periodic trigger
- actual image files returned during search when possible

---

## Definition of Done

This task is NOT complete until all of the following are true:

1. the photo library folder is chosen and readable
2. image analysis is verified to work in the current OpenClaw runtime
3. the album backend is created or reconnected and writable
4. the initial import succeeds, or an existing album is verified reachable
5. automatic maintenance is configured
6. a real search is verified against the indexed backend
7. the user has been sent the full Step 7 handoff message

---

## Common failure modes

Agents often make one of these mistakes:
- indexing before verifying that the image model actually works
- finishing the first import but forgetting to enable automatic maintenance
- leading with backend setup details and losing the user onboarding flow
- saying setup is done without verifying search on the indexed backend

Treat the final handoff and the first verified search as required setup steps.

---

## Terminology

Use this distinction consistently:

| Internal term | User-facing explanation |
|---------------|-------------------------|
| photo library folder | the local folder that will be indexed recursively |
| album backend | where the searchable photo index is stored |
| album profile | the local saved setup information needed to reconnect later |
| manifest JSONL | internal ingestion file used during implementation and debugging |
| captioned JSONL | internal import file used during implementation and debugging |

Preferred wording:
- use "photo library folder" for the user's local image root
- use "album backend" for the searchable storage target
- use "auto sync" for periodic maintenance in normal conversation

If the user asks "What do I need to save for later?" answer plainly:

> Save the album profile details and backend connection details needed to reconnect this same album later.

---

## Onboarding

### Step 0 - Choose the setup mode

`[AGENT]` Ask the user before doing anything else:

> Which setup do you want?
> 1. Create a new photo album
> 2. Reconnect an existing photo album
> 3. Search an already configured album
>
> If you choose reconnect, send the saved album profile or the backend details you used before.

Branching:
- if the user chooses `1`, continue to Step 1
- if the user chooses `2`, verify the saved backend is reachable, then continue to Step 5 or Step 6 as appropriate
- if the user chooses `3`, use the existing album backend and go directly to the user search flow
- if the user wants search but no configured album exists, tell them setup is required first

### Step 1 - Ask for the photo library folder

First ask the user for one local folder path.

Good prompt style:
- ask for exactly one folder path
- explain that the skill will index that folder recursively
- make it clear that ordinary folders are supported and system photo apps are out of scope

Do not continue setup until the user has provided a folder.

### Step 2 - Run preflight checks

Before indexing anything, verify all of the following:
- the folder exists and is readable
- the folder contains supported image files
- `agents.defaults.imageModel` points to a vision-capable model
- image analysis actually works in the current OpenClaw runtime

Do not trust the model name alone; test with a real image if needed.

If preflight fails:
- tell the user setup is blocked
- explain exactly what must be fixed
- do not proceed with fake captions or filename-derived descriptions

### Step 3 - Choose the backend

Prefer this backend decision rule:
- if `db9` CLI is installed and usable, prefer `db9`
- otherwise fall back to `TiDB Cloud Zero`

If the user is reconnecting an existing album, keep the same backend so the same album stays intact.

When using TiDB Zero:
- remind the user that Zero is temporary
- explicitly tell the user they should claim it if they want to keep the database
- preserve enough local config to reconnect later

### Step 4 - Create or reconnect the album backend

Create or reconnect the backend before running the first import.

For reconnect:
- verify the backend is reachable
- verify the target album can be searched or written
- if the saved configuration is incomplete, ask only for the missing backend details

For a new album:
- create the backend
- save the album profile details needed for future reconnect

Do not move on until the backend is confirmed reachable.

### Step 5 - Run the first indexing pass

Use this flow for a new album:

1. scan the photo library and build the manifest
2. generate short factual captions and retrieval tags with a vision-capable model
3. import the records into the backend

Caption rules:
- short
- factual
- retrieval-oriented
- visually grounded

Avoid:
- naming unknown people
- inferring sensitive traits
- speculative storytelling
- filename-based fake descriptions

Read `references/caption-schema.md` before generating records.

### Step 6 - Enable automatic maintenance

Automatic maintenance is the default operating mode after setup.

Do not consider setup complete until one of these is true:
- heartbeat is configured to maintain the album
- another periodic trigger is configured to run the same incremental flow

If neither is configured, tell the user setup is incomplete.

### Step 7 - Final handoff

`[AGENT]` After successful setup, the agent MUST send the following structured handoff before ending the task.
Translate the content into the user's language before sending.

```text
Your AI photo album is ready.

WHAT IS SET UP
- Photo library folder: <folder>
- Album backend: <db9 or tidb>
- Auto sync: <enabled trigger>

HOW TO SEARCH
- "find cats on the sofa"
- "show photos from 2026-03"
- "find photos tagged beach"
- "show my most recent photos"

RECOVERY
- Save the album profile and backend details needed to reconnect this album later.
- If this album uses TiDB Zero, claim it if you want to keep it.

WHAT HAPPENS NEXT
- New or changed photos will be picked up by auto sync.
- I can search the album for you any time and send matching images back.
```

Do not end setup without sending the handoff.

Immediately after setup:
- verify search with a real query against the indexed backend
- if the result is clear, summarize it and send the top matching image or images

---

## User search flow

When the user asks to find photos, use the indexed backend as the search source.

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
- if the user wants the photo itself, send the matching image file back

Preferred interaction style:
- if there is one clear match, send that image directly with a short explanation
- if there are multiple strong matches, summarize them first and send the top 1-3 images
- if results are weak or ambiguous, say so and suggest a better query

This skill should present as an AI photo librarian.

---

## Backend notes

### db9

Prefer db9 when available because it fits this skill well:
- database-per-user model
- OpenClaw-friendly CLI flow
- Postgres + JSONB + vector + full-text search
- simple operational model for a personal AI album

Credential handling:
- `db9` account auth for the CLI is usually enough
- `db9 login`, `db9 claim`, or `db9 login --api-key ...` stores auth in `~/.db9/credentials`
- scripts identify the target database by database name or id
- the skill normally does not need to copy raw Postgres passwords into extra files

### TiDB Cloud Zero / Starter

Use TiDB when db9 is unavailable or when the user wants TiDB specifically.

Good fit:
- disposable demo backend via Zero
- longer-lived backend after claim to Starter
- HTTP SQL API path without requiring a local MySQL CLI

Important caveats:
- TiDB auto embedding is mainly for text
- use caption text or external image embeddings for image retrieval

Credential handling:
- prefer HTTP SQL API credentials
- use MySQL-compatible connection details only when already available
- if no local MySQL client is available, use the HTTP SQL API
- avoid copying passwords into shell history or plaintext config files outside the skill's working config

For TiDB Zero specifically, preserve enough local config to reconnect:
- database host
- username
- password
- database name
- claim URL
- expiration time

If the backend is TiDB Zero, remind the user that long-term durability starts after they claim it.

---

## Initialization commands

Use these commands after the user-facing onboarding steps above.

### 1. Create the backend database

#### db9 path

```bash
# db9 create --name my-album
python3 scripts/init_db.py <db> --backend db9
```

#### TiDB path

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

### 3. Generate initial captions

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

### 4. Import the initial records

#### db9 path

```bash
python3 scripts/import_records.py <db> /tmp/photos.captioned.jsonl --backend db9
```

#### TiDB path

```bash
python3 scripts/import_records.py /path/to/tidb-target.json /tmp/photos.captioned.jsonl --backend tidb
```

---

## Heartbeat maintenance flow

When a heartbeat arrives for a configured album:

1. run incremental sync:

```bash
python3 scripts/sync_photos.py <target> <photo-folder> --backend <db9|tidb>
```

2. read the JSON output
3. if `to_caption` is `0`, return `HEARTBEAT_OK`
4. if `to_caption` is greater than `0`:
   - read `incremental_manifest_jsonl`
   - caption only those new or changed records with a vision-capable model
   - write a captioned JSONL file
   - import it with `import_records.py`
5. stay quiet unless:
   - indexing failed
   - user attention is needed
   - the user explicitly asked for progress updates

### Incremental rule

Incrementality is determined by:
- `file_path`
- `sha256`

If both match an existing record, skip the image.
If either differs, treat it as new or changed and reprocess it.

---

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

---

## Reference files

### `references/caption-schema.md`
Read this before generating the captioned JSONL. It defines the expected output shape for the vision step.

---

## Notes on search

This base workflow uses structured image understanding plus text retrieval fields. Search quality comes from captions, tags, scene labels, objects, and visible text.

For the first usable album, search works well enough via:
- caption text
- tags
- scene labels
- objects
- visible text in image

If the user later wants stronger semantic retrieval, extend the same workflow with a text or image embedding step after captions are generated.

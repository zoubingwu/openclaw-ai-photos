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

ai-photos turns one or more local photo sources into a searchable AI photo album for OpenClaw.

When talking to users:
- say "photo sources", "album", "indexing", and "auto sync"
- keep prompts short and concrete
- use plain language
- support local photo sources only, not system photo apps
- do not say setup is complete before one real search is verified

## Required outcome

This task is not complete until all of the following are true:

1. at least one photo source is chosen and readable for a new album
2. image analysis is verified to work in the current OpenClaw runtime
3. the album backend is created or reconnected and writable
4. the first import succeeds, or an existing album is verified reachable
5. the user explicitly approved auto sync or explicitly declined it
6. if auto sync was approved, OpenClaw heartbeat is configured, `HEARTBEAT.md` is written, and one verification heartbeat has run
7. a real search is verified against the indexed backend
8. the user has been sent the final handoff

## Core terms

- `photo sources`: one or more local paths scanned into the same album
- `album backend`: where the searchable photo index is stored
- `album profile`: saved reconnect information, stored automatically under `~/.openclaw/ai-photos/albums/default.json`
- `caption input JSONL`: the manifest file that still needs vision captions and import

If the user asks what to save for later, say:

> OpenClaw saves the album profile automatically at `~/.openclaw/ai-photos/albums/default.json`. Save that file only if you want a manual backup.

## Onboarding

### Step 0 - Choose mode

`[AGENT]` Ask first:

> Which setup do you want?
> 1. Create a new photo album
> 2. Reconnect an existing photo album
> 3. Search an already configured album
>
> For reconnect, I will first try the saved default album profile automatically. I will only ask for backend details if that is missing or incomplete.

Branching:
- `1`: continue to Step 1
- `2`: continue to Step 3 and Step 4
- `3`: go directly to Search flow
- if the user wants search but no configured album exists, tell them setup is required first

### Step 1 - Ask for photo sources

Ask for one or more local photo source paths.
Do not continue until the user has provided at least one photo source.

### Step 2 - Run preflight

Before indexing anything, verify:
- each photo source exists and is readable
- the selected sources contain supported image files
- `agents.defaults.imageModel` is vision-capable
- image analysis actually works on a real image in the current OpenClaw runtime

If preflight fails:
- tell the user setup is blocked
- explain exactly what must be fixed
- stop

### Step 3 - Choose the backend

- if reconnecting, keep the existing backend
- otherwise use `db9` if it is installed and usable
- if `db9` is not available, use `TiDB Cloud Zero`
- if using `TiDB Cloud Zero`, tell the user to claim it if they want to keep it

### Step 4 - Create or reconnect the album

For a new album, run exactly one setup command:

```bash
# db9
python3 scripts/setup_album.py --source <photo-source-a> --source <photo-source-b> --backend db9 --target <db>

# TiDB
python3 scripts/setup_album.py --source <photo-source-a> --source <photo-source-b> --backend tidb --target /path/to/tidb-target.json
```

Read the JSON output:
- `profile_path` tells you where the default album profile was saved
- `caption_input_jsonl` is the input for the first record ingestion pass
- `sync.to_caption` tells you how many records still need captions and import

For reconnect:
- try the saved default album profile first
- verify the backend is reachable
- verify the album can be searched or written
- ask only for missing backend details

Do not continue until the backend is confirmed reachable.

### Step 5 - Run the shared record ingestion flow

Use this same flow for:
- the first album import
- later incremental updates

Input:
- first import: `caption_input_jsonl` from `setup_album.py`
- later updates: `incremental_manifest_jsonl` from `sync_photos.py`

Before generating records, read `references/caption-schema.md`.

`[AGENT]` For each record in the input manifest:
1. inspect the referenced image with a vision-capable model
2. preserve the original fields
3. add `caption`, `tags`, `scene`, `objects`, and `text_in_image`
4. write one JSON object per line into a captioned JSONL file
5. import it with:

```bash
python3 scripts/import_records.py /tmp/photos.captioned.jsonl
```

Rules:
- keep captions short, factual, retrieval-oriented, and visually grounded
- do not invent names, sensitive traits, or stories
- if there is nothing to caption, skip this step

### Step 6 - Enable auto sync

`[AGENT]` Ask the user:

> I can enable auto sync for this album.
> OpenClaw will periodically check these photo sources for new or changed files and update the album.
>
> Do you want me to enable auto sync now?

If the user says yes, run:

```bash
openclaw config set agents.defaults.heartbeat.every "30m"
openclaw config set agents.defaults.heartbeat.target "last"
openclaw config set agents.defaults.heartbeat.lightContext true --strict-json
openclaw config validate
openclaw config get agents.defaults.workspace
```

Then create or update `<workspace>/HEARTBEAT.md` with:

```md
# ai-photos heartbeat

- Run `python3 <absolute-path-to-ai-photos>/scripts/sync_photos.py`.
- If `to_caption` is `0`, reply `HEARTBEAT_OK`.
- If `to_caption` is greater than `0`, run the shared record ingestion flow using `incremental_manifest_jsonl`.
- Stay quiet unless indexing failed or user attention is needed.
```

Then verify once:

```bash
openclaw system event --text "Run ai-photos heartbeat now" --mode now
openclaw system heartbeat last
```

Then tell the user the result:
- success: auto sync is enabled and the verification heartbeat ran
- declined: the album is ready, but future changes require manual sync
- failed: the album is usable, but auto sync is not active yet

### Step 7 - Final handoff

`[AGENT]` Before ending the task, send the user a handoff that includes:
- photo sources
- album backend
- auto sync status
- 3 or 4 example searches
- the default album profile location
- TiDB claim reminder if TiDB Zero is used

Immediately after setup:
- verify one real search against the indexed backend
- if the result is clear, summarize it and send the top matching image or images
- if the user declined auto sync, say clearly that the album is in manual-only mode

## Search flow

When the user asks to find photos, run:

```bash
python3 scripts/search_photos.py --text "cat on sofa"
python3 scripts/search_photos.py --tag cat
python3 scripts/search_photos.py --date 2026-03
python3 scripts/search_photos.py --recent
```

When answering:
- summarize the best matches clearly
- mention filenames, dates, or captions when useful
- send image files when possible
- if results are weak, say so and suggest a better query

## Heartbeat run behavior

When a heartbeat arrives for a configured album:

1. run:

```bash
python3 scripts/sync_photos.py
```

2. read the JSON output
3. if `to_caption` is `0`, return `HEARTBEAT_OK`
4. if `to_caption` is greater than `0`, run the shared record ingestion flow using `incremental_manifest_jsonl`
5. stay quiet unless indexing failed or user attention is needed

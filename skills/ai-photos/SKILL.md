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
- try to match the user's language
- explain the outcome simply: choose local folders now, then use OpenClaw to search and organize them
- describe capabilities and next steps instead of prescribing exact product or storage terminology
- avoid internal terms unless the user asks or troubleshooting requires them
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

## Internal terms

Use these terms for agent reasoning, troubleshooting, or recovery only.
Do not introduce them to the user unless needed.

- `photo sources`: one or more local paths scanned into the same album
- `album backend`: where the searchable photo index is stored
- `album profile`: saved reconnect information, stored automatically under `~/.openclaw/ai-photos/albums/default.json`
- `caption input JSONL`: the manifest file that still needs vision captions and import

If the user asks what to save for later, explain that OpenClaw saves the reconnect information automatically at `~/.openclaw/ai-photos/albums/default.json`, and that they only need to keep that file if they want a manual backup.

## Onboarding

### Step 0 - Choose mode

User-facing:

- Ask whether the user wants to create a new photo album, reconnect an existing one, or search an already configured album.
- If they want to reconnect, explain that you will try the saved connection first and only ask for more details if needed.

`[AGENT]` Branching:

- `1`: continue to Step 1
- `2`: continue to Step 3 and Step 4
- `3`: go directly to Search flow
- if the user wants search but no configured album exists, tell them setup is required first

### Step 1 - Ask for photo folders

User-facing:

- Ask for one or more local folder paths that contain photos.

`[AGENT]`

Do not continue until the user has provided at least one photo source.

### Step 2 - Run preflight

User-facing:

- Tell the user you will quickly verify that the folders are readable and that image analysis works before importing anything.

`[AGENT]`

Before indexing anything, verify:
- each photo source exists and is readable
- the selected sources contain supported image files
- `agents.defaults.imageModel` is vision-capable
- image analysis actually works on a real image in the current OpenClaw runtime

If preflight fails:
- tell the user setup is blocked in plain language
- explain exactly what must be fixed without exposing unnecessary implementation details
- stop

### Step 3 - Choose the backend

`[AGENT]`

- if reconnecting, keep the existing backend
- otherwise use `db9` if it is installed and usable
- if `db9` is not available, use `TiDB Cloud Zero`
- if using `TiDB Cloud Zero`, tell the user to claim it if they want to keep it, but do not lead with backend details unless they matter

### Step 4 - Create or reconnect the album

User-facing for a new album:

- Tell the user setup is in progress and that the selected folders will be searchable through OpenClaw when it finishes.

`[AGENT]`

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

`[AGENT]` For reconnect:
- try the saved default album profile first
- verify the backend is reachable
- verify the album can be searched or written
- ask only for missing backend details

Do not continue until the backend is confirmed reachable.

### Step 5 - Run the shared record ingestion flow

Use this same flow for:
- the first album import
- later incremental updates

User-facing:

- Tell the user photos are being imported and analyzed, and that large libraries may take some time.

`[AGENT]`

Input:
- first import: `caption_input_jsonl` from `setup_album.py`
- later updates: `incremental_manifest_jsonl` from `sync_photos.py`

Before generating records, read `references/caption-schema.md`.

`[AGENT]` For each record in the input manifest:
1. run `python3 scripts/prepare_image.py --mode caption <file_path>`
2. send the returned `output_path` to the vision-capable model
3. preserve the original manifest fields from the source image
4. add `caption`, `tags`, `scene`, `objects`, and `text_in_image`
5. write one JSON object per line into a captioned JSONL file
6. import it with:

```bash
python3 scripts/import_records.py /tmp/photos.captioned.jsonl
```

Rules:
- keep captions short, factual, retrieval-oriented, and visually grounded
- do not invent names, sensitive traits, or stories
- do not replace the original `file_path` with the temporary derived image path
- if there is nothing to caption, skip this step

### Step 6 - Enable auto sync

User-facing:

- Offer automatic updates in plain language.
- Explain that OpenClaw can periodically check the selected folders for new or changed photos and update the album.
- Ask whether the user wants to enable that now.

`[AGENT]`

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
- success: explain that automatic updates are active and the verification run succeeded
- declined: explain that the album is ready, but future changes require a manual update
- failed: explain that the album is usable, but automatic updates are not active yet

### Step 7 - Final handoff

User-facing handoff should include:
- connected photo folders
- what the user can now do in OpenClaw
- automatic update status
- 3 or 4 example searches
- a cloud claim reminder if the chosen storage needs it

Do not proactively include backend names, profile paths, JSONL files, or other internal implementation details unless the user asks or recovery requires them.

`[AGENT]`

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
- summarize the best matches clearly and in plain language
- mention filenames, dates, or captions when useful
- avoid explaining internal ingestion or storage details unless the user asks
- before sending an image file, run `python3 scripts/prepare_image.py --mode preview <matched-file>`
- send the returned `output_path` when possible
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

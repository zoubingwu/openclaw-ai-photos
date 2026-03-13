# AI Photos Image Derivation Design

## Goal

Reduce transfer cost in two places without changing the original photo files:

1. before sending an image to the vision model
2. before sending a matched image back to the user

The solution must use operating system tools already available on macOS.

## Non-goals

- do not modify original photos
- do not introduce persistent caches
- do not add third-party image processing dependencies
- do not change indexing or search semantics

## Options

### Option A: SKILL-only shell snippets

Add raw `sips` commands directly into `SKILL.md` for caption input and preview output.

Pros:
- minimal code changes

Cons:
- duplicated rules
- messy temp-file handling
- harder to test and maintain

### Option B: Shared derivation script

Add a small shared script that wraps `sips` and emits structured JSON.

Pros:
- one source of truth for resize and quality rules
- reusable for caption and preview paths
- easy to test
- no new dependencies

Cons:
- one extra script file

### Option C: Persistent derived-image cache

Store compressed derivatives under managed storage and reuse them across runs.

Pros:
- less repeated work

Cons:
- cache invalidation and cleanup complexity
- unnecessary for the current scope

## Decision

Use Option B.

This keeps the implementation small, explicit, and easy to change. It avoids repeating `sips` logic in `SKILL.md` while staying dependency-free.

## Design

### New script

Add `skills/ai-photos/scripts/prepare_image.py`.

Inputs:
- source image path
- mode: `caption` or `preview`

Output:
- JSON with:
  - `used_original`
  - `input_path`
  - `output_path`
  - `mode`
  - `width`
  - `height`

### Derivation rules

`caption` mode:
- longest edge target: `1536`
- JPEG quality: `75`
- if the source longest edge is `<= 1600`, use the original file

`preview` mode:
- longest edge target: `2048`
- JPEG quality: `80`
- if the source longest edge is `<= 2200`, use the original file

### Tooling

Use macOS `sips` only:
- read dimensions
- resize by max edge
- export JPEG with quality setting

### Temporary files

Derived files live under the system temp directory, for example:
- `/tmp/ai-photos-derived/<hash>.caption.jpg`
- `/tmp/ai-photos-derived/<hash>.preview.jpg`

The filename should include:
- source path and metadata hash
- mode

This prevents collisions while keeping the logic simple.

## Integration points

### Caption flow

Before a record is sent to the vision model:

1. run `prepare_image.py --mode caption <image>`
2. pass `output_path` to the vision model
3. keep all indexing records tied to the original image path

### Preview flow

Before sending an image back to the user:

1. run `prepare_image.py --mode preview <image>`
2. send `output_path` to the user
3. keep the original path for search and metadata

### SKILL updates

`SKILL.md` should only describe:
- that OpenClaw prepares a smaller temporary image before captioning
- that OpenClaw prepares a smaller temporary image before sending a result image

It should not inline the raw `sips` commands.

## Error handling

- if `sips` is unavailable, fail clearly and fall back to the original image path only if the caller explicitly allows it
- if derivation fails for one image, surface the error instead of silently pretending compression succeeded
- if the source image is already small enough, return `used_original: true`

## Testing

1. verify `prepare_image.py` returns original paths for small images
2. verify it creates JPEG derivatives for large images
3. verify `caption` and `preview` modes use different thresholds and quality settings
4. verify temp output paths are stable and non-conflicting
5. verify `SKILL.md` references the new step without exposing implementation noise

## Implementation order

1. add `prepare_image.py`
2. add script-level tests or shell verification for both modes
3. update `SKILL.md` caption and preview flows
4. wire preview preparation into the user-facing image send path
5. wire caption preparation into the vision-caption path

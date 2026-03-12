# Caption Record Schema

Each JSONL line should contain manifest fields plus vision-model output.

## Required base fields
- `file_path`
- `filename`
- `sha256`
- `mime_type`
- `size_bytes`
- `width`
- `height`
- `taken_at`
- `exif`

## Vision fields
- `caption`: one short factual sentence
- `tags`: array of 5-12 short tags
- `scene`: short scene label
- `objects`: array of the main visible objects
- `text_in_image`: visible text or `null`

## Optional fields
- `metadata`: free-form JSON object
- `search_text`: concatenated retrieval text; if omitted, the import script builds it

## Example

```json
{
  "file_path": "/photos/2026/03/cat.jpg",
  "filename": "cat.jpg",
  "sha256": "abc123",
  "mime_type": "image/jpeg",
  "size_bytes": 231231,
  "width": 3024,
  "height": 4032,
  "taken_at": "2026-03-12T09:12:00+00:00",
  "exif": {"Make": "Apple", "Model": "iPhone 15 Pro"},
  "caption": "A white cat resting on a gray sofa near a sunlit window.",
  "tags": ["cat", "sofa", "indoor", "sunlight", "pet"],
  "scene": "living room",
  "objects": ["cat", "sofa", "window"],
  "text_in_image": null,
  "metadata": {"source": "demo"}
}
```
package app

import (
	"reflect"
	"testing"
)

func TestMergeImportRecordPreservesExistingTextAndMergesTags(t *testing.T) {
	record := map[string]any{
		"caption":       "",
		"scene":         "",
		"text_in_image": nil,
		"tags":          []any{"new", "shared"},
		"objects":       []any{"lamp"},
		"metadata":      map[string]any{"source": "manifest"},
	}
	existing := importExistingRecord{
		Caption:     "Existing caption",
		Scene:       "Existing scene",
		TextInImage: "Existing text",
		Tags:        []string{"old", "shared"},
		Objects:     []string{"chair"},
		Metadata:    map[string]any{"rating": "5"},
	}

	mergeImportRecord(record, existing)

	if got := record["caption"]; got != "Existing caption" {
		t.Fatalf("caption = %v, want existing caption", got)
	}
	if got := record["scene"]; got != "Existing scene" {
		t.Fatalf("scene = %v, want existing scene", got)
	}
	if got := record["text_in_image"]; got != "Existing text" {
		t.Fatalf("text_in_image = %v, want existing text", got)
	}

	wantTags := []string{"old", "shared", "new"}
	if got := record["tags"]; !reflect.DeepEqual(got, wantTags) {
		t.Fatalf("tags = %#v, want %#v", got, wantTags)
	}

	wantObjects := []string{"chair", "lamp"}
	if got := record["objects"]; !reflect.DeepEqual(got, wantObjects) {
		t.Fatalf("objects = %#v, want %#v", got, wantObjects)
	}

	wantMetadata := map[string]any{"rating": "5", "source": "manifest"}
	if got := record["metadata"]; !reflect.DeepEqual(got, wantMetadata) {
		t.Fatalf("metadata = %#v, want %#v", got, wantMetadata)
	}
}

func TestMergeImportBatchRebuildsSearchTextFromMergedValues(t *testing.T) {
	record := map[string]any{
		"caption":  "",
		"scene":    "",
		"tags":     []any{},
		"objects":  []any{"lamp"},
		"metadata": map[string]any{},
	}
	existing := importExistingRecord{
		Caption: "Existing caption",
		Scene:   "Existing scene",
		Tags:    []string{"old-tag"},
	}

	normalizeRecord(record)
	mergeImportRecord(record, existing)
	record["search_text"] = buildSearchText(record)

	got := record["search_text"]
	want := "Existing caption Existing scene old-tag lamp"
	if got != want {
		t.Fatalf("search_text = %q, want %q", got, want)
	}
}

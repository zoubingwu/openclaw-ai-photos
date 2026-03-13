package app

import (
	"strings"
	"testing"
)

func TestValidateSearchParamsRejectsRecentMix(t *testing.T) {
	t.Parallel()

	err := validateSearchParams(SearchParams{
		Text:   "cat",
		Recent: true,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestBuildSearchQueriesDB9(t *testing.T) {
	t.Parallel()

	listSQL, countSQL := buildSearchQueries("db9", normalizeSearchParams(SearchParams{
		Text:     "cat sofa",
		Page:     2,
		PageSize: 20,
	}))

	if !strings.Contains(listSQL, "websearch_to_tsquery") {
		t.Fatalf("expected db9 text query, got %q", listSQL)
	}
	if !strings.Contains(listSQL, "OFFSET 20") {
		t.Fatalf("expected second page offset, got %q", listSQL)
	}
	if !strings.Contains(countSQL, "COUNT(*)") {
		t.Fatalf("expected count query, got %q", countSQL)
	}
}

func TestBuildSearchQueriesTiDBTag(t *testing.T) {
	t.Parallel()

	listSQL, _ := buildSearchQueries("tidb", normalizeSearchParams(SearchParams{
		Tag:      "cat",
		Page:     1,
		PageSize: 18,
	}))
	if !strings.Contains(listSQL, "JSON_CONTAINS") {
		t.Fatalf("expected TiDB tag filter, got %q", listSQL)
	}
}

package api

import (
	"context"
	"manga-manager/internal/metadata"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type searchOnlySource struct{ limit, offset int }

func (*searchOnlySource) Name() string { return "controlled" }
func (s *searchOnlySource) SearchMetadata(_ context.Context, title string, limit, offset int) ([]*metadata.SeriesMetadata, int, error) {
	s.limit = limit
	s.offset = offset
	return []*metadata.SeriesMetadata{{Title: title}}, 72, nil
}
func TestMetadataSearchConsumesSearchOnlySource(t *testing.T) {
	c, _, _, _ := newTestController(t)
	source := &searchOnlySource{}
	c.providers.searcherFactory = func(string) metadata.Searcher { return source }
	rec := httptest.NewRecorder()
	c.providers.scrapeSearchMetadata(rec, requestWithRouteParam(http.MethodGet, "/?q=sample&limit=999&offset=2", nil, "seriesId", "1"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "sample") || !strings.Contains(rec.Body.String(), `"total":72`) || source.limit != 50 || source.offset != 2 {
		t.Fatalf("response=%d %s limit=%d offset=%d", rec.Code, rec.Body.String(), source.limit, source.offset)
	}
}

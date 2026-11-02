package api

import (
	"errors"
	"net/http"

	"manga-manager/internal/catalogsearch"
)

func (c *ReaderHTTP) searchBooks(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	target := r.URL.Query().Get("target") // "all", "series", "book"
	if target == "" {
		target = "all"
	}

	if query == "" {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"hits": []interface{}{}})
		return
	}

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	result, err := c.search.Query(r.Context(), subject, query, target, 20)
	if errors.Is(err, catalogsearch.ErrTarget) {
		jsonError(w, http.StatusBadRequest, "Invalid search target")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Search failed")
		return
	}
	out := SearchResult{Total: result.Total, MaxScore: result.MaxScore}
	for _, hit := range result.Hits {
		out.Hits = append(out.Hits, &SearchHit{ID: hit.ID, Score: hit.Score, Fields: map[string]interface{}{"id": hit.ID, "title": hit.Title, "series_name": hit.SeriesName, "type": hit.Kind, "cover_path": hit.CoverPath}})
	}
	jsonResponse(w, http.StatusOK, out)
}

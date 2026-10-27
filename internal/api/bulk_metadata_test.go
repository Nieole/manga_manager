package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"manga-manager/internal/database"
)

func TestBulkMetadataTransactionAndRefreshFailure(t *testing.T) {
	for _, fail := range []string{"series_tags", "series_stats", ""} {
		t.Run("failure_"+fail, func(t *testing.T) {
			c, store, _, _ := newTestController(t)
			lib, err := store.CreateLibrary(t.Context(), database.CreateLibraryParams{Name: "Bulk", Path: t.TempDir(), ScanMode: "none"})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 2)
			for i := range ids {
				s, err := store.CreateSeries(t.Context(), database.CreateSeriesParams{LibraryID: lib.ID, Name: fmt.Sprint(i), Path: filepath.Join(lib.Path, fmt.Sprint(i)), NameInitial: "B"})
				if err != nil {
					t.Fatal(err)
				}
				ids[i] = s.ID
			}
			if fail != "" {
				_, err := store.DB().Exec(fmt.Sprintf("CREATE TRIGGER reject_bulk BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'rejected'); END", fail))
				if err != nil {
					t.Fatal(err)
				}
			}
			status := "completed"
			selectedIDs := make([]int64, 502)
			for i := range selectedIDs {
				selectedIDs[i] = ids[0]
			}
			selectedIDs[499], selectedIDs[500], selectedIDs[501] = ids[1], ids[1], 99999
			body, err := json.Marshal(BulkEditSeriesRequest{SeriesIDs: selectedIDs, AddTags: []string{"Action"}, Status: &status})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			c.bulkEditSeries(rec, httptest.NewRequest(http.MethodPost, "/api/series/bulk-edit", bytes.NewReader(body)))
			want := http.StatusOK
			if fail == "series_tags" {
				want = http.StatusInternalServerError
			}
			if rec.Code != want {
				t.Fatalf("status %d want %d: %s", rec.Code, want, rec.Body.String())
			}
			for _, id := range ids {
				got, err := store.GetSeries(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				tags, err := store.GetTagsForSeries(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				if fail == "series_tags" {
					if got.Status.Valid || len(tags) != 0 {
						t.Fatalf("failed edit committed: %+v %+v", got, tags)
					}
				} else if got.Status.String != "completed" || len(tags) != 1 {
					t.Fatalf("committed edit missing: %+v %+v", got, tags)
				}
			}
			var refreshed int
			if err := store.DB().QueryRow("SELECT count(*) FROM series_stats WHERE tag_names_cache='Action'").Scan(&refreshed); err != nil {
				t.Fatal(err)
			}
			wantRefreshed := 0
			if fail == "" {
				wantRefreshed = 2
			}
			if refreshed != wantRefreshed {
				t.Fatalf("refreshed caches = %d, want %d", refreshed, wantRefreshed)
			}
			var result struct {
				Updated          int     `json:"updated"`
				Committed        bool    `json:"committed"`
				SeriesIDs        []int64 `json:"committed_series_ids"`
				RefreshFailedIDs []int64 `json:"refresh_failed_series_ids"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if fail == "series_tags" {
				if result.Committed || result.Updated != 0 || len(result.SeriesIDs) != 0 {
					t.Fatalf("uncommitted result: %+v", result)
				}
			} else if !result.Committed || result.Updated != 502 || len(result.SeriesIDs) != 2 {
				t.Fatalf("committed result: %+v", result)
			}
			if fail == "series_stats" && len(result.RefreshFailedIDs) != 2 {
				t.Fatalf("missing postcommit warning: %+v", result)
			}
			if fail != "series_stats" && len(result.RefreshFailedIDs) != 0 {
				t.Fatalf("unexpected postcommit warning: %+v", result)
			}
		})
	}
}

func TestBulkMetadataRequestResults(t *testing.T) {
	c, _, _, _ := newTestController(t)
	for _, tc := range []struct {
		body     string
		status   int
		response string
	}{
		{"{", 400, ""}, {`{"series_ids":[]}`, 200, ""},
	} {
		rec := httptest.NewRecorder()
		c.bulkEditSeries(rec, httptest.NewRequest(http.MethodPost, "/api/series/bulk-edit", bytes.NewBufferString(tc.body)))
		if rec.Code != tc.status || (tc.response != "" && rec.Body.String() != tc.response) {
			t.Fatalf("response %d: %s", rec.Code, rec.Body.String())
		}
		if tc.status == http.StatusOK {
			var result bulkEditSeriesResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Committed || result.Updated != 0 || len(result.SeriesIDs) != 0 || len(result.RefreshFailedIDs) != 0 {
				t.Fatalf("empty selection: %+v", result)
			}
		}
	}
}

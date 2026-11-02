package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/proposal"
)

type interleavingProposalStore struct {
	database.Store
	beforeTransaction func()
}

func (s interleavingProposalStore) ExecTx(ctx context.Context, fn func(proposal.Queries) error) error {
	s.beforeTransaction()
	return s.Store.ExecTx(ctx, func(q *database.Queries) error { return fn(q) })
}

func TestPartialApplyHTTPClassifiesTransactionCurrentOutcome(t *testing.T) {
	for _, decision := range []string{"rejected", "filled", "fault"} {
		t.Run(decision, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			series, review := seedPartialApplyFixture(t, controller, store, "Alpha")
			original := controller.proposals
			controller.organizationHTTP.proposals = proposal.NewService(interleavingProposalStore{Store: store, beforeTransaction: func() {
				switch decision {
				case "rejected":
					got, err := original.Reject(context.Background(), review.ID)
					if err != nil || got.Status != proposal.RejectRejected {
						t.Fatalf("Reject = %+v, %v", got, err)
					}
				case "filled":
					_, err := store.(*database.SqlStore).DB().ExecContext(context.Background(), `UPDATE series SET summary = 'manual', publisher = 'manual' WHERE id = ?`, series.ID)
					if err != nil {
						t.Fatal(err)
					}
				case "fault":
					_, err := store.(*database.SqlStore).DB().ExecContext(context.Background(), `CREATE TRIGGER fail_field_delete BEFORE DELETE ON metadata_review_fields BEGIN SELECT RAISE(ABORT, 'injected storage failure'); END`)
					if err != nil {
						t.Fatal(err)
					}
				}
			}})
			body := []byte(`{"review_ids":[` + strconv.FormatInt(review.ID, 10) + `],"mode":"fill_empty"}`)
			rec := httptest.NewRecorder()
			controller.bulkApplyMetadataReviews(rec, httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-apply", bytes.NewReader(body)))
			if rec.Code != http.StatusOK {
				t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
			}
			var got metadataReviewBulkResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			switch decision {
			case "rejected":
				if len(got.Conflict) != 1 || len(got.Failed) != 0 || !got.Success {
					t.Fatalf("rejection outcome: %+v", got)
				}
			case "filled":
				if len(got.Skipped) != 1 || len(got.Failed) != 0 || !got.Success {
					t.Fatalf("no-change outcome: %+v", got)
				}
			case "fault":
				if len(got.Failed) != 1 || len(got.Conflict) != 0 || got.Success {
					t.Fatalf("fault outcome: %+v", got)
				}
			}
			if len(got.Applied) != 0 || len(got.Partial) != 0 {
				t.Fatalf("uncommitted operation reported a write: %+v", got)
			}
		})
	}
}

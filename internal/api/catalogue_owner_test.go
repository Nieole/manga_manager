package api

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/reading"
	"manga-manager/internal/recommendation"
	"manga-manager/internal/seriescontext"
	"manga-manager/internal/seriesreview"
	"math"
	"testing"
	"time"
)

func TestSeriesReviewCompleteIntentAndIsolation(t *testing.T) {
	_, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	a := mkTestUser(t, store, "review-a", "user")
	b := mkTestUser(t, store, "review-b", "user")
	sa, _ := reading.UserSubject(a.ID)
	sb, _ := reading.UserSubject(b.ID)
	service := seriesreview.New(store)
	rating := 4.5
	out, err := service.Save(context.Background(), sa, series.ID, &rating, "  thoughtful  ")
	if err != nil || !out.Exists || out.Review != "thoughtful" {
		t.Fatalf("save=%+v err=%v", out, err)
	}
	if got, err := service.Get(context.Background(), sb, series.ID); err != nil || got.Exists {
		t.Fatalf("other=%+v err=%v", got, err)
	}
	invalid := math.NaN()
	if _, err := service.Save(context.Background(), sa, series.ID, &invalid, "bad"); !errors.Is(err, seriesreview.ErrRating) {
		t.Fatalf("rating=%v", err)
	}
	if got, err := service.Get(context.Background(), sa, series.ID); err != nil || got.Review != "thoughtful" {
		t.Fatalf("failed write changed review=%+v err=%v", got, err)
	}
	if out, err := service.Save(context.Background(), sa, series.ID, nil, "  "); err != nil || out.Exists {
		t.Fatalf("clear=%+v err=%v", out, err)
	}
}
func TestSeriesContextOwnerDistinguishesAncillaryFailure(t *testing.T) {
	c, store, _, root := newTestController(t)
	_, series, _ := seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	_, err := seriescontext.New(failedContextTags{store}, c.relations, c.proposals, c.taskEngine.engine).Load(context.Background(), reading.AnonymousSubject(), series.ID)
	var failed *seriescontext.AuxiliaryError
	if !errors.As(err, &failed) || failed.Part != "tags" {
		t.Fatalf("error=%v", err)
	}
}

type controlledRecommender struct{ calls int }

func (p *controlledRecommender) GenerateRecommendations(ctx context.Context, _ []string, candidates []metadata.CandidateSeries, _ int) ([]metadata.AIRecommendation, error) {
	p.calls++
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return []metadata.AIRecommendation{{SeriesID: candidates[0].ID, Reason: "controlled"}}, nil
}
func TestRecommendationOwnerPartitionsCachesAndInvalidates(t *testing.T) {
	_, store, _, root := newTestController(t)
	seedBookFixture(t, store, root, "Library", "Series", "01.cbz", 10)
	if _, err := store.(*database.SqlStore).DB().Exec("UPDATE series SET summary='A candidate summary'"); err != nil {
		t.Fatal(err)
	}
	a := mkTestUser(t, store, "rec-a", "user")
	b := mkTestUser(t, store, "rec-b", "user")
	sa, _ := reading.UserSubject(a.ID)
	sb, _ := reading.UserSubject(b.ID)
	service := recommendation.New(time.Hour)
	source := &controlledRecommender{}
	factory := func() metadata.Recommender { return source }
	load := func(subject reading.Subject, locale string) {
		t.Helper()
		items, err := service.Load(context.Background(), store, subject, locale, false, factory)
		if err != nil || len(items) != 1 || items[0].Reason != "controlled" {
			t.Fatalf("items=%+v err=%v", items, err)
		}
	}
	load(sa, "en-US")
	load(sa, "en-US")
	if source.calls != 1 {
		t.Fatalf("cache calls=%d", source.calls)
	}
	load(sb, "en-US")
	load(sa, "zh-CN")
	if source.calls != 3 {
		t.Fatalf("partition calls=%d", source.calls)
	}
	service.Purge()
	load(sa, "en-US")
	if source.calls != 4 {
		t.Fatalf("purge calls=%d", source.calls)
	}
}

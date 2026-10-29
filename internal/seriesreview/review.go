// Package seriesreview owns reader-specific series ratings and review text.
package seriesreview

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"manga-manager/internal/reading"
	"math"
	"strings"
	"time"
)

type Store interface {
	GetUserSeriesReview(context.Context, int64, int64) (database.UserSeriesReview, bool, error)
	UpsertUserSeriesReview(context.Context, int64, int64, *float64, string) error
	DeleteUserSeriesReview(context.Context, int64, int64) error
}

var ErrRating = errors.New("rating must be between 0 and 5")

type Result struct {
	Exists    bool
	Rating    *float64
	Review    string
	UpdatedAt *time.Time
}
type Service struct{ store Store }

func New(store Store) *Service { return &Service{store} }
func (s *Service) Get(ctx context.Context, subject reading.Subject, id int64) (Result, error) {
	uid, err := subject.UserID()
	if err != nil || uid == 0 {
		return Result{}, err
	}
	row, found, err := s.store.GetUserSeriesReview(ctx, uid, id)
	if err != nil || !found {
		return Result{}, err
	}
	out := Result{Exists: true, Review: row.Review, UpdatedAt: &row.UpdatedAt}
	if row.Rating.Valid {
		v := row.Rating.Float64
		out.Rating = &v
	}
	return out, nil
}
func (s *Service) Save(ctx context.Context, subject reading.Subject, id int64, rating *float64, text string) (Result, error) {
	if rating != nil && (*rating < 0 || *rating > 5 || math.IsNaN(*rating) || math.IsInf(*rating, 0)) {
		return Result{}, ErrRating
	}
	uid, err := subject.UserID()
	if err != nil || uid == 0 {
		return Result{}, err
	}
	text = strings.TrimSpace(text)
	if rating == nil && text == "" {
		return Result{}, s.store.DeleteUserSeriesReview(ctx, uid, id)
	}
	if err := s.store.UpsertUserSeriesReview(ctx, uid, id, rating, text); err != nil {
		return Result{}, err
	}
	now := time.Now()
	return Result{Exists: true, Rating: rating, Review: text, UpdatedAt: &now}, nil
}
func (s *Service) Delete(ctx context.Context, subject reading.Subject, id int64) error {
	uid, err := subject.UserID()
	if err != nil || uid == 0 {
		return err
	}
	return s.store.DeleteUserSeriesReview(ctx, uid, id)
}

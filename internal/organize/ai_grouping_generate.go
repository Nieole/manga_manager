package organize

import (
	"context"
	"encoding/json"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"sort"
	"strings"
)

type aiGroupingReviewProposal struct {
	Name        string
	Description string
	SeriesIDs   []int64
}

func normalizeAIGroupingReviewProposals(candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) []aiGroupingReviewProposal {
	candidateIDs := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidateIDs[candidate.ID] = struct{}{}
	}

	proposals := make([]aiGroupingReviewProposal, 0, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		seriesIDs := make([]int64, 0, len(group.SeriesIDs))
		seen := make(map[int64]struct{}, len(group.SeriesIDs))
		for _, id := range group.SeriesIDs {
			if _, ok := candidateIDs[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			seriesIDs = append(seriesIDs, id)
		}
		sort.Slice(seriesIDs, func(i, j int) bool { return seriesIDs[i] < seriesIDs[j] })
		if len(seriesIDs) == 0 {
			continue
		}
		proposals = append(proposals, aiGroupingReviewProposal{
			Name:        name,
			Description: strings.TrimSpace(group.Description),
			SeriesIDs:   seriesIDs,
		})
	}
	return proposals
}

// QueueGenerated validates generated candidates and durably queues a collection review without applying it.
func (c *AIGrouping) QueueGenerated(ctx context.Context, libraryID int64, providerName string, candidates []metadata.CandidateSeries, groups []metadata.AIGroupCollection) (database.AiGroupingReview, int, error) {
	var created database.AiGroupingReview
	proposals := normalizeAIGroupingReviewProposals(candidates, groups)
	if len(proposals) == 0 {
		return created, 0, nil
	}
	payload, _ := json.Marshal(map[string]any{
		"candidates":  candidates,
		"collections": groups,
		"proposals":   proposals,
	})

	err := c.store.ExecTx(ctx, func(q *database.Queries) error {
		review, err := q.CreateAIGroupingReview(ctx, database.CreateAIGroupingReviewParams{
			LibraryID:       libraryID,
			Provider:        strings.TrimSpace(providerName),
			Status:          "pending",
			Summary:         "AI grouping review queued",
			RawPayload:      string(payload),
			CandidateCount:  int64(len(candidates)),
			CollectionCount: int64(len(proposals)),
		})
		if err != nil {
			return err
		}

		for _, proposal := range proposals {
			rawIDs, _ := json.Marshal(proposal.SeriesIDs)
			if _, err := q.CreateAIGroupingReviewCollection(ctx, database.CreateAIGroupingReviewCollectionParams{
				ReviewID:    review.ID,
				Name:        proposal.Name,
				Description: proposal.Description,
				SeriesIds:   string(rawIDs),
				SeriesCount: int64(len(proposal.SeriesIDs)),
				Status:      "pending",
			}); err != nil {
				return err
			}
		}

		created = review
		return nil
	})
	return created, len(proposals), err
}

// GroupingGenerator is the external AI boundary, separate from metadata proposals.
type GroupingGenerator interface {
	Name() string
	GenerateGrouping(context.Context, []metadata.CandidateSeries) ([]metadata.AIGroupCollection, error)
}

// GenerationObserver reports semantic phases and obeys the caller's cancellation and pause gate.
type GenerationObserver interface {
	Collecting()
	Requesting(int, string)
	Queueing()
	Checkpoint(context.Context) error
}

// GroupingGeneration is a queued suggestion, never an applied collection.
type GroupingGeneration struct {
	Review      database.AiGroupingReview
	Candidates  int
	Collections int
}

// GroupingGenerationError identifies the failed business phase while preserving its cause.
type GroupingGenerationError struct {
	Phase string
	Err   error
}

func (e *GroupingGenerationError) Error() string { return e.Err.Error() }
func (e *GroupingGenerationError) Unwrap() error { return e.Err }

// Generate selects ungrouped candidates, requests suggestions and atomically queues their review.
func (c *AIGrouping) Generate(ctx context.Context, libraryID int64, source func() GroupingGenerator, observer GenerationObserver) (GroupingGeneration, error) {
	observer.Collecting()
	rows, err := c.store.GetSeriesWithoutCollection(ctx, libraryID)
	if err != nil {
		return GroupingGeneration{}, &GroupingGenerationError{Phase: "fetch", Err: err}
	}
	if len(rows) == 0 {
		return GroupingGeneration{}, nil
	}
	if err = observer.Checkpoint(ctx); err != nil {
		return GroupingGeneration{}, err
	}
	if len(rows) > 50 {
		rows = rows[:50]
	}
	candidates := make([]metadata.CandidateSeries, 0, len(rows))
	for _, row := range rows {
		title := row.Title.String
		if title == "" {
			title = row.Name
		}
		candidates = append(candidates, metadata.CandidateSeries{ID: row.ID, Title: title, Summary: row.Summary.String})
	}
	provider := source()
	observer.Requesting(len(candidates), provider.Name())
	groups, err := provider.GenerateGrouping(ctx, candidates)
	if err != nil {
		return GroupingGeneration{}, &GroupingGenerationError{Phase: "generate", Err: err}
	}
	observer.Queueing()
	review, count, err := c.QueueGenerated(ctx, libraryID, provider.Name(), candidates, groups)
	if err != nil {
		return GroupingGeneration{}, &GroupingGenerationError{Phase: "review", Err: err}
	}
	return GroupingGeneration{Review: review, Candidates: len(candidates), Collections: count}, nil
}

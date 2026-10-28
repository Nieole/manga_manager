package bookcontent

import (
	"context"
	"time"

	"manga-manager/internal/parser"
)

// Publication separates original-file commit from subsequent fact persistence failure.
type Publication struct {
	Committed bool
	Source    Source
	Warning   error
}

// Publisher owns staging, publication and the handoff to facts and content caches.
type Publisher struct {
	persist    func(context.Context, Source) error
	invalidate func()
}

// NewPublisher binds the fact store and shared cache invalidation effects.
func NewPublisher(persist func(context.Context, Source) error, invalidate func()) *Publisher {
	return &Publisher{persist: persist, invalidate: invalidate}
}

// Publish leaves existing readers intact and never reports a committed replacement as
// a failed file write. Callers must surface Warning without automatically rewriting.
func (p *Publisher) Publish(ctx context.Context, source Source, info parser.ComicInfo) (Publication, error) {
	result := Publication{Source: source}
	stage, err := parser.PrepareComicInfo(ctx, source.Path, info)
	if err != nil {
		return result, err
	}
	defer stage.Close()
	stat, err := stage.Publish(ctx)
	if err != nil {
		return result, err
	}
	result.Committed = true
	result.Source.Size = stat.Size()
	result.Source.FileModifiedAt = stat.ModTime()
	// Cancellation after rename cannot undo the file; finish the bounded handoff.
	postCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if p.persist != nil {
		result.Warning = p.persist(postCtx, result.Source)
	}
	if p.invalidate != nil {
		p.invalidate()
	}
	return result, nil
}

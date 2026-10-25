// Package intelligence owns background metadata scraping and AI grouping generation.
// Metadata sources, metadata proposals and collection suggestions retain separate rules and storage.
package intelligence

import (
	"context"
	"manga-manager/internal/application"
	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
)

// Store supplies candidate discovery and current series facts.
type Store interface {
	ListLibraries(context.Context) ([]database.Library, error)
	GetLibrary(context.Context, int64) (database.Library, error)
	ListSeriesByLibraryLite(context.Context, int64) ([]database.Series, error)
	GetSeries(context.Context, int64) (database.Series, error)
}

// GroupingProvider is the external generation boundary; it never applies collections.
type GroupingProvider = organize.GroupingGenerator

// Sources creates external clients from current configuration and supports controlled sources in tests.
type Sources struct {
	Metadata func(string) metadata.Provider
	Grouping func() GroupingProvider
}

// Service launches complete generation use cases using the existing task engine.
type Service struct {
	runs         *application.Runs
	store        Store
	proposals    *proposal.Service
	groups       *organize.AIGrouping
	sources      Sources
	publishEvent func(string)
}

// New assembles capabilities before any requests or restart work can enter.
func New(runs *application.Runs, store Store, proposals *proposal.Service, groups *organize.AIGrouping, sources Sources, publish func(string)) *Service {
	return &Service{runs, store, proposals, groups, sources, publish}
}
func (c *Service) publish() {
	if c.publishEvent != nil {
		c.publishEvent("refresh")
	}
}

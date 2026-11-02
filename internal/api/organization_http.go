package api

import (
	"context"
	"github.com/go-chi/chi/v5"
	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/proposal"
	"manga-manager/internal/reading"
	"manga-manager/internal/seriesmeta"
	"net/http"
)

// Organization capabilities are consumer contracts, not an application or Store mirror.
type CollectionsCapability interface {
	List(ctx context.Context) ([]database.ListCollectionsWithSeriesCountRow, error)
	Members(ctx context.Context, id int64) ([]database.ListCollectionSeriesRow, error)
	Create(ctx context.Context, name, desc string) (int64, error)
	Update(ctx context.Context, id int64, name, desc string) error
	Delete(ctx context.Context, id int64) error
	AddMembers(ctx context.Context, id int64, ids []int64) (int, error)
	RemoveMember(ctx context.Context, id, sid int64) error
}

type ShelvesCapability interface {
	List(ctx context.Context, libraryID int64) ([]organize.SmartFilter, error)
	Get(ctx context.Context, id int64) (organize.SmartFilter, error)
	Save(ctx context.Context, libraryID int64, req organize.SaveSmartFilter) (organize.SmartFilter, error)
	Update(ctx context.Context, id int64, req organize.SaveSmartFilter) (organize.SmartFilter, error)
	Delete(ctx context.Context, id int64) error
	Members(ctx context.Context, id int64, subject reading.Subject, limit, offset int) (organize.SmartCollectionPage, error)
	Views(ctx context.Context, subject reading.Subject) ([]organize.CollectionView, error)
	PreviewSnapshot(ctx context.Context, id int64, subject reading.Subject, req organize.SmartSnapshotRequest, previewLimit int) (organize.SmartSnapshotPreview, error)
	Snapshot(ctx context.Context, id int64, subject reading.Subject, req organize.SmartSnapshotRequest) (organize.SmartSnapshotResult, error)
}

type ReadingListsCapability interface {
	Page(context.Context, reading.Subject, int64, int64, int64) (organize.ReadingListPage, error)
	List(ctx context.Context) ([]database.ListReadingListsRow, error)
	Create(ctx context.Context, name, description string) (database.ReadingList, error)
	Update(ctx context.Context, id int64, name, description string) (database.ReadingList, error)
	Delete(ctx context.Context, id int64) error
	Add(ctx context.Context, id, seriesID int64, note string) (database.ReadingListItem, error)
	Remove(ctx context.Context, id, itemID int64) error
	Reorder(ctx context.Context, id int64, itemIDs []int64) error
	Items(ctx context.Context, subject reading.Subject, id int64) ([]organize.ReadingListItem, error)
}

type RelationsCapability interface {
	Create(ctx context.Context, source, target int64, kind string) (organize.CreatedRelation, error)
	Update(ctx context.Context, id int64, kind string) error
	Delete(ctx context.Context, id int64) error
	Graph(ctx context.Context, id int64) ([]database.GetConnectedSeriesRelationsRow, error)
	LibraryGraph(ctx context.Context, id int64) ([]database.GetAllSeriesRelationsForLibraryRow, error)
	List(ctx context.Context, seriesID int64) ([]organize.SeriesRelation, error)
	FranchiseState(ctx context.Context) (organize.FranchiseState, error)
	RetryFranchiseRepair(ctx context.Context) error
}

type GroupingCapability interface {
	Review(ctx context.Context, id int64) (organize.AIGroupingReview, error)
	EditCollection(ctx context.Context, reviewID, collectionID int64, req organize.EditAIGroupingCollection) (database.AiGroupingReviewCollection, error)
	ApplyCollection(ctx context.Context, reviewID, collectionID int64) (organize.AIGroupingOutcome, error)
	ApplyReview(ctx context.Context, reviewID int64) (organize.AIGroupingOutcome, error)
	RejectCollection(ctx context.Context, reviewID, collectionID int64) (organize.AIGroupingOutcome, error)
	RejectReview(ctx context.Context, reviewID int64) (organize.AIGroupingOutcome, error)
}

type MetadataEditor interface {
	Save(context.Context, int64, seriesmeta.ManualInput) (seriesediting.Result, error)
}
type SelectionEditor interface {
	Edit(context.Context, []int64, seriesmeta.SelectionInput) (seriesediting.SelectionResult, error)
}
type ProposalCapability interface {
	ListBySeries(context.Context, int64) (proposal.SeriesProposals, error)
	Inbox(context.Context, proposal.InboxQuery) (proposal.InboxPage, error)
	PendingCount(context.Context) (int64, error)
	Apply(context.Context, int64, proposal.ApplyMode) (proposal.ApplyResult, error)
	Reject(context.Context, int64) (proposal.RejectResult, error)
}

// GroupingQueries retains batch hydration and pagination without granting arbitrary SQL.
type GroupingQueries interface {
	CountAIGroupingReviews(context.Context, database.CountAIGroupingReviewsParams) (int64, error)
	ListAIGroupingReviews(context.Context, database.ListAIGroupingReviewsParams) ([]database.ListAIGroupingReviewsRow, error)
	ListAIGroupingReviewCollectionsByReviews(context.Context, []int64) ([]database.AiGroupingReviewCollection, error)
	GetSeriesNamesByIDs(context.Context, []int64) ([]database.GetSeriesNamesByIDsRow, error)
}
type OrganizationSeriesQuery interface {
	GetSeries(context.Context, int64) (database.Series, error)
}
type StaticCollectionQueries interface {
	GetStaticCollectionView(context.Context, int64) (database.GetStaticCollectionViewRow, error)
	ListStaticCollectionSeriesPaged(context.Context, database.ListStaticCollectionSeriesPagedParams) ([]database.ListStaticCollectionSeriesPagedRow, error)
}

// OrganizationDependencies supplies only metadata and organization capabilities.
// Effects are the existing transport leaves; binding creates no runtime or caches.
type OrganizationDependencies struct {
	Collections       CollectionsCapability
	Shelves           ShelvesCapability
	ReadingLists      ReadingListsCapability
	Relations         RelationsCapability
	Proposals         ProposalCapability
	Grouping          GroupingCapability
	Metadata          MetadataEditor
	Selection         SelectionEditor
	GroupingQueries   GroupingQueries
	Series            OrganizationSeriesQuery
	StaticCollections StaticCollectionQueries
	UnmatchedCount    func(context.Context) (int64, error)
	KOReaderEnabled   func() bool
	Publish           func(string)
}

type organizationHTTP struct {
	collections       CollectionsCapability
	shelves           ShelvesCapability
	lists             ReadingListsCapability
	relations         RelationsCapability
	proposals         ProposalCapability
	grouping          GroupingCapability
	metadata          MetadataEditor
	selection         SelectionEditor
	groupingQueries   GroupingQueries
	series            OrganizationSeriesQuery
	staticCollections StaticCollectionQueries
	unmatchedCount    func(context.Context) (int64, error)
	koreaderEnabled   func() bool
	publish           func(string)
}

func newOrganizationHTTP(d OrganizationDependencies) *organizationHTTP {
	return &organizationHTTP{
		collections: d.Collections, shelves: d.Shelves, lists: d.ReadingLists,
		relations: d.Relations, proposals: d.Proposals, grouping: d.Grouping,
		metadata: d.Metadata, selection: d.Selection, groupingQueries: d.GroupingQueries,
		series: d.Series, staticCollections: d.StaticCollections,
		unmatchedCount: d.UnmatchedCount, koreaderEnabled: d.KOReaderEnabled, publish: d.Publish,
	}
}
func (*organizationHTTP) currentReadingSubject(r *http.Request) (reading.Subject, error) {
	if user, ok := userFromContext(r.Context()); ok {
		return reading.UserSubject(user.ID)
	}
	return reading.AnonymousSubject(), nil
}

// setupRoutes mounts this family inside the host authentication gate.
func (c *organizationHTTP) setupRoutes(r chi.Router) {
	c.setupRootRoutes(r)
	r.Route("/series", c.setupSeriesRoutes)
}

func (c *organizationHTTP) setupRootRoutes(r chi.Router) {
	r.Get("/libraries/{libraryId}/franchise", c.getLibraryFranchiseGraph)
	r.Get("/metadata/reviews", c.listMetadataReviewInbox)
	r.Post("/metadata/reviews/bulk-apply", c.bulkApplyMetadataReviews)
	r.Post("/metadata/reviews/bulk-reject", c.bulkRejectMetadataReviews)
	r.Get("/ai-grouping/reviews", c.listAIGroupingReviews)
	r.Get("/reviews/inbox", c.listReviewInbox)
	r.Get("/reviews/inbox/summary", c.getReviewInboxSummary)
	r.Post("/ai-grouping/reviews/{reviewId}/apply", c.applyAIGroupingReview)
	r.Post("/ai-grouping/reviews/{reviewId}/reject", c.rejectAIGroupingReview)
	r.Put("/ai-grouping/reviews/{reviewId}/collections/{collectionId}", c.updateAIGroupingReviewCollection)
	r.Post("/ai-grouping/reviews/{reviewId}/collections/{collectionId}/apply", c.applyAIGroupingReviewCollection)
	r.Post("/ai-grouping/reviews/{reviewId}/collections/{collectionId}/reject", c.rejectAIGroupingReviewCollection)
	r.Post("/metadata/reviews/{reviewId}/apply", c.applyMetadataReview)
	r.Post("/metadata/reviews/{reviewId}/reject", c.rejectMetadataReview)
	r.Post("/system/rebuild-franchises", c.rebuildFranchiseCollectionsHandler)
	// 合集管理
	r.Route("/collections", func(r chi.Router) {
		r.Get("/", c.listCollections)
		r.Post("/", c.createCollection)
		r.Put("/{collectionId}", c.updateCollection)
		r.Delete("/{collectionId}", c.deleteCollection)
		r.Get("/{collectionId}/series", c.getCollectionSeries)
		r.Post("/{collectionId}/series", c.addSeriesToCollection)
		r.Delete("/{collectionId}/series/{seriesId}", c.removeSeriesFromCollection)
	})
	r.Get("/franchise-state", c.getFranchiseState)
	r.Get("/collection-views", c.listCollectionViews)
	r.Get("/collection-views/smart/{filterId}/series", c.getSmartCollectionSeries)
	r.Get("/collection-views/smart/{filterId}/snapshot-preview", c.previewSmartCollectionSnapshot)
	r.Post("/collection-views/smart/{filterId}/snapshot", c.snapshotSmartCollection)

	r.Route("/libraries/{libraryId}/smart-filters", func(r chi.Router) {
		r.Get("/", c.listSmartFilters)
		r.Post("/", c.upsertSmartFilter)
	})
	r.Put("/smart-filters/{filterId}", c.updateSmartFilter)
	r.Delete("/smart-filters/{filterId}", c.deleteSmartFilter)

	// 有序阅读清单
	r.Route("/reading-lists", func(r chi.Router) {
		r.Get("/", c.listReadingLists)
		r.Post("/", c.createReadingList)
		r.Put("/{listId}", c.updateReadingList)
		r.Delete("/{listId}", c.deleteReadingList)
		r.Get("/{listId}/items", c.listReadingListItems)
		r.Post("/{listId}/items", c.addReadingListItem)
		r.Post("/{listId}/items/reorder", c.reorderReadingListItems)
		r.Delete("/{listId}/items/{itemId}", c.removeReadingListItem)
	})

	// 系列关联
	r.Delete("/relations/{relationId}", c.deleteSeriesRelation)
	r.Put("/relations/{relationId}", c.updateSeriesRelation)
}

// setupSeriesRoutes shares the host series subtree with reader and legacy routes.
func (c *organizationHTTP) setupSeriesRoutes(r chi.Router) {
	r.Get("/{seriesId}/metadata-review", c.listSeriesMetadataReview)
	r.Post("/bulk-edit", c.bulkEditSeries)
	r.Put("/info/{seriesId}", c.updateSeriesInfo)
	r.Get("/{seriesId}/relations", c.getSeriesRelations)
	r.Get("/{seriesId}/franchise", c.getSeriesFranchise)
	r.Post("/{seriesId}/relations", c.createSeriesRelation)
}

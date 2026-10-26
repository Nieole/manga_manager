package organize

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"manga-manager/internal/database"
)

type SmartFilter struct {
	ID              int64     `json:"id"`
	LibraryID       int64     `json:"library_id"`
	Name            string    `json:"name"`
	ActiveTag       *string   `json:"activeTag"`
	ActiveAuthor    *string   `json:"activeAuthor"`
	ActiveStatus    *string   `json:"activeStatus"`
	ActiveLetter    *string   `json:"activeLetter"`
	ReadState       *string   `json:"readState"`
	MinRating       *float64  `json:"minRating"`
	MaxRating       *float64  `json:"maxRating"`
	MinProgress     *float64  `json:"minProgress"`
	MaxProgress     *float64  `json:"maxProgress"`
	AddedWithinDays *int      `json:"addedWithinDays"`
	SortByField     string    `json:"sortByField"`
	SortDir         string    `json:"sortDir"`
	PageSize        int       `json:"pageSize"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type SaveSmartFilter struct {
	Name            string   `json:"name"`
	ActiveTag       *string  `json:"activeTag"`
	ActiveAuthor    *string  `json:"activeAuthor"`
	ActiveStatus    *string  `json:"activeStatus"`
	ActiveLetter    *string  `json:"activeLetter"`
	ReadState       *string  `json:"readState"`
	MinRating       *float64 `json:"minRating"`
	MaxRating       *float64 `json:"maxRating"`
	MinProgress     *float64 `json:"minProgress"`
	MaxProgress     *float64 `json:"maxProgress"`
	AddedWithinDays *int     `json:"addedWithinDays"`
	SortByField     string   `json:"sortByField"`
	SortDir         string   `json:"sortDir"`
	PageSize        int      `json:"pageSize"`
}

type CollectionView struct {
	ViewID         string       `json:"view_id"`
	NumericID      int64        `json:"numeric_id"`
	Kind           string       `json:"kind"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	LibraryID      *int64       `json:"library_id,omitempty"`
	LibraryName    string       `json:"library_name,omitempty"`
	SeriesCount    int          `json:"series_count"`
	SourceType     string       `json:"source_type"`
	SourceReviewID *int64       `json:"source_review_id,omitempty"`
	SortOrder      int          `json:"sort_order"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	SmartFilter    *SmartFilter `json:"smart_filter,omitempty"`
}

type SmartCollectionStore interface {
	ExecTx(context.Context, func(*database.Queries) error) error
	GetLibrary(context.Context, int64) (database.Library, error)
	GetSmartFilterByID(context.Context, int64) (database.SmartFilter, error)
	ListSmartFilters(context.Context) ([]database.SmartFilter, error)
	ListSmartFiltersByLibrary(context.Context, int64) ([]database.SmartFilter, error)
	UpsertSmartFilter(context.Context, database.UpsertSmartFilterParams) (database.SmartFilter, error)
	UpdateSmartFilter(context.Context, database.UpdateSmartFilterParams) (database.SmartFilter, error)
	DeleteSmartFilter(context.Context, int64) (int64, error)
	ListCollectionViews(context.Context) ([]database.ListCollectionViewsRow, error)
	SearchSmartCollectionSeries(context.Context, database.SmartCollectionFilter, int, int) ([]database.SearchSeriesPagedRow, int, error)
	CountSmartCollectionSeries(context.Context, database.SmartCollectionFilter) (int, error)
	CollectionNameExists(context.Context, string) (int64, error)
}

var ErrSmartFilterNameTaken = errors.New("A smart filter with this name already exists in this library")

type SmartCollections struct{ store SmartCollectionStore }

func NewSmartCollections(store SmartCollectionStore) *SmartCollections {
	return &SmartCollections{store: store}
}

// InvalidSmartFilter distinguishes invalid saved conditions from persistence failures.
type InvalidSmartFilter struct{ Err error }

func (e *InvalidSmartFilter) Error() string { return e.Err.Error() }
func (e *InvalidSmartFilter) Unwrap() error { return e.Err }
func (s *SmartCollections) List(ctx context.Context, libraryID int64) ([]SmartFilter, error) {
	if _, err := s.store.GetLibrary(ctx, libraryID); err != nil {
		return nil, err
	}
	rows, err := s.store.ListSmartFiltersByLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	result := make([]SmartFilter, 0, len(rows))
	for _, row := range rows {
		result = append(result, smartFilterFromDB(row))
	}
	return result, nil
}
func (s *SmartCollections) Get(ctx context.Context, id int64) (SmartFilter, error) {
	row, err := s.store.GetSmartFilterByID(ctx, id)
	return smartFilterFromDB(row), err
}
func (s *SmartCollections) Save(ctx context.Context, libraryID int64, req SaveSmartFilter) (SmartFilter, error) {
	if _, err := s.store.GetLibrary(ctx, libraryID); err != nil {
		return SmartFilter{}, err
	}
	normalized, err := normalizeSmartFilter(req)
	if err != nil {
		return SmartFilter{}, &InvalidSmartFilter{Err: err}
	}
	row, err := s.store.UpsertSmartFilter(ctx, saveParams(libraryID, normalized))
	return smartFilterFromDB(row), err
}
func saveParams(libraryID int64, req SaveSmartFilter) database.UpsertSmartFilterParams {
	return database.UpsertSmartFilterParams{
		LibraryID:       libraryID,
		Name:            req.Name,
		ActiveTag:       nullStringFromPointer(req.ActiveTag),
		ActiveAuthor:    nullStringFromPointer(req.ActiveAuthor),
		ActiveStatus:    nullStringFromPointer(req.ActiveStatus),
		ActiveLetter:    nullStringFromPointer(req.ActiveLetter),
		ReadState:       nullStringFromPointer(req.ReadState),
		MinRating:       nullFloatFromPointer(req.MinRating),
		MaxRating:       nullFloatFromPointer(req.MaxRating),
		MinProgress:     nullFloatFromPointer(req.MinProgress),
		MaxProgress:     nullFloatFromPointer(req.MaxProgress),
		AddedWithinDays: nullIntFromPointer(req.AddedWithinDays),
		SortByField:     req.SortByField,
		SortDir:         req.SortDir,
		PageSize:        int64(req.PageSize),
	}
}
func (s *SmartCollections) Update(ctx context.Context, id int64, req SaveSmartFilter) (SmartFilter, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return SmartFilter{}, err
	}
	normalized, err := normalizeSmartFilter(req)
	if err != nil {
		return SmartFilter{}, &InvalidSmartFilter{Err: err}
	}
	p := saveParams(0, normalized)
	row, err := s.store.UpdateSmartFilter(ctx, database.UpdateSmartFilterParams{
		ID: id, Name: p.Name,
		ActiveTag: p.ActiveTag, ActiveAuthor: p.ActiveAuthor,
		ActiveStatus: p.ActiveStatus, ActiveLetter: p.ActiveLetter,
		ReadState: p.ReadState,
		MinRating: p.MinRating, MaxRating: p.MaxRating,
		MinProgress: p.MinProgress, MaxProgress: p.MaxProgress,
		AddedWithinDays: p.AddedWithinDays,
		SortByField:     p.SortByField, SortDir: p.SortDir, PageSize: p.PageSize,
	})
	// SQLITE_CONSTRAINT_UNIQUE is the library/name ownership conflict.
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code() == 2067 {
		err = ErrSmartFilterNameTaken
	}
	return smartFilterFromDB(row), err
}
func (s *SmartCollections) Delete(ctx context.Context, id int64) error {
	n, err := s.store.DeleteSmartFilter(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type SmartCollectionPage struct {
	Items                []database.SearchSeriesPagedRow
	Total, Limit, Offset int
	Filter               SmartFilter
}

func (s *SmartCollections) Members(ctx context.Context, id, userID int64, limit, offset int) (SmartCollectionPage, error) {
	filter, err := s.Get(ctx, id)
	if err != nil {
		return SmartCollectionPage{}, err
	}
	if limit <= 0 {
		limit = SmartCollectionPageLimit(filter.PageSize)
	}
	if offset < 0 {
		offset = 0
	}
	rows, total, err := s.store.SearchSmartCollectionSeries(ctx, smartCollectionFilterFrom(filter, userID), limit, offset)
	return SmartCollectionPage{Items: rows, Total: total, Limit: limit, Offset: offset, Filter: filter}, err
}

const DefaultSmartCollectionPageLimit = 30
const MaxSmartCollectionPageLimit = 100

func SmartCollectionPageLimit(want int) int {
	if want <= 0 {
		return DefaultSmartCollectionPageLimit
	}
	if want > MaxSmartCollectionPageLimit {
		return MaxSmartCollectionPageLimit
	}
	return want
}
func (s *SmartCollections) Views(ctx context.Context, userID int64) ([]CollectionView, error) {
	rows, err := s.store.ListCollectionViews(ctx)
	if err != nil {
		return nil, err
	}

	// 只在确有智能书架时才去取筛选定义。
	smartFilters := make(map[int64]database.SmartFilter)
	for _, row := range rows {
		if row.Kind == "smart" {
			filters, ferr := s.store.ListSmartFilters(ctx)
			if ferr != nil {
				return nil, ferr
			}
			for _, f := range filters {
				smartFilters[f.ID] = f
			}
			break
		}
	}

	items := make([]CollectionView, 0, len(rows))
	for _, row := range rows {
		viewID, _ := row.ViewID.(string)
		item := CollectionView{
			ViewID:      viewID,
			NumericID:   row.ID,
			Kind:        row.Kind,
			Name:        row.Name,
			Description: row.Description,
			LibraryName: row.LibraryName,
			SeriesCount: int(row.SeriesCount),
			SourceType:  row.SourceType,
			SortOrder:   int(row.SortOrder),
			CreatedAt:   row.CreatedAt.Time,
			UpdatedAt:   row.UpdatedAt.Time,
		}
		if row.Kind == "smart" {
			if raw, ok := smartFilters[row.ID]; ok {
				filter := smartFilterFromDB(raw)
				count, cerr := s.store.CountSmartCollectionSeries(ctx, smartCollectionFilterFrom(filter, userID))
				if cerr != nil {
					return nil, cerr
				}
				item.SeriesCount = count
				// 左栏靠这份定义渲染筛选芯片；description 里那串拼接文本不是给界面读的。
				item.SmartFilter = &filter
			}
		}
		if row.LibraryID.Valid {
			value := row.LibraryID.Int64
			item.LibraryID = &value
		}
		if row.SourceReviewID.Valid {
			value := row.SourceReviewID.Int64
			item.SourceReviewID = &value
		}
		items = append(items, item)
	}
	return items, nil
}

func smartCollectionFilterFrom(filter SmartFilter, userID int64) database.SmartCollectionFilter {
	return database.SmartCollectionFilter{
		UserID:          userID,
		LibraryID:       filter.LibraryID,
		ActiveLetter:    stringValue(filter.ActiveLetter),
		ActiveStatus:    stringValue(filter.ActiveStatus),
		ActiveTag:       stringValue(filter.ActiveTag),
		ActiveAuthor:    stringValue(filter.ActiveAuthor),
		MinRating:       filter.MinRating,
		MaxRating:       filter.MaxRating,
		MinProgress:     filter.MinProgress,
		MaxProgress:     filter.MaxProgress,
		AddedWithinDays: filter.AddedWithinDays,
		ReadState:       stringValue(filter.ReadState),
		SortByField:     filter.SortByField,
		SortDir:         filter.SortDir,
	}
}

func smartFilterFromDB(row database.SmartFilter) SmartFilter {
	return SmartFilter{
		ID:              row.ID,
		LibraryID:       row.LibraryID,
		Name:            row.Name,
		ActiveTag:       nullStringPointer(row.ActiveTag),
		ActiveAuthor:    nullStringPointer(row.ActiveAuthor),
		ActiveStatus:    nullStringPointer(row.ActiveStatus),
		ActiveLetter:    nullStringPointer(row.ActiveLetter),
		ReadState:       nullStringPointer(row.ReadState),
		MinRating:       nullFloatPointer(row.MinRating),
		MaxRating:       nullFloatPointer(row.MaxRating),
		MinProgress:     nullFloatPointer(row.MinProgress),
		MaxProgress:     nullFloatPointer(row.MaxProgress),
		AddedWithinDays: nullIntPointer(row.AddedWithinDays),
		SortByField:     row.SortByField,
		SortDir:         row.SortDir,
		PageSize:        int(row.PageSize),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func normalizeSmartFilter(req SaveSmartFilter) (SaveSmartFilter, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return req, errors.New("Name is required")
	}
	if len([]rune(req.Name)) > 80 {
		return req, errors.New("Name is too long")
	}

	req.ActiveTag = cleanOptionalString(req.ActiveTag)
	req.ActiveAuthor = cleanOptionalString(req.ActiveAuthor)
	req.ActiveStatus = cleanOptionalString(req.ActiveStatus)
	req.ActiveLetter = cleanOptionalString(req.ActiveLetter)
	req.ReadState = cleanOptionalString(req.ReadState)
	if req.ReadState != nil {
		switch *req.ReadState {
		case "unread", "reading", "completed":
		default:
			return req, errors.New("Invalid read state")
		}
	}
	if err := validateOptionalFloatRange(req.MinRating, req.MaxRating, 0, 10, "rating"); err != nil {
		return req, err
	}
	if err := validateOptionalFloatRange(req.MinProgress, req.MaxProgress, 0, 100, "progress"); err != nil {
		return req, err
	}
	if req.AddedWithinDays != nil {
		if *req.AddedWithinDays <= 0 || *req.AddedWithinDays > 3650 {
			return req, errors.New("Invalid added window")
		}
	}

	if req.SortByField == "" {
		req.SortByField = "name"
	}
	switch req.SortByField {
	case "name", "created", "updated", "rating", "volumes", "books", "pages", "read", "favorite":
	default:
		return req, errors.New("Invalid sort field")
	}

	if req.SortDir == "" {
		req.SortDir = "asc"
	}
	if req.SortDir != "asc" && req.SortDir != "desc" {
		return req, errors.New("Invalid sort direction")
	}

	switch req.PageSize {
	case 30, 50, 100:
		return req, nil
	case 0:
		req.PageSize = 30
		return req, nil
	default:
		return req, errors.New("Invalid page size")
	}
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func validateOptionalFloatRange(minValue, maxValue *float64, minAllowed, maxAllowed float64, label string) error {
	if minValue != nil && (*minValue < minAllowed || *minValue > maxAllowed) {
		return errors.New("Invalid " + label + " range")
	}
	if maxValue != nil && (*maxValue < minAllowed || *maxValue > maxAllowed) {
		return errors.New("Invalid " + label + " range")
	}
	if minValue != nil && maxValue != nil && *minValue > *maxValue {
		return errors.New("Invalid " + label + " range")
	}
	return nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}

func nullFloatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

func nullStringFromPointer(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullFloatFromPointer(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func nullIntFromPointer(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

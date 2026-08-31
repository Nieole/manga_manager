package database

type ExternalLibraryBookRow struct {
	BookID     int64  `json:"book_id"`
	SeriesID   int64  `json:"series_id"`
	SeriesName string `json:"series_name"`
	Path       string `json:"path"`
}

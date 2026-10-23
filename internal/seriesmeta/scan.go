package seriesmeta

import (
	"database/sql"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

// ScanMetadata contains only metadata columns for the scanner's identity upsert.
// It cannot supply library ownership, paths, counts, books or covers.
type ScanMetadata struct {
	Title        sql.NullString
	Summary      sql.NullString
	Publisher    sql.NullString
	Status       sql.NullString
	Rating       sql.NullFloat64
	Language     sql.NullString
	LockedFields sql.NullString
	NameInitial  string
}

// ScanLocks retains the scanner's lock snapshot without reloading the series.
type ScanLocks struct{ fields map[string]bool }

// ParseScanLocks captures the locks at the scanner's existing cache boundary.
func ParseScanLocks(value sql.NullString) ScanLocks {
	locks := ScanLocks{fields: make(map[string]bool)}
	if value.Valid {
		for _, field := range strings.Split(value.String, ",") {
			if field = strings.TrimSpace(field); field != "" {
				locks.fields[field] = true
			}
		}
	}
	return locks
}

// PrepareScanInitial supplies creation defaults without creating a series or transaction.
func PrepareScanInitial(directoryName string, info *parser.ComicInfo) ScanMetadata {
	result := scanArchiveMetadata(info)
	result.Title = sql.NullString{String: directoryName, Valid: true}
	result.LockedFields = sql.NullString{String: "title", Valid: true}
	result.NameInitial = database.SeriesInitial(directoryName, directoryName)
	return result
}

// PrepareScanUpdate uses the caller's cached row and locks. The scanner calls it
// only when ComicInfo exists; unlocked empty scalars clear and status stays empty.
func PrepareScanUpdate(current database.Series, locks ScanLocks, directoryName string, info *parser.ComicInfo) ScanMetadata {
	result := scanArchiveMetadata(info)
	result.Title = current.Title
	if !locks.fields["title"] && strings.TrimSpace(result.Title.String) == "" {
		result.Title = sql.NullString{String: directoryName, Valid: true}
	}
	for _, field := range []struct {
		name     string
		target   *sql.NullString
		previous sql.NullString
	}{
		{"summary", &result.Summary, current.Summary},
		{"publisher", &result.Publisher, current.Publisher},
		{"status", &result.Status, current.Status},
		{"language", &result.Language, current.Language},
	} {
		if locks.fields[field.name] {
			*field.target = field.previous
		}
	}
	if locks.fields["rating"] {
		result.Rating = current.Rating
	}
	result.LockedFields = current.LockedFields
	result.NameInitial = database.SeriesInitialFromNullTitle(result.Title, directoryName)
	return result
}

func scanArchiveMetadata(info *parser.ComicInfo) ScanMetadata {
	var result ScanMetadata
	if info == nil {
		return result
	}
	result.Summary = sql.NullString{String: info.Summary, Valid: info.Summary != ""}
	result.Publisher = sql.NullString{String: info.Publisher, Valid: info.Publisher != ""}
	result.Language = sql.NullString{String: info.LanguageISO, Valid: info.LanguageISO != ""}
	if info.CommunityRating > 0 {
		result.Rating = sql.NullFloat64{Float64: float64(info.CommunityRating), Valid: true}
	}
	return result
}

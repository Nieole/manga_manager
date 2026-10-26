package seriesmeta

import (
	"database/sql"
	"manga-manager/internal/database"
	"manga-manager/internal/parser"
	"strconv"
	"strings"
)

// BuildComicInfoForBook projects known book and series facts, leaving unknown archive fields unset.
func BuildComicInfoForBook(book database.Book, series database.Series, tags []database.Tag, authors []database.Author) parser.ComicInfo {
	info := parser.ComicInfo{
		Title:       firstNonEmpty(nullString(book.Title), book.Name),
		Series:      firstNonEmpty(nullString(series.Title), series.Name),
		Summary:     firstNonEmpty(nullString(book.Summary), nullString(series.Summary)),
		Number:      firstNonEmpty(nullString(book.Number), formatNullableFloat(book.SortNumber)),
		Volume:      book.Volume,
		Publisher:   nullString(series.Publisher),
		Genre:       joinTagNames(tags),
		LanguageISO: nullString(series.Language),
		PageCount:   parser.LenientInt(book.PageCount),
	}

	if series.Rating.Valid {
		info.CommunityRating = parser.LenientFloat(series.Rating.Float64)
	}

	for _, author := range authors {
		switch strings.ToLower(strings.TrimSpace(author.Role)) {
		case "writer", "author", "story":
			info.Writer = appendCommaValue(info.Writer, author.Name)
		case "penciller", "artist", "illustrator":
			info.Penciller = appendCommaValue(info.Penciller, author.Name)
		case "letterer":
			info.Letterer = appendCommaValue(info.Letterer, author.Name)
		case "translator":
			info.Translator = appendCommaValue(info.Translator, author.Name)
		}
	}

	return info
}

func nullString(value sql.NullString) string {
	if value.Valid {
		return strings.TrimSpace(value.String)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatNullableFloat(value sql.NullFloat64) string {
	if !value.Valid {
		return ""
	}
	return strconv.FormatFloat(value.Float64, 'f', -1, 64)
}

func joinTagNames(tags []database.Tag) string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		if name := strings.TrimSpace(tag.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func appendCommaValue(current, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	return current + ", " + next
}

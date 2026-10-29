package seriescontext

import (
	"database/sql"
	"manga-manager/internal/booksort"
	"manga-manager/internal/reading"
	"sort"
	"strings"
	"time"
)

type Volume struct {
	Name        string
	BookCount   int
	TotalPages  int64
	ReadPages   int64
	CoverBookID int64
	CoverPath   sql.NullString
	UpdatedAt   time.Time
	Books       []reading.BookView
}

func Volumes(books []reading.BookView, includeBooks bool) []Volume {
	type volumeAccumulator struct {
		summary Volume
		books   []reading.BookView
	}
	volumeMap := make(map[string]*volumeAccumulator)
	for _, view := range books {
		book := view.Facts
		volumeName := strings.TrimSpace(book.Volume)
		if volumeName == "" {
			continue
		}
		acc, ok := volumeMap[volumeName]
		if !ok {
			acc = &volumeAccumulator{summary: Volume{Name: volumeName}}
			volumeMap[volumeName] = acc
		}
		acc.summary.BookCount++
		acc.summary.TotalPages += book.PageCount
		acc.summary.ReadPages += view.Inspect().ReadPages
		if acc.summary.CoverBookID == 0 && book.CoverPath.Valid && strings.TrimSpace(book.CoverPath.String) != "" {
			acc.summary.CoverBookID = book.ID
			acc.summary.CoverPath = book.CoverPath
			acc.summary.UpdatedAt = book.UpdatedAt
		}
		if includeBooks {
			acc.books = append(acc.books, view)
		}
	}
	items := make([]Volume, 0, len(volumeMap))
	for _, acc := range volumeMap {
		if includeBooks {
			acc.summary.Books = acc.books
		}
		items = append(items, acc.summary)
	}
	sort.Slice(items, func(i, j int) bool {
		return booksort.CompareLabels(items[i].Name, items[j].Name) < 0
	})
	return items
}

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manga-manager/internal/database"

	_ "modernc.org/sqlite"
)

type seedConfig struct {
	dbPath         string
	libraries      int
	seriesPerLib   int
	booksPerSeries int
	minPages       int
	maxPages       int
	withProgress   bool
}

func main() {
	cfg := seedConfig{}
	flag.StringVar(&cfg.dbPath, "db", "data/sample.db", "SQLite database path to create or overwrite")
	flag.IntVar(&cfg.libraries, "libraries", 2, "number of libraries")
	flag.IntVar(&cfg.seriesPerLib, "series", 1000, "series per library")
	flag.IntVar(&cfg.booksPerSeries, "books", 3, "books per series")
	flag.IntVar(&cfg.minPages, "min-pages", 80, "minimum pages per book")
	flag.IntVar(&cfg.maxPages, "max-pages", 220, "maximum pages per book")
	flag.BoolVar(&cfg.withProgress, "progress", true, "seed deterministic reading progress")
	flag.Parse()

	if err := validateConfig(cfg); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.dbPath), 0o755); err != nil {
		log.Fatalf("create database directory: %v", err)
	}
	if err := os.Remove(cfg.dbPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("remove existing database: %v", err)
	}
	if err := database.Migrate(cfg.dbPath); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	db, err := sql.Open("sqlite", cfg.dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	started := time.Now()
	if err := seed(context.Background(), db, cfg); err != nil {
		log.Fatal(err)
	}

	totalSeries := cfg.libraries * cfg.seriesPerLib
	totalBooks := totalSeries * cfg.booksPerSeries
	fmt.Printf("sample database created: %s\n", cfg.dbPath)
	fmt.Printf("libraries=%d series=%d books=%d elapsed=%s\n", cfg.libraries, totalSeries, totalBooks, time.Since(started).Round(time.Millisecond))
}

func validateConfig(cfg seedConfig) error {
	if strings.TrimSpace(cfg.dbPath) == "" {
		return fmt.Errorf("db path is required")
	}
	if cfg.libraries <= 0 || cfg.seriesPerLib <= 0 || cfg.booksPerSeries <= 0 {
		return fmt.Errorf("libraries, series, and books must be positive")
	}
	if cfg.minPages <= 0 || cfg.maxPages < cfg.minPages {
		return fmt.Errorf("page range must be positive and max-pages must be >= min-pages")
	}
	return nil
}

func seed(ctx context.Context, db *sql.DB, cfg seedConfig) error {
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode = WAL; PRAGMA synchronous = NORMAL; PRAGMA temp_store = MEMORY;`); err != nil {
		return fmt.Errorf("configure sqlite pragmas: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	insertLibrary, err := tx.PrepareContext(ctx, `INSERT INTO libraries (name, path, scan_mode, koreader_sync_enabled, scan_interval, scan_formats) VALUES (?, ?, 'manual', true, 60, 'zip,cbz,rar,cbr')`)
	if err != nil {
		return err
	}
	defer insertLibrary.Close()

	insertSeries, err := tx.PrepareContext(ctx, `INSERT INTO series (library_id, name, title, summary, publisher, status, rating, language, name_initial, path, is_favorite, volume_count, book_count, total_pages, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertSeries.Close()

	insertBook, err := tx.PrepareContext(ctx, `INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, volume, title, number, sort_number, page_count, last_read_page, last_read_at, file_hash, quick_hash, path_fingerprint, path_fingerprint_no_ext, filename_fingerprint, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertBook.Close()

	rng := rand.New(rand.NewSource(20260514))
	now := time.Now().UTC()
	statuses := []string{"ONGOING", "COMPLETED", "HIATUS", ""}
	languages := []string{"ja", "zh", "en", ""}
	publishers := []string{"Kodansha", "Shueisha", "Kadokawa", "Local Scan", ""}

	for libIdx := 1; libIdx <= cfg.libraries; libIdx++ {
		libPath := filepath.ToSlash(filepath.Join("Z:/manga-sample", fmt.Sprintf("library-%02d", libIdx)))
		res, err := insertLibrary.ExecContext(ctx, fmt.Sprintf("Sample Library %02d", libIdx), libPath)
		if err != nil {
			return fmt.Errorf("insert library %d: %w", libIdx, err)
		}
		libraryID, err := res.LastInsertId()
		if err != nil {
			return err
		}

		for seriesIdx := 1; seriesIdx <= cfg.seriesPerLib; seriesIdx++ {
			seriesName := fmt.Sprintf("Series %02d-%05d", libIdx, seriesIdx)
			seriesPath := fmt.Sprintf("%s/%s", libPath, slug(seriesName))
			createdAt := now.Add(-time.Duration(rng.Intn(7200)) * time.Hour)
			totalPages := 0
			pageCounts := make([]int, cfg.booksPerSeries)
			for i := range pageCounts {
				pageCounts[i] = cfg.minPages + rng.Intn(cfg.maxPages-cfg.minPages+1)
				totalPages += pageCounts[i]
			}

			res, err = insertSeries.ExecContext(
				ctx,
				libraryID,
				seriesName,
				fmt.Sprintf("Sample Title %05d", seriesIdx),
				fmt.Sprintf("Synthetic benchmark series %d in library %d.", seriesIdx, libIdx),
				publishers[rng.Intn(len(publishers))],
				statuses[rng.Intn(len(statuses))],
				float64(rng.Intn(51))/10,
				languages[rng.Intn(len(languages))],
				fmt.Sprintf("%c", 'A'+rune(seriesIdx%26)),
				seriesPath,
				seriesIdx%17 == 0,
				cfg.booksPerSeries,
				cfg.booksPerSeries,
				totalPages,
				createdAt,
				createdAt.Add(time.Duration(rng.Intn(240))*time.Hour),
			)
			if err != nil {
				return fmt.Errorf("insert series %d/%d: %w", libIdx, seriesIdx, err)
			}
			seriesID, err := res.LastInsertId()
			if err != nil {
				return err
			}

			for bookIdx := 1; bookIdx <= cfg.booksPerSeries; bookIdx++ {
				pageCount := pageCounts[bookIdx-1]
				bookName := fmt.Sprintf("%s Vol.%03d.cbz", seriesName, bookIdx)
				bookPath := fmt.Sprintf("%s/%s", seriesPath, bookName)
				modifiedAt := createdAt.Add(time.Duration(bookIdx) * time.Hour)
				lastPage := sql.NullInt64{}
				lastReadAt := sql.NullTime{}
				if cfg.withProgress && (seriesIdx+bookIdx)%5 == 0 {
					lastPage = sql.NullInt64{Int64: int64(1 + rng.Intn(pageCount)), Valid: true}
					lastReadAt = sql.NullTime{Time: now.Add(-time.Duration(rng.Intn(60*24)) * time.Minute), Valid: true}
				}

				if _, err := insertBook.ExecContext(
					ctx,
					seriesID,
					libraryID,
					bookName,
					bookPath,
					int64(pageCount*(700_000+rng.Intn(300_000))),
					modifiedAt,
					fmt.Sprintf("%03d", bookIdx),
					fmt.Sprintf("Volume %03d", bookIdx),
					fmt.Sprintf("%d", bookIdx),
					float64(bookIdx),
					pageCount,
					lastPage,
					lastReadAt,
					fmt.Sprintf("full-%02d-%05d-%03d", libIdx, seriesIdx, bookIdx),
					fmt.Sprintf("quick-%02d-%05d-%03d", libIdx, seriesIdx, bookIdx),
					fmt.Sprintf("fp-%02d-%05d-%03d", libIdx, seriesIdx, bookIdx),
					fmt.Sprintf("fpnoext-%02d-%05d-%03d", libIdx, seriesIdx, bookIdx),
					fmt.Sprintf("file-%02d-%05d-%03d", libIdx, seriesIdx, bookIdx),
					modifiedAt,
					modifiedAt,
				); err != nil {
					return fmt.Errorf("insert book %d/%d/%d: %w", libIdx, seriesIdx, bookIdx, err)
				}
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("analyze sample database: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sample database: %w", err)
	}
	return nil
}

func slug(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, ".", "")
	return value
}

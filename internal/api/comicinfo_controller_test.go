package api

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

func TestExportBookComicInfo(t *testing.T) {
	controller, _, book := seedComicInfoFixture(t)

	rec := httptest.NewRecorder()
	controller.exportBookComicInfo(rec, requestWithRouteParam(http.MethodGet, "/api/books/1/comicinfo.xml", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if contentDisposition := rec.Header().Get("Content-Disposition"); !strings.Contains(contentDisposition, "Vol-01--ComicInfo.xml") {
		t.Fatalf("expected sanitized download filename, got %q", contentDisposition)
	}

	var info parser.ComicInfo
	if err := xml.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal exported comicinfo failed: %v", err)
	}
	if info.Title != "Book Title" || info.Series != "Display Series" || info.Summary != "Book summary" {
		t.Fatalf("unexpected title/series/summary: %+v", info)
	}
	if info.Number != "1" || info.Volume != "1" || info.PageCount != 188 {
		t.Fatalf("unexpected book fields: %+v", info)
	}
	// Count 是 ComicRack 的「本系列共几卷」，本项目只知道资料库里收了几卷，两者不是一回事。
	// 拿库里的卷数冒充它会把用户原本正确的数字改错，因此这个字段一个字都不写。
	if strings.Contains(rec.Body.String(), "<Count>") {
		t.Fatalf("不该导出 Count: %s", rec.Body.String())
	}
	if info.Publisher != "Publisher" || info.Genre != "冒险" || info.Writer != "Writer A" || info.LanguageISO != "zh" {
		t.Fatalf("unexpected metadata fields: %+v", info)
	}
	if info.CommunityRating != 4.5 {
		t.Fatalf("expected rating 4.5, got %v", info.CommunityRating)
	}
}

func TestExportSeriesComicInfoArchive(t *testing.T) {
	controller, series, _ := seedComicInfoFixture(t)

	rec := httptest.NewRecorder()
	controller.exportSeriesComicInfoArchive(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/comicinfo.zip", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	// 文件名必须同时给 ASCII 兜底与 RFC 5987 的 UTF-8 版本，否则中文标题浏览器保存下来是乱码。
	// UTF-8 版是百分号编码的，故按编码后的形式断言。
	contentDisposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, `filename="series-`) {
		t.Fatalf("expected an ASCII filename fallback, got %q", contentDisposition)
	}
	if !strings.Contains(contentDisposition, "filename*=UTF-8''"+url.PathEscape("Display Series-ComicInfo.zip")) {
		t.Fatalf("expected an RFC 5987 UTF-8 filename, got %q", contentDisposition)
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("open zip failed: %v", err)
	}
	if len(reader.File) != 2 {
		t.Fatalf("expected 2 ComicInfo entries, got %d", len(reader.File))
	}
	if reader.File[0].Name != "Vol-01-/ComicInfo.xml" || reader.File[1].Name != "Vol-01--2/ComicInfo.xml" {
		t.Fatalf("unexpected archive entry names: %q %q", reader.File[0].Name, reader.File[1].Name)
	}

	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("open zip entry failed: %v", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read zip entry failed: %v", err)
	}

	var info parser.ComicInfo
	if err := xml.Unmarshal(payload, &info); err != nil {
		t.Fatalf("unmarshal zipped ComicInfo failed: %v", err)
	}
	if info.Series != "Display Series" || info.Writer != "Writer A" {
		t.Fatalf("unexpected zipped ComicInfo: %+v", info)
	}
	if strings.Contains(string(payload), "<Count>") {
		t.Fatalf("不该导出 Count: %s", payload)
	}
}

func TestExportBookComicInfoRejectsInvalidBookID(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.exportBookComicInfo(rec, requestWithRouteParam(http.MethodGet, "/api/books/bad/comicinfo.xml", nil, "bookId", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestExportSeriesComicInfoArchiveRejectsInvalidSeriesID(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.exportSeriesComicInfoArchive(rec, requestWithRouteParam(http.MethodGet, "/api/series/bad/comicinfo.zip", nil, "seriesId", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// taggedArchiveComicInfo 是 ComicTagger 打过标的真实形状：本项目建模的字段之外，
// 还带着出版日期、其余署名、阅读方向与分级——这些本项目既不显示也不给编辑。
const taggedArchiveComicInfo = `<?xml version="1.0" encoding="utf-8"?>
<ComicInfo>
  <Title>旧标题</Title>
  <Count>20</Count>
  <Year>1995</Year>
  <Month>2</Month>
  <Inker>某上墨</Inker>
  <CoverArtist>某封面</CoverArtist>
  <Web>https://example.com/x</Web>
  <Manga>YesAndRightToLeft</Manga>
  <AgeRating>Mature 17+</AgeRating>
  <Notes>Tagged with ComicTagger</Notes>
</ComicInfo>`

// TestWriteBookComicInfoMergesInsteadOfReplacing 守的是回写端到端不丢字段：从库里聚合、
// 到落进用户原始归档的整条链路上，本项目不建模的字段必须一个不少地活下来。
// 破了就是用户换个软件反而丢标——而回写是原子替换、不留备份。
func TestWriteBookComicInfoMergesInsteadOfReplacing(t *testing.T) {
	controller, _, book := seedComicInfoFixture(t)

	if err := os.MkdirAll(filepath.Dir(book.Path), 0o755); err != nil {
		t.Fatalf("造系列目录失败: %v", err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range map[string]string{"001.jpg": "page", "ComicInfo.xml": taggedArchiveComicInfo} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("造归档条目失败: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("写归档条目失败: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("收尾归档失败: %v", err)
	}
	if err := os.WriteFile(book.Path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("落盘归档失败: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.writeBookComicInfo(rec, requestWithRouteParam(http.MethodPost, "/api/books/1/comicinfo", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	reader, err := zip.OpenReader(book.Path)
	if err != nil {
		t.Fatalf("重开归档失败: %v", err)
	}
	defer func() { _ = reader.Close() }()
	var written string
	for _, entry := range reader.File {
		if !strings.EqualFold(entry.Name, "ComicInfo.xml") {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			t.Fatalf("打开 ComicInfo.xml 失败: %v", err)
		}
		payload, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("读 ComicInfo.xml 失败: %v", err)
		}
		written = string(payload)
	}

	for _, want := range []string{
		"<Year>1995</Year>", "<Month>2</Month>", "<Inker>某上墨</Inker>",
		"<CoverArtist>某封面</CoverArtist>", "<Web>https://example.com/x</Web>",
		"<Manga>YesAndRightToLeft</Manga>", "<AgeRating>Mature 17+</AgeRating>",
		"<Notes>Tagged with ComicTagger</Notes>",
		// 本系列共几卷是归档自己的数据，本项目改不动它。
		"<Count>20</Count>",
		// 本项目管的字段按库里的值更新。
		"<Title>Book Title</Title>", "<Series>Display Series</Series>", "<Writer>Writer A</Writer>",
	} {
		if !strings.Contains(written, want) {
			t.Errorf("回写后应含 %s，实得:\n%s", want, written)
		}
	}
}

func seedComicInfoFixture(t *testing.T) (*Controller, database.Series, database.Book) {
	t.Helper()

	controller, store, _, tempDir := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name:                "Library",
		Path:                filepath.Join(tempDir, "Library"),
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "cbz",
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Raw Series",
		Path:        filepath.Join(tempDir, "Library", "Raw Series"),
		NameInitial: database.SeriesInitial("", "Raw Series"),
	})
	if err != nil {
		t.Fatalf("CreateSeries failed: %v", err)
	}
	series, err = store.UpdateSeriesMetadata(ctx, database.UpdateSeriesMetadataParams{
		Title:       sql.NullString{String: "Display Series", Valid: true},
		Summary:     sql.NullString{String: "Series summary", Valid: true},
		Publisher:   sql.NullString{String: "Publisher", Valid: true},
		Rating:      sql.NullFloat64{Float64: 4.5, Valid: true},
		Language:    sql.NullString{String: "zh", Valid: true},
		NameInitial: database.SeriesInitial("Display Series", "Raw Series"),
		ID:          series.ID,
	})
	if err != nil {
		t.Fatalf("UpdateSeriesMetadata failed: %v", err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           `Vol:01?.cbz`,
		Path:           filepath.Join(tempDir, "Library", "Raw Series", "Vol01.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "1",
		Title:          sql.NullString{String: "Book Title", Valid: true},
		Summary:        sql.NullString{String: "Book summary", Valid: true},
		Number:         sql.NullString{String: "1", Valid: true},
		PageCount:      188,
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	if _, err := store.CreateBook(ctx, database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           `Vol:01?.cbz`,
		Path:           filepath.Join(tempDir, "Library", "Raw Series", "Vol01-duplicate.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "2",
		PageCount:      166,
	}); err != nil {
		t.Fatalf("CreateBook second failed: %v", err)
	}

	tag, err := store.UpsertTag(ctx, "冒险")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	if err := store.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag failed: %v", err)
	}
	author, err := store.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: "Writer A", Role: "writer"})
	if err != nil {
		t.Fatalf("UpsertAuthor failed: %v", err)
	}
	if err := store.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: author.ID}); err != nil {
		t.Fatalf("LinkSeriesAuthor failed: %v", err)
	}
	return controller, series, book
}

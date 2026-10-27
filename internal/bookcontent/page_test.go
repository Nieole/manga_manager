package bookcontent_test

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/parser"
)

func archive(t *testing.T, path string, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.SetRGBA(x, y, color.RGBA{R: shade, G: uint8(x * 13), B: uint8(y * 11), A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, err := z.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { parser.EvictArchiveFromPool(path) })
	return data.Bytes()
}

func TestPageMaterializesBytesAndReleasesBorrowBeforeReturning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "book.cbz")
	want := archive(t, path, 90)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	source := bookcontent.Source{ID: 1, Path: path, Size: info.Size(), FileModifiedAt: info.ModTime(), PageCount: 1}
	cfg := config.Config{}
	service := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return cfg }, bookcontent.NewMemoryCache(1<<20), 8, 8)
	manifest, err := service.List(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Pages) != 1 {
		t.Fatalf("manifest: %+v", manifest)
	}
	result, err := service.Page(context.Background(), 1, 1, bookcontent.PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Data, want) || result.Derived || result.CacheSource != "raw" {
		t.Fatalf("raw result: %+v", result)
	}
	if got := parser.ActiveArchiveBorrows(path); got != 0 {
		t.Fatalf("materialized bytes still borrow archive: %d", got)
	}
	parser.EvictArchiveFromPool(path)
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatalf("materialized result retained a file handle: %v", err)
	}
	if !bytes.Equal(result.Data, want) {
		t.Fatal("moving the archive changed materialized page bytes")
	}
}

func TestDerivedPageCacheTracksVariantAndArchivePath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.cbz")
	b := filepath.Join(dir, "b.cbz")
	archive(t, a, 30)
	archive(t, b, 190)
	source := bookcontent.Source{ID: 1, Path: a, Size: 100, PageCount: 1}
	cfg := config.Config{}
	service := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return cfg }, nil, 8, 8)
	options := bookcontent.PageOptions{Width: 256, Format: "png"}
	first, err := service.Page(context.Background(), 1, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Derived || first.CacheSource != "processed" {
		t.Fatalf("first variant: %+v", first)
	}
	cached, err := service.Page(context.Background(), 1, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	if cached.CacheSource != "memory" || cached.Version != first.Version || !bytes.Equal(cached.Data, first.Data) {
		t.Fatalf("cached variant: %+v", cached)
	}
	options.Height = 512
	changed, err := service.Page(context.Background(), 1, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Version == first.Version || changed.CacheSource == "memory" {
		t.Fatal("variant options reused the wrong identity")
	}
	source.Path = b
	service.PurgeSources()
	options.Height = 0
	relocated, err := service.Page(context.Background(), 1, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	if relocated.Version == first.Version || bytes.Equal(relocated.Data, first.Data) {
		t.Fatal("different archive path reused cached bytes despite identical ID/size/mtime")
	}
}

func TestPassthroughAndUnavailableFilterStayOutOfProcessedCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.cbz")
	raw := archive(t, path, 80)
	source := bookcontent.Source{ID: 1, Path: path, PageCount: 1}
	service := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) { return source, nil }, func() config.Config { return config.Config{} }, nil, 8, 8)
	options := bookcontent.PageOptions{Width: 256, FitInside: true}
	for range 2 {
		result, err := service.Page(context.Background(), 1, 1, options)
		if err != nil {
			t.Fatal(err)
		}
		if result.Derived || result.CacheSource != "raw" || !bytes.Equal(result.Data, raw) {
			t.Fatalf("passthrough was cached or transformed: %+v", result)
		}
	}
	options.Filter = "waifu2x"
	first, err := service.Page(context.Background(), 1, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	if first.Derived || first.Fallback != "waifu2x" || first.CacheSource != "raw" {
		t.Fatalf("unavailable AI filter: %+v", first)
	}
	options.KnownVersion = first.Version
	second, err := service.Page(context.Background(), 1, 1, options)
	if err != nil {
		t.Fatal(err)
	}
	if !second.NotModified || !second.ArchiveOpen || second.Derived {
		t.Fatalf("fallback identity bypassed a fresh source read: %+v", second)
	}
}

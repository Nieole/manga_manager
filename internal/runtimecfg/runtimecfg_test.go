package runtimecfg

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
	"manga-manager/internal/images"
	"manga-manager/internal/logger"
	"manga-manager/internal/parser"
)

// TestApplyRebuildsRuntimeResources 验证 Apply 按配置重建全部三类派生资源，且再次以不同值调用会重建
// （幂等重建，而非只首次生效）——这是「文件热重载」与「API 保存」共享同一副作用集合的基础。
func TestApplyRebuildsRuntimeResources(t *testing.T) {
	cfg := &config.Config{}
	cfg.Scanner.ArchivePoolSize = 7
	cfg.Scanner.MaxAiConcurrency = 4
	cfg.Logging.Level = "warn"
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := parser.PoolMaxSize(); got != 7 {
		t.Fatalf("archive pool size = %d, want 7", got)
	}
	if got := images.AIConcurrency(); got != 4 {
		t.Fatalf("AI concurrency = %d, want 4", got)
	}
	if got := logger.CurrentLevel(); got != "warn" {
		t.Fatalf("log level = %q, want warn", got)
	}

	cfg.Scanner.ArchivePoolSize = 3
	cfg.Scanner.MaxAiConcurrency = 1
	cfg.Logging.Level = "debug"
	if err := Apply(cfg); err != nil {
		t.Fatalf("Apply (reapply): %v", err)
	}
	if got := parser.PoolMaxSize(); got != 3 {
		t.Fatalf("archive pool size after reapply = %d, want 3", got)
	}
	if got := images.AIConcurrency(); got != 1 {
		t.Fatalf("AI concurrency after reapply = %d, want 1", got)
	}
	if got := logger.CurrentLevel(); got != "debug" {
		t.Fatalf("log level after reapply = %q, want debug", got)
	}
}

var fixtureSerial atomic.Int64

type heldPixels struct {
	once    sync.Once
	entered chan struct{}
	release <-chan struct{}
	notify  func()
}

func (p *heldPixels) ColorModel() color.Model { return color.RGBAModel }
func (p *heldPixels) Bounds() image.Rectangle { return image.Rect(0, 0, 2, 2) }
func (p *heldPixels) At(int, int) color.Color {
	p.once.Do(func() {
		if p.notify != nil {
			p.notify()
		} else {
			close(p.entered)
		}
		<-p.release
	})
	return color.RGBA{R: 200, A: 255}
}

func TestAIResourceUpdateDoesNotDuplicateSoftwareBudget(t *testing.T) {
	owner := NewResources()
	t.Cleanup(owner.Close)
	cfg := &config.Config{}
	cfg.Logging.Level = "warn"
	cfg.Scanner.MaxAiConcurrency = 1
	if err := owner.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	limit := runtime.NumCPU()
	entered := make(chan struct{}, limit+1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	magic := fmt.Sprintf("runtimecfg-budget-%d", fixtureSerial.Add(1))
	image.RegisterFormat(magic, magic, func(io.Reader) (image.Image, error) {
		return &heldPixels{notify: func() { entered <- struct{}{} }, release: release}, nil
	}, func(io.Reader) (image.Config, error) {
		return image.Config{ColorModel: color.RGBAModel, Width: 2, Height: 2}, nil
	})
	finished := make(chan error, limit+1)
	process := func() {
		_, _, err := images.ProcessImage(context.Background(), []byte(magic), "image/fixture", images.ProcessOptions{Format: "png"})
		finished <- err
	}
	for i := 0; i < limit; i++ {
		go process()
	}
	for i := 0; i < limit; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("software work did not fill its budget")
		}
	}
	cfg.Scanner.MaxAiConcurrency = 2
	if err := owner.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	go process()
	select {
	case <-entered:
		t.Fatal("AI update admitted work past the occupied software budget")
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	for i := 0; i < limit+1; i++ {
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("software work failed to return permits")
		}
	}
}

func TestImageProcessingReturnsOriginalBudgetAfterResourceRetirement(t *testing.T) {
	owner := NewResources()
	t.Cleanup(owner.Close)
	cfg := &config.Config{}
	cfg.Logging.Level = "warn"
	cfg.Scanner.ArchivePoolSize = 1
	cfg.Scanner.MaxAiConcurrency = 1
	if err := owner.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	pixels := &heldPixels{entered: entered, release: release}
	magic := fmt.Sprintf("runtimecfg-image-%d", fixtureSerial.Add(1))
	image.RegisterFormat(magic, magic, func(io.Reader) (image.Image, error) { return pixels, nil }, func(io.Reader) (image.Config, error) {
		return image.Config{ColorModel: color.RGBAModel, Width: 2, Height: 2}, nil
	})
	finished := make(chan error, 1)
	go func() {
		data, ct, err := images.ProcessImage(context.Background(), []byte(magic), "image/fixture", images.ProcessOptions{Format: "png"})
		if err == nil {
			var decoded image.Image
			decoded, err = png.Decode(bytes.NewReader(data))
			if err == nil && (ct != "image/png" || decoded.Bounds().Dx() != 2 || decoded.At(0, 0) != (color.RGBA{R: 200, A: 255})) {
				err = fmt.Errorf("wrong decoded result: %s %v", ct, decoded.Bounds())
			}
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("processor did not enter encoding")
	}
	cfg.Scanner.MaxAiConcurrency = 2
	if err := owner.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	owner.Close()
	unblock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("old processor could not return its budget")
	}
}

func TestOwnedResourcesKeepBorrowUsableAcrossUpdateAndRetirement(t *testing.T) {
	owner := NewResources()
	t.Cleanup(owner.Close)
	cfg := &config.Config{}
	cfg.Scanner.ArchivePoolSize = 2
	cfg.Scanner.MaxAiConcurrency = 2
	cfg.Logging.Level = "warn"
	if err := owner.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "active.cbz")
	var page bytes.Buffer
	pixels := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pixels.Set(0, 0, color.RGBA{R: 200, A: 255})
	if err := png.Encode(&page, pixels); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	w, err := zw.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(page.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	archive, release, err := parser.GetArchiveFromPool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cfg.Scanner.ArchivePoolSize = 1
	cfg.Scanner.MaxAiConcurrency = 1
	if err := owner.Apply(cfg); err != nil {
		t.Fatal(err)
	}
	content := bookcontent.New(func(context.Context, int64) (bookcontent.Source, error) {
		return bookcontent.Source{ID: 1, LibraryID: 1, Path: path, PageCount: 1}, nil
	}, func() config.Config { return *cfg }, nil, 2, 2)
	result, err := content.Page(context.Background(), 1, 1, bookcontent.PageOptions{})
	if err != nil || !bytes.Equal(result.Data, page.Bytes()) {
		t.Fatalf("content consumer after resource switch: %d bytes %v", len(result.Data), err)
	}
	owner.Close()
	if data, err := archive.ReadPage("001.png"); err != nil || !bytes.Equal(data, page.Bytes()) {
		t.Fatalf("retiring resources interrupted the reader: %q %v", data, err)
	}
	if err := owner.Apply(cfg); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed owner accepted publication: %v", err)
	}
	if parser.PoolMaxSize() != 0 || images.AIConcurrency() != 0 {
		t.Fatal("closed owner reopened process resources")
	}
	release()
	if parser.ActiveArchiveBorrows(path) != 0 {
		t.Fatal("retired borrower was not returned")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("last return left the archive open: %v", err)
	}
}

func TestRejectedConfigurationPreservesPublishedResources(t *testing.T) {
	initial := &config.Config{}
	initial.Scanner.ArchivePoolSize = 3
	initial.Scanner.MaxAiConcurrency = 2
	initial.Logging.Level = "warn"
	if err := Apply(initial); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(Close)
	next := *initial
	next.Scanner.ArchivePoolSize = 9
	next.Scanner.MaxAiConcurrency = 7
	next.Logging.Level = "invalid-level"
	if err := Apply(&next); err == nil {
		t.Fatal("invalid log level was accepted")
	}
	if parser.PoolMaxSize() != 3 || images.AIConcurrency() != 2 || logger.CurrentLevel() != "warn" {
		t.Fatalf("rejected update changed resources: pool=%d image=%d log=%s", parser.PoolMaxSize(), images.AIConcurrency(), logger.CurrentLevel())
	}
}

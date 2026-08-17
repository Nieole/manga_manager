// 归档打开这一处**磁盘作业**的失败记账与令牌归还。

package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/parser"
)

// unreadablePagesArchive 打得开、读不出页目录，用来触碰归档打开**之后**的那条失败出口。
type unreadablePagesArchive struct{}

func (unreadablePagesArchive) Close() error { return nil }

func (unreadablePagesArchive) GetPages() ([]parser.PageMetadata, error) {
	return nil, errors.New("page directory unreadable")
}

func (unreadablePagesArchive) ReadPage(string) ([]byte, error) {
	return nil, errors.New("page directory unreadable")
}

func (unreadablePagesArchive) ReadMetadataFile(string) ([]byte, error) {
	return nil, errors.New("page directory unreadable")
}

// TestScanAccountsArchiveFailuresAndKeepsScanning 守归档打开这一处的两条失败出口：坏档计入
// 失败归档数，令牌照样归还。
//
// 库走外置硬盘档，归档打开并发因此是 1：任一条出口漏掉令牌，排在坏档后面的好档就再也拿不到
// 令牌，扫描会一直挂到用例超时——这才是本用例真正在守的东西，计数只是它的旁证。坏档夹在两个
// 好档中间，正为了让「漏令牌」必然表现成后一个好档扫不到。
func TestScanAccountsArchiveFailuresAndKeepsScanning(t *testing.T) {
	const brokenName = "Beta 02.cbz"
	tests := []struct {
		name string
		// breakArchive 把 brokenPath 那一档弄坏，坏法决定它停在哪条出口上。
		breakArchive   func(t *testing.T, s *Scanner, brokenPath string)
		openedArchives int64
	}{
		{
			name: "归档打不开",
			breakArchive: func(t *testing.T, _ *Scanner, brokenPath string) {
				t.Helper()
				if err := os.WriteFile(brokenPath, []byte("not a zip at all"), 0o644); err != nil {
					t.Fatalf("write broken cbz failed: %v", err)
				}
			},
			openedArchives: 2,
		},
		{
			name: "归档打开后读不出页目录",
			breakArchive: func(t *testing.T, s *Scanner, brokenPath string) {
				t.Helper()
				s.openArchive = func(path string) (parser.Archive, error) {
					if path == brokenPath {
						return unreadablePagesArchive{}, nil
					}
					return parser.OpenArchive(path)
				}
			},
			openedArchives: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, store, lib, libraryPath := newScannerTestLibrary(t)
			seriesPath := filepath.Join(libraryPath, "Series Alpha")
			for _, name := range []string{"Alpha 01.cbz", brokenName, "Gamma 03.cbz"} {
				if err := writeScannerTestCBZ(filepath.Join(seriesPath, name), map[string][]byte{"001.png": testPNG1x1}); err != nil {
					t.Fatalf("write cbz failed: %v", err)
				}
			}

			s := NewScanner(store, config.NewManager(newSlowDiskTestConfig(t)))
			tc.breakArchive(t, s, filepath.Join(seriesPath, brokenName))

			observer := &spyObserver{}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, observer); err != nil {
				t.Fatalf("ScanLibrary: %v", err)
			}

			metrics := observer.lastMetrics()
			if metrics.OpenedArchives != tc.openedArchives || metrics.FailedArchives != 1 {
				t.Fatalf("已打开/失败归档数为 %d/%d, want %d/1: %+v",
					metrics.OpenedArchives, metrics.FailedArchives, tc.openedArchives, metrics)
			}
			books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
			if err != nil {
				t.Fatalf("list books failed: %v", err)
			}
			if len(books) != 2 {
				t.Fatalf("入库 %d 本, want 2 —— 坏档不该入库，它后面的好档该入库", len(books))
			}
		})
	}
}

// newSlowDiskTestConfig 配出「归档打开并发为 1」的外置硬盘档，让漏掉的令牌无处藏身。
func newSlowDiskTestConfig(t testing.TB) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.Scanner.Workers = 2
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.Cache.Dir = filepath.Join(t.TempDir(), "thumbs")
	cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	config.NormalizeConfig(cfg)
	return cfg
}

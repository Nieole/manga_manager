// 封面构建这一处**磁盘作业**的令牌归还。

package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/parser"
)

// coverReadFailingArchive 打得开、列得出页目录、读不出页，正好停在封面构建读那一页的出口上；
// 页目录仍要读得出，否则扫描阶段就先失败了，封面作业根本排不上队。
type coverReadFailingArchive struct {
	parser.Archive
}

func (coverReadFailingArchive) ReadPage(string) ([]byte, error) {
	return nil, errors.New("cover page unreadable")
}

// TestCoverBuildReleasesTokenOnFailure 守封面构建的两条失败出口：坏档不出封面，排在它后面的
// 好档照样出封面。
//
// 库走外置硬盘档，封面并发因此是 1、封面 worker 也只有一个：任一条出口漏掉令牌，坏档后面那本
// 就再也拿不到令牌，封面队列会一直挂到用例超时——这才是本用例真正在守的东西。坏档夹在两个好档
// 中间，正为了让「漏令牌」必然表现成后一本没有封面。
func TestCoverBuildReleasesTokenOnFailure(t *testing.T) {
	const brokenName = "Beta 02.cbz"
	tests := []struct {
		name string
		// breakArchive 让 brokenPath 那一档在**封面构建**时坏掉，坏法决定它停在哪条出口上；
		// 扫描阶段那次打开必须照常成功。
		breakArchive func(t *testing.T, s *Scanner, brokenPath string)
	}{
		{
			name: "封面构建时归档打不开",
			breakArchive: func(t *testing.T, s *Scanner, brokenPath string) {
				t.Helper()
				// 一本书的封面作业排在它自己的元数据扫描之后，因此坏档的第二次打开必是封面构建那次。
				var mu sync.Mutex
				opened := map[string]int{}
				s.openArchive = func(path string) (parser.Archive, error) {
					mu.Lock()
					opened[path]++
					count := opened[path]
					mu.Unlock()
					if path == brokenPath && count > 1 {
						return nil, errors.New("archive vanished before cover build")
					}
					return parser.OpenArchive(path)
				}
			},
		},
		{
			name: "封面那一页读不出",
			breakArchive: func(t *testing.T, s *Scanner, brokenPath string) {
				t.Helper()
				s.openArchive = func(path string) (parser.Archive, error) {
					arc, err := parser.OpenArchive(path)
					if err != nil || path != brokenPath {
						return arc, err
					}
					return coverReadFailingArchive{Archive: arc}, nil
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, store, lib, libraryPath := newScannerTestLibrary(t)
			seriesPath := filepath.Join(libraryPath, "Series Alpha")
			archives := writeScannerTestSeries(t, seriesPath, "Alpha 01.cbz", brokenName, "Gamma 03.cbz")

			cfg := newSlowDiskTestConfig(t)
			// 单 worker 把扫描与封面队列都压成顺序的，坏档因此必然夹在两个好档中间。
			cfg.Scanner.Workers = 1
			s := NewScanner(store, config.NewManager(cfg))
			tc.breakArchive(t, s, filepath.Join(seriesPath, brokenName))

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
				t.Fatalf("ScanLibrary: %v", err)
			}
			if err := s.waitForCoverQueue(ctx); err != nil {
				t.Fatalf("封面队列没能收尾（漏掉的令牌会让后一本永远等下去）: %v", err)
			}

			books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
			if err != nil {
				t.Fatalf("list books failed: %v", err)
			}
			if len(books) != len(archives) {
				t.Fatalf("入库 %d 本, want %d —— 封面构建失败不该拦下入库", len(books), len(archives))
			}
			for _, book := range books {
				hasCover := book.CoverPath.Valid && book.CoverPath.String != ""
				broken := filepath.Base(book.Path) == brokenName
				if broken && hasCover {
					t.Fatalf("坏档 %s 不该有封面, got %q", book.Path, book.CoverPath.String)
				}
				if !broken && !hasCover {
					t.Fatalf("好档 %s 该有封面, got %+v", book.Path, book.CoverPath)
				}
			}
		})
	}
}

// 守封面自带通道之后扫描器这一侧的三条：扫描不再等封面、一批封面自己数自己、
// 一批的去处由接手它的那条运行的 ctx 说了算。

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// heldCoverBatches 是一个「接手但先不跑」的封面运行发起方：用例据此在扫描返回的那一刻
// 观察这一批还剩多少，正是生产里那条运行还排着队时的样子。
type heldCoverBatches struct {
	mu      sync.Mutex
	batches []*CoverBatch
}

func (h *heldCoverBatches) launch(batch *CoverBatch) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.batches = append(h.batches, batch)
}

func (h *heldCoverBatches) only(t *testing.T) *CoverBatch {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.batches) != 1 {
		t.Fatalf("接手了 %d 批封面, want 1 —— 一次扫描只排一批", len(h.batches))
	}
	return h.batches[0]
}

// recordedCovers 收下一条封面运行的报文，供断言「报文确实走了自带的那条通道」。
type recordedCovers struct {
	mu      sync.Mutex
	reports []CoverProgressReport
}

func (r *recordedCovers) Progress(report CoverProgressReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports = append(r.reports, report)
}

func (r *recordedCovers) last(t *testing.T) CoverProgressReport {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reports) == 0 {
		t.Fatal("封面运行一条报文都没收到 —— 那条通道没接上")
	}
	return r.reports[len(r.reports)-1]
}

// newCoverRunTestLibrary 铺一个两本书的库，返回扫描器与扫描要用的那两样。
func newCoverRunTestLibrary(t *testing.T) (*Scanner, database.Store, database.Library, string) {
	t.Helper()
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	writeScannerTestSeries(t, filepath.Join(libraryPath, "Series Alpha"), "Alpha 01.cbz", "Alpha 02.cbz")
	return NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata))), store, lib, libraryPath
}

// booksWithCover 数库里已经有封面的书。
func booksWithCover(t *testing.T, store database.Store, libraryID int64) int {
	t.Helper()
	books, err := store.ListBooksByLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatalf("list books failed: %v", err)
	}
	count := 0
	for _, book := range books {
		if book.CoverPath.Valid && book.CoverPath.String != "" {
			count++
		}
	}
	return count
}

// TestScanFinishesBeforeItsCoversDo 守本票那条对外行为变更：扫描在归档处理完就结束。
//
// 「扫描完成」此前是一句谎话——它在归档处理完就进**终态**，此后封面仍在后台生成，
// 而那些迟到的报文被终态守卫丢弃。现在那一批封面还整批挂在接手它的那条运行名下，
// 扫描返回的那一刻一张都还没生成。
func TestScanFinishesBeforeItsCoversDo(t *testing.T) {
	s, store, lib, libraryPath := newCoverRunTestLibrary(t)
	held := &heldCoverBatches{}
	s.SetCoverRunLauncher(held.launch)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// 扫描返回了，书全部入库，封面一张都没有：它不再假装封面也好了。
	if got := booksWithCover(t, store, lib.ID); got != 0 {
		t.Fatalf("扫描返回时已有 %d 本带封面 —— 扫描又在等封面了", got)
	}
	batch := held.only(t)
	if batch.LibraryID() != lib.ID {
		t.Fatalf("这一批封面挂在库 %d 上, want %d", batch.LibraryID(), lib.ID)
	}
	if batch.Queued() != 2 || batch.Remaining() != 2 {
		t.Fatalf("这一批排了 %d 张、剩 %d 张, want 2 / 2", batch.Queued(), batch.Remaining())
	}

	// 接手它的那条运行把它跑完，报文走的是它自带的那条通道，不经扫描观察者。
	covers := &recordedCovers{}
	if err := batch.Drain(ctx, covers); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if got := booksWithCover(t, store, lib.ID); got != 2 {
		t.Fatalf("封面运行跑完后有 %d 本带封面, want 2", got)
	}
	if last := covers.last(t); last.Generated != 2 || last.Remaining != 0 || last.Queued != 2 {
		t.Fatalf("收尾报文为 生成 %d / 剩 %d / 共 %d, want 2 / 0 / 2", last.Generated, last.Remaining, last.Queued)
	}
	if batch.Remaining() != 0 {
		t.Fatalf("跑完之后这一批还剩 %d 张 —— 结算漏了一条出口", batch.Remaining())
	}
}

// TestEachLibraryCountsItsOwnCoverBatch 守计数按批而不是全局：两个库各排一批，
// 各自只答得出自己那一批还剩几张。全局计数答的是「所有库加起来还剩几张」，
// 而任务中心上那两条运行各要自己的分母。
func TestEachLibraryCountsItsOwnCoverBatch(t *testing.T) {
	_, store, libA, pathA := newScannerTestLibrary(t)
	writeScannerTestSeries(t, filepath.Join(pathA, "Series Alpha"), "Alpha 01.cbz", "Alpha 02.cbz")
	pathB := filepath.Join(filepath.Dir(pathA), "Library B")
	seriesB := filepath.Join(pathB, "Series Beta")
	if err := os.MkdirAll(seriesB, 0o755); err != nil {
		t.Fatalf("mkdir library B failed: %v", err)
	}
	writeScannerTestSeries(t, seriesB, "Beta 01.cbz")
	libB, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name: "Library B", Path: pathB, ScanMode: "none", ScanInterval: 60,
		ScanFormats: config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("create library B failed: %v", err)
	}

	s := NewScanner(store, config.NewManager(newProfileScanTestConfig(t, config.ScanProfileMetadata)))
	held := &heldCoverBatches{}
	s.SetCoverRunLauncher(held.launch)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, lib := range []struct {
		row  database.Library
		path string
	}{{libA, pathA}, {libB, pathB}} {
		if err := s.ScanLibrary(ctx, lib.row.ID, lib.path, true, nil); err != nil {
			t.Fatalf("ScanLibrary %d: %v", lib.row.ID, err)
		}
	}

	held.mu.Lock()
	batches := append([]*CoverBatch(nil), held.batches...)
	held.mu.Unlock()
	if len(batches) != 2 {
		t.Fatalf("接手了 %d 批, want 2 —— 每库一批", len(batches))
	}
	want := map[int64]int64{libA.ID: 2, libB.ID: 1}
	for _, batch := range batches {
		if got := batch.Remaining(); got != want[batch.LibraryID()] {
			t.Fatalf("库 %d 那一批剩 %d 张, want %d —— 计数串到别的库去了",
				batch.LibraryID(), got, want[batch.LibraryID()])
		}
	}
}

// TestCancelledCoverRunLeavesTheScanAlone 守封面可以单独取消：按下封面那条运行的取消，
// 停的只是封面——书还在库里，扫描那条运行不受影响（用户「只让出一部分磁盘」）。
func TestCancelledCoverRunLeavesTheScanAlone(t *testing.T) {
	s, store, lib, libraryPath := newCoverRunTestLibrary(t)
	held := &heldCoverBatches{}
	s.SetCoverRunLauncher(held.launch)

	scanCtx, cancelScan := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelScan()
	if err := s.ScanLibrary(scanCtx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// 封面运行有它自己的 ctx：取消它不碰扫描那一条。
	coverCtx, cancelCovers := context.WithCancel(context.Background())
	cancelCovers()
	if err := held.only(t).Drain(coverCtx, nil); err == nil {
		t.Fatal("取消掉的封面运行仍然跑完了这一批")
	}
	if got := booksWithCover(t, store, lib.ID); got != 0 {
		t.Fatalf("取消之后仍生成了 %d 张封面", got)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 2 {
		t.Fatalf("取消封面把扫描的成果一起带走了：err=%v 入库 %d 本, want 2", err, len(books))
	}
	if scanCtx.Err() != nil {
		t.Fatalf("取消封面把扫描的上下文也取消了: %v", scanCtx.Err())
	}
}

// TestQueueMissingCoversRebuildsTheBatch 守**续跑与重试**那条路：进程里那一批随重启一起没了，
// 缺封面的书还在库里，重新排一遍就能补上。挑哪一页由封面构建自己去读——
// 库里读得到书行，读不到归档里的页目录。
func TestQueueMissingCoversRebuildsTheBatch(t *testing.T) {
	s, store, lib, libraryPath := newCoverRunTestLibrary(t)
	held := &heldCoverBatches{}
	s.SetCoverRunLauncher(held.launch)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	// 那一批被丢掉了（相当于服务在封面生成期间重启）。
	held.only(t)

	batch, err := s.QueueMissingCovers(ctx, lib.ID)
	if err != nil {
		t.Fatalf("QueueMissingCovers: %v", err)
	}
	if batch.Queued() != 2 {
		t.Fatalf("重排了 %d 张, want 2 —— 缺封面的书没被找全", batch.Queued())
	}
	covers := &recordedCovers{}
	if err := batch.Drain(ctx, covers); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if got := booksWithCover(t, store, lib.ID); got != 2 {
		t.Fatalf("续跑之后有 %d 本带封面, want 2", got)
	}

	// 已经补齐之后再排一次：一张都不该再排，否则每次重启都要把整库封面重做一遍。
	again, err := s.QueueMissingCovers(ctx, lib.ID)
	if err != nil {
		t.Fatalf("QueueMissingCovers 第二次: %v", err)
	}
	if again.Queued() != 0 {
		t.Fatalf("封面齐全之后仍重排了 %d 张", again.Queued())
	}
}

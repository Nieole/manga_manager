// 守书籍**指纹**重建的**磁盘作业**语义：逐本读完整个文件之前必须先取**存储令牌**，因此慢盘上
// 它会为前台阅读让路；指纹算不出来时只跳过这一本，**分页游标照常推进**。
//
// 两条都没有编译期约束。少了令牌，用户一边翻页一边跑指纹重建，盘被两边一起抢；游标停住则批次
// 按 id > afterID 切，坏书排在批次末尾时同一本被永远切回来，任务再也走不到尽头。

package koreader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/storageio"
)

// seedHashableBook 落一本内容已写盘、缺**指纹**的书。
func seedHashableBook(t *testing.T, store database.Store, rootDir, name string) database.Book {
	t.Helper()

	_, book := seedServiceBook(t, store, rootDir, "Library"+name, "Series"+name, name+".cbz")
	if err := os.WriteFile(book.Path, []byte(book.Name), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}
	return book
}

// hashableBookSeriesDir 是 seedHashableBook 给这本书铺的系列目录，比它所属资料库的根深一层。
// 策略条目配在这一层，「按谁的路径解析策略」才有两个不同的答案。两处的目录名必须一致，不一致
// 时策略匹配不上，用例会当场变红而不是悄悄失去守卫。
func hashableBookSeriesDir(rootDir, name string) string {
	return filepath.Join(rootDir, "Library"+name, "Series"+name)
}

// assertRebuildBlocked 断言重建在**拿不到令牌**时既跑不完也不读盘。取不到令牌就该一直等着，
// 因此这里靠一个短超时把它逼回来：没取令牌的实现会在超时之前把书算完，于是三条断言全落空。
func assertRebuildBlocked(t *testing.T, service *Service, store database.Store, book database.Book, reason string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	updated, _, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 500}, nil)
	if err == nil {
		t.Fatalf("重建跑完了 —— %s", reason)
	}
	if updated != 0 {
		t.Fatalf("updated = %d, want 0 —— %s", updated, reason)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, book.ID); fileHash != "" {
		t.Fatalf("file_hash = %q, want 空 —— %s", fileHash, reason)
	}
}

func TestRebuildBookIdentitiesAcquiresHashToken(t *testing.T) {
	sched := storageio.NewScheduler()
	service, store, rootDir := newTestServiceWithStorage(t, config.KOReaderMatchModeBinaryHash, sched, func(cfg *config.Config, rootDir string) {
		// 限流的那一档配在**比库根更深**的系列目录上，库根那一档不限流。这样一来这条用例连
		// 「按谁的路径解析存储策略」一起守住了：按**书**的路径解析才会撞上下面那把锁，按库根
		// 解析会落到 SSD 档、一路放行。
		//
		// custom 档的正值不会被归一化改写：哈希并发压到 1，其余三项都更宽——工种挑错了字段，
		// 那把锁同样挡不住它。
		cfg.Library.StorageProfile = config.StorageProfileSSD
		cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{
			Path:           hashableBookSeriesDir(rootDir, "Book1"),
			StorageProfile: config.StorageProfileCustom,
			IOPolicy: config.StorageIOPolicy{
				ScanConcurrency:        5,
				ArchiveOpenConcurrency: 5,
				CoverConcurrency:       5,
				HashConcurrency:        1,
			},
		}}
	})
	book := seedHashableBook(t, store, rootDir, "Book1")

	// 占住「哈希并发 = 1」那把限流器。令牌按 卷|上限 计数，因此这把锁只挡请求上限同为 1 的作业。
	held, err := sched.Acquire(context.Background(), storageio.Request{
		VolumeKey: config.VolumeKey(book.Path),
		Limit:     1,
		Kind:      storageio.WorkKindIdentityHash,
	})
	if err != nil {
		t.Fatalf("acquire holder token: %v", err)
	}

	assertRebuildBlocked(t, service, store, book, "哈希令牌被占满时它不该动手，而它没取令牌")

	// 归还之后同一本必须算得出来：上面那几条断言不能靠「重建根本不工作」来满足。
	held.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updated, total, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 500}, nil)
	if err != nil {
		t.Fatalf("令牌归还后重建返回 %v, want nil", err)
	}
	if updated != 1 || total != 1 {
		t.Fatalf("令牌归还后 updated=%d total=%d, want 1/1", updated, total)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, book.ID); fileHash == "" {
		t.Fatal("file_hash 仍为空 —— 令牌归还后这一本该算出指纹")
	}
}

func TestRebuildBookIdentitiesYieldsToForegroundReading(t *testing.T) {
	sched := storageio.NewScheduler()
	service, store, rootDir := newTestServiceWithStorage(t, config.KOReaderMatchModeBinaryHash, sched, func(cfg *config.Config, _ string) {
		// 外置硬盘档：为阅读让路与只在空闲时干重活两项都为真，正是本票要修的那一条。
		cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	})
	book := seedHashableBook(t, store, rootDir, "Book1")

	// 前台取页。上限刻意不取 1：取 1 会顺手占满哈希那把限流器，于是这条用例守的就变成了
	// 「限流器满了要排队」，而不是「有人在读这块盘，后台就该让路」。
	reading, err := sched.Acquire(context.Background(), storageio.Request{
		VolumeKey: config.VolumeKey(book.Path),
		Limit:     4,
		Kind:      storageio.WorkKindReader,
	})
	if err != nil {
		t.Fatalf("acquire reader token: %v", err)
	}
	defer reading.Release()

	assertRebuildBlocked(t, service, store, book, "前台正在读这块盘，它该让路")
}

// TestRebuildBookIdentitiesPausesBetweenBatches 守批间停顿：低优先级回填全靠它把自己压低到长时间
// 跑着也不碍事，而停顿本身必须可被取消——不然一次关服要等它睡完才走得掉。
func TestRebuildBookIdentitiesPausesBetweenBatches(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeBinaryHash)

	first := seedHashableBook(t, store, rootDir, "First")
	second := seedHashableBook(t, store, rootDir, "Second")

	// 一批一本、停顿 300ms：100ms 的期限必然落在第一批之后的那段停顿里。没有停顿的话两本都会
	// 在几毫秒内算完，这条用例就再也等不到那个期限。
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	updated, total, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 1, BatchGap: 300 * time.Millisecond}, nil)
	if err == nil {
		t.Fatal("重建跑完了 —— 批间停顿没生效，两批之间一步没歇")
	}
	if updated != 1 || total != 2 {
		t.Fatalf("updated=%d total=%d, want 1/2 —— 第一批该算完，第二批该被停顿挡在期限之外", updated, total)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, first.ID); fileHash == "" {
		t.Fatal("第一本的 file_hash 为空 —— 停顿之前那一批该正常算完")
	}
	if fileHash, _, _ := loadBookIdentity(t, store, second.ID); fileHash != "" {
		t.Fatalf("第二本的 file_hash = %q, want 空 —— 停顿被取消打断后不该再往下算", fileHash)
	}
}

func TestRebuildBookIdentitiesSkipsUnreadableBookAndAdvancesCursor(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeBinaryHash)

	good := seedHashableBook(t, store, rootDir, "Good")
	// 坏书必须排在**最后**：它的文件根本没落盘，指纹必然算不出来，而只有它是这一批的末尾时，
	// 「跳过时不推游标」才真的会把循环钉死——排在中间的话，后面那本好书会替它把游标推过去，
	// 缺陷被顺手掩盖。
	_, bad := seedServiceBook(t, store, rootDir, "LibraryBad", "SeriesBad", "Bad.cbz")

	// 超时当守卫：游标失守时这条是超时失败，而不是把整个包挂死。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updated, total, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 500}, nil)
	if err != nil {
		t.Fatalf("重建返回 %v, want nil —— 超时说明坏书上的分页游标没推进，同一本被永远切回来", err)
	}
	if updated != 1 || total != 2 {
		t.Fatalf("重建结果为 updated=%d total=%d, want 1/2 —— 坏书该跳过、好书该落库", updated, total)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, good.ID); fileHash == "" {
		t.Fatal("好书的 file_hash 为空 —— 坏书不该连累它")
	}
	if fileHash, _, _ := loadBookIdentity(t, store, bad.ID); fileHash != "" {
		t.Fatalf("坏书的 file_hash = %q, want 空", fileHash)
	}
}

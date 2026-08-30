// 守书籍**指纹**重建那条批循环的三条不变量：逐本读完整个文件必须经**任务句柄**的**磁盘作业**
// 入口发起；指纹算不出来只跳过这一本、**分页游标照常推进**；中止只由磁盘作业入口返回的错误决定。
//
// 三条都没有编译期约束。绕开磁盘作业入口，用户一边翻页一边跑指纹重建，盘被两边一起抢；游标停住
// 则批次按 id > afterID 切，坏书排在批次末尾时同一本被永远切回来，任务再也走不到尽头；把「算不出
// 指纹」也当成中止条件，一本坏书就能让整轮回填停在半路。

package koreader

import (
	"context"
	"errors"
	"os"
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

// TestRebuildBookIdentitiesReadsBooksThroughDiskWork 守「读整个文件必须走磁盘作业入口」：
// 工种与路径都要对上——工种错了限流会按别的上限放行，路径错了存储策略会按别人的盘解析。
func TestRebuildBookIdentitiesReadsBooksThroughDiskWork(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeBinaryHash)
	book := seedHashableBook(t, store, rootDir, "Book1")

	task := &fakeTaskHandle{}
	updated, total, err := service.RebuildBookIdentities(context.Background(), RebuildOptions{BatchSize: 500}, task)
	if err != nil {
		t.Fatalf("重建返回 %v, want nil", err)
	}
	if updated != 1 || total != 1 {
		t.Fatalf("updated=%d total=%d, want 1/1", updated, total)
	}
	if len(task.works) != 1 {
		t.Fatalf("经磁盘作业入口发起了 %d 次, want 1 —— 读整个文件绕开了闸门与令牌", len(task.works))
	}
	if task.works[0].Kind != storageio.WorkKindIdentityHash {
		t.Fatalf("**磁盘作业**的工种为 %q, want %q", task.works[0].Kind, storageio.WorkKindIdentityHash)
	}
	if task.works[0].Path != book.Path {
		t.Fatalf("磁盘作业报的路径为 %q, want %q —— 存储策略按这个路径解析", task.works[0].Path, book.Path)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, book.ID); fileHash == "" {
		t.Fatal("file_hash 仍为空 —— 上面几条断言不能靠「重建根本不工作」来满足")
	}
}

// TestRebuildBookIdentitiesStopsWhenDiskWorkIsRefused 守中止条件的上半条：磁盘作业入口回绝
// （**暂停闸门**或**存储令牌**的错误）就地中止，一个字节都不读、一本都不落库。
func TestRebuildBookIdentitiesStopsWhenDiskWorkIsRefused(t *testing.T) {
	service, store, rootDir := newTestService(t, config.KOReaderMatchModeBinaryHash)
	book := seedHashableBook(t, store, rootDir, "Book1")

	refused := errors.New("storage token unavailable")
	task := &fakeTaskHandle{diskErr: refused}
	updated, _, err := service.RebuildBookIdentities(context.Background(), RebuildOptions{BatchSize: 500}, task)
	if !errors.Is(err, refused) {
		t.Fatalf("重建返回 %v, want %v —— 磁盘作业被回绝时它该就地中止", err, refused)
	}
	if updated != 0 {
		t.Fatalf("updated = %d, want 0", updated)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, book.ID); fileHash != "" {
		t.Fatalf("file_hash = %q, want 空 —— 被挡下的书一个字节都没读，不该有指纹", fileHash)
	}
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

	updated, total, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 1, BatchGap: 300 * time.Millisecond}, &fakeTaskHandle{})
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

// TestRebuildBookIdentitiesSkipsUnreadableBookAndAdvancesCursor 一条用例守两件事：算不出指纹
// 不中止（中止条件的下半条），以及跳过时分页游标照常推进。
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

	task := &fakeTaskHandle{}
	updated, total, err := service.RebuildBookIdentities(ctx, RebuildOptions{BatchSize: 500}, task)
	if err != nil {
		t.Fatalf("重建返回 %v, want nil —— 超时说明坏书上的分页游标没推进，同一本被永远切回来", err)
	}
	if updated != 1 || total != 2 {
		t.Fatalf("重建结果为 updated=%d total=%d, want 1/2 —— 坏书该跳过、好书该落库", updated, total)
	}
	if len(task.advances) != 1 || task.advances[0] != 1 {
		t.Fatalf("**计数推进**报了 %v, want [1] —— 跳过的那本不该计入", task.advances)
	}
	if fileHash, _, _ := loadBookIdentity(t, store, good.ID); fileHash == "" {
		t.Fatal("好书的 file_hash 为空 —— 坏书不该连累它")
	}
	if fileHash, _, _ := loadBookIdentity(t, store, bad.ID); fileHash != "" {
		t.Fatalf("坏书的 file_hash = %q, want 空", fileHash)
	}
}

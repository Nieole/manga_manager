// 守 ComicInfo 回写经启动入口之后的三件事：任务声明一次落地、逐条目进度整帧报出、
// 三个计数走占位参数而不是后端拼好的一句话。
//
// 计数拼成句子就只能有一种语言；帧撕开了，任务面板上的书名与进度条来自两本不同的书。

package api

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
)

const comicInfoSeriesID = int64(7)

// newComicInfoRig 拼出回写任务体要的那两样：任务引擎（仍经它唯一的 seam 构造）与配置。
// 任务体不读数据库——系列、书目、标签与作者都由启动点从 HTTP 层带进来，因此这里没有存储替身。
func newComicInfoRig(t *testing.T, now func() time.Time, run func(func())) (*Controller, func() []TaskStatus) {
	t.Helper()
	e, snapshots := newBackgroundTestEngine(run)
	e.now = now

	cfg := &config.Config{}
	cfg.Cache.Dir = t.TempDir()
	config.NormalizeConfig(cfg)

	c := &Controller{taskEngine: e, config: config.NewManager(cfg)}
	// 回写走**磁盘作业**入口。调度器**必须**新建而不能用包级实例：后者按卷计数，
	// 用例之间会经它互相污染。
	c.diskWork = diskwork.NewRunner(c.currentConfig, storageio.NewScheduler())
	// 引擎交给任务体的**任务句柄**要能发起磁盘作业：回写的每一本书都经它写。
	e.diskWork = c.diskWork
	return c, snapshots
}

func comicInfoSeries() database.Series {
	return database.Series{ID: comicInfoSeriesID, Name: "Series A"}
}

// writableBook 在盘上落成一本真实的 cbz：回写要解压重压，指向一个不存在的路径只会落进失败计数。
func writableBook(t *testing.T, dir string, id int64, name string) database.Book {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("001.jpg")
	if err != nil {
		t.Fatalf("造归档条目失败: %v", err)
	}
	if _, err := entry.Write([]byte("page")); err != nil {
		t.Fatalf("写归档条目失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("收尾归档失败: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("落盘归档失败: %v", err)
	}
	return database.Book{ID: id, SeriesID: comicInfoSeriesID, Name: name, Path: path}
}

// bookWithoutFile 造一条指向空气的书目：书目在、盘上的文件不在。扩展名决定它落进哪一类——
// `.cbr` 在打开之前就被判为格式不可写（算**跳过**，不是失败），`.cbz` 会真的去打开、然后失败。
func bookWithoutFile(dir string, id int64, name string) database.Book {
	return database.Book{ID: id, SeriesID: comicInfoSeriesID, Name: name, Path: filepath.Join(dir, name)}
}

// archiveHasComicInfo 判断归档里已经有 ComicInfo.xml——回写到底有没有落到用户的原始文件上，
// 只有这里看得出来。
func archiveHasComicInfo(t *testing.T, path string) bool {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("打开归档失败: %v", err)
	}
	defer func() { _ = reader.Close() }()
	for _, entry := range reader.File {
		if strings.EqualFold(entry.Name, "ComicInfo.xml") {
			return true
		}
	}
	return false
}

// TestWriteComicInfoDeclarationLandsWhole 守任务声明一次性落地：总数、元数据、**作用域**显示名
// 与两项控制能力都在首帧上。拆成启动之后的补写会留下一个「任务已在列表里、却还没有作用域名」
// 的窗口，任务列表接口能观察到。
//
// 它同时钉住「可取消但不可暂停」：任务中心的暂停控件由 can_pause 决定。
func TestWriteComicInfoDeclarationLandsWhole(t *testing.T) {
	// 后台能力只登记不执行：本用例要看的是任务诞生那一刻，任务体一步都不该跑。
	c, snapshots := newComicInfoRig(t, frozenClock(), func(func()) {})
	dir := t.TempDir()
	books := []database.Book{
		writableBook(t, dir, 1, "vol01.cbz"),
		writableBook(t, dir, 2, "vol02.cbz"),
	}
	key := writeComicInfoTaskKey(comicInfoSeriesID)

	if err := c.launchWriteSeriesComicInfoTask(comicInfoSeries(), books, nil, nil); err != nil {
		t.Fatalf("启动 ComicInfo 回写失败: %v", err)
	}

	if got := publishedCountFor(snapshots(), key); got != 1 {
		t.Fatalf("任务诞生投递了 %d 条载荷, want 1 —— 声明被拆成了启动之后的多次写入", got)
	}
	task := firstPublishedTask(t, snapshots(), key)
	if task.Total != 2 || task.MessageCode != "task.msg.write_comicinfo.start" {
		t.Fatalf("首帧的总数 / 起始文案码为 %d / %q, want 2 + ...write_comicinfo.start", task.Total, task.MessageCode)
	}
	if task.Params["series_id"] != "7" || task.ScopeName != "Series A" {
		t.Fatalf("首帧没带齐元数据与作用域名：params=%v scopeName=%q", task.Params, task.ScopeName)
	}
	// **作用域**由任务类型与任务键推导：类型里没有 series 二字，于是这个任务是**系统**作用域、
	// 却带着系列 id。这个组合看着别扭，但改它会挪动这个任务在任务中心的挂点，不得顺手改。
	if task.Scope != "system" || task.ScopeID == nil || *task.ScopeID != comicInfoSeriesID {
		t.Fatalf("作用域推导为 %q / %v, want system + %d", task.Scope, task.ScopeID, comicInfoSeriesID)
	}
	if !task.CanCancel || task.CanPause {
		t.Fatalf("控制能力为 cancel=%v pause=%v, want 可取消、不可暂停", task.CanCancel, task.CanPause)
	}
	if err := c.taskEngine.pause(key); !errors.Is(err, errTaskNotPausable) {
		t.Fatalf("暂停这个不可暂停的任务返回 %v, want errTaskNotPausable", err)
	}
}

// TestWriteComicInfoFrameIsPublishedWhole 守一本书一条载荷，且那条载荷内部自洽：计数、书名与
// 三个结局计数都来自同一本书。拆成 Advance / Item / Metrics 分报即变红——本用例的时钟每帧都
// 跨过投递窗口，载荷条数当场翻几倍；生产里真正的代价是水位放行头一条、吞掉后几条，
// 于是同一条载荷里进度条已经到第 N 本、书名还停在第 N-1 本。
func TestWriteComicInfoFrameIsPublishedWhole(t *testing.T) {
	c, snapshots := newComicInfoRig(t, windowSteppingClock(), runTaskBodySynchronously)
	dir := t.TempDir()
	books := []database.Book{
		writableBook(t, dir, 1, "vol01.cbz"),
		bookWithoutFile(dir, 2, "vol02.cbr"),
	}
	key := writeComicInfoTaskKey(comicInfoSeriesID)

	if err := c.launchWriteSeriesComicInfoTask(comicInfoSeries(), books, nil, nil); err != nil {
		t.Fatalf("启动 ComicInfo 回写失败: %v", err)
	}

	frames := publishedTasksWithCode(snapshots(), key, "task.msg.write_comicinfo.progress")
	want := []struct {
		item    string
		written int64
		skipped int64
	}{
		{"vol01.cbz", 1, 0},
		{"vol02.cbr", 1, 1},
	}
	if len(frames) != len(want) {
		t.Fatalf("两本书投递了 %d 条逐条目载荷, want %d", len(frames), len(want))
	}
	for i, frame := range frames {
		if frame.Current != i+1 || frame.Total != 2 {
			t.Fatalf("第 %d 帧的**计数推进**为 %d/%d, want %d/2", i+1, frame.Current, frame.Total, i+1)
		}
		if frame.CurrentItem != want[i].item {
			t.Fatalf("第 %d 帧的条目为 %q, want %q —— 帧被撕开了", i+1, frame.CurrentItem, want[i].item)
		}
		if frame.Metrics["written"] != want[i].written || frame.Metrics["skipped"] != want[i].skipped {
			t.Fatalf("第 %d 帧的指标为 %v, 与计数不是同一本书", i+1, frame.Metrics)
		}
		if frame.Phase != "writing" {
			t.Fatalf("第 %d 帧的**阶段**为 %q, want writing", i+1, frame.Phase)
		}
	}
}

// TestWriteComicInfoCompletionCountsGoToParams 守**完成**文案的三个计数走占位参数：由后端拼成
// 一句话的话，这句话只能有一种语言，英文用户在任务中心看到的就是中文。
//
// 三本书刻意各走一条结局：写成、格式不可写（跳过）、打不开（失败）。
func TestWriteComicInfoCompletionCountsGoToParams(t *testing.T) {
	c, snapshots := newComicInfoRig(t, frozenClock(), runTaskBodySynchronously)
	dir := t.TempDir()
	books := []database.Book{
		writableBook(t, dir, 1, "vol01.cbz"),
		bookWithoutFile(dir, 2, "vol02.cbr"),
		bookWithoutFile(dir, 3, "vol03.cbz"),
	}
	key := writeComicInfoTaskKey(comicInfoSeriesID)

	if err := c.launchWriteSeriesComicInfoTask(comicInfoSeries(), books, nil, nil); err != nil {
		t.Fatalf("启动 ComicInfo 回写失败: %v", err)
	}

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "completed" || task.MessageCode != "task.msg.write_comicinfo.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...write_comicinfo.complete", task.Status, task.MessageCode)
	}
	if task.Message != "" {
		t.Fatalf("终态还带着一句后端拼好的文案：%q", task.Message)
	}
	if task.MessageParams["written"] != "1" || task.MessageParams["skipped"] != "1" || task.MessageParams["failed"] != "1" {
		t.Fatalf("完成文案的占位参数为 %v, want written=1 skipped=1 failed=1", task.MessageParams)
	}
	if task.Current != 3 || task.Total != 3 {
		t.Fatalf("收尾时进度条停在 %d/%d, want 3/3", task.Current, task.Total)
	}
	if !archiveHasComicInfo(t, books[0].Path) {
		t.Fatal("那本可写的书没有真的被写进 ComicInfo")
	}
}

// TestWriteComicInfoCancellationLandsCancelled 守取消由引擎裁决，并落到这个任务**自己的**取消
// 文案上：通用的那句「正在取消任务...」是**取消中**这个活动态的文案，拿它收尾会让一条**终态**
// 在说自己正在取消。
func TestWriteComicInfoCancellationLandsCancelled(t *testing.T) {
	var body func()
	c, snapshots := newComicInfoRig(t, frozenClock(), func(fn func()) { body = fn })
	dir := t.TempDir()
	books := []database.Book{writableBook(t, dir, 1, "vol01.cbz")}
	key := writeComicInfoTaskKey(comicInfoSeriesID)

	if err := c.launchWriteSeriesComicInfoTask(comicInfoSeries(), books, nil, nil); err != nil {
		t.Fatalf("启动 ComicInfo 回写失败: %v", err)
	}
	if err := c.taskEngine.cancel(key); err != nil {
		t.Fatalf("取消回写失败: %v", err)
	}
	body()

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "cancelled" || task.MessageCode != "task.msg.write_comicinfo.cancelled" {
		t.Fatalf("终态为 %q / %q, want cancelled + ...write_comicinfo.cancelled", task.Status, task.MessageCode)
	}
	if archiveHasComicInfo(t, books[0].Path) {
		t.Fatal("取消之后还是把 ComicInfo 写进用户的归档了")
	}
}

// TestWriteComicInfoRejectsSecondLaunchOnSameKey 守**任务键**闸门仍然生效，且返回的是哨兵错误
// ——HTTP 层据此才分得清 409 与 500。
func TestWriteComicInfoRejectsSecondLaunchOnSameKey(t *testing.T) {
	c, _ := newComicInfoRig(t, frozenClock(), func(func()) {})
	dir := t.TempDir()
	books := []database.Book{writableBook(t, dir, 1, "vol01.cbz")}

	if err := c.launchWriteSeriesComicInfoTask(comicInfoSeries(), books, nil, nil); err != nil {
		t.Fatalf("启动 ComicInfo 回写失败: %v", err)
	}
	if err := c.launchWriteSeriesComicInfoTask(comicInfoSeries(), books, nil, nil); !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("同一任务键再次启动返回 %v, want errTaskAlreadyRunning", err)
	}
}

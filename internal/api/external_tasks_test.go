// 守外部库扫描与传输经启动入口之后的两件事：这两条路径上只能有引擎水位一层节流，
// 以及各条**终态**分支落到自己那个文案码上。
//
// 任务体里再加一层节流，两层的节奏就各不相同；文案码串了，用户分不清刚才那次传输是全成了、
// 一本没传，还是有几本失败在那里。

package api

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/external"
)

// externalTaskStore 同时充当两个角色：Controller 的 database.Store（只有 GetLibrary 会被调到）
// 与 external.Manager 的窄接口。其余方法留给内嵌的 nil 接口——走到没实现的那条路会当场炸掉，
// 而不是拿到一个零值继续跑完。
type externalTaskStore struct {
	database.Store

	lib       database.Library
	books     []database.ExternalLibraryBookRow
	transfers []database.ListExternalTransferBooksBySeriesRow
}

func (s *externalTaskStore) GetLibrary(_ context.Context, id int64) (database.Library, error) {
	if id != s.lib.ID {
		return database.Library{}, sql.ErrNoRows
	}
	return s.lib, nil
}

func (s *externalTaskStore) ListExternalLibraryBooksByLibrary(context.Context, int64) ([]database.ExternalLibraryBookRow, error) {
	return s.books, nil
}

func (s *externalTaskStore) ListExternalTransferBooksBySeries(context.Context, []int64) ([]database.ListExternalTransferBooksBySeriesRow, error) {
	return s.transfers, nil
}

// steppingClock 每读一次就自己往前走一步。引擎每投递一帧读一次时钟，因此步长取一个投递窗口
// 就等于「每一帧都恰好落在窗口之外」。它测的是水位认不认这个注入的时钟——任务体里若还留着一层
// 按墙上时钟计时的节流，这个时钟怎么走都撬不动它。
type steppingClock struct {
	clock *fakeClock
	step  time.Duration
}

func (c *steppingClock) Now() time.Time {
	now := c.clock.Now()
	c.clock.advance(c.step)
	return now
}

// externalRig 把两个外部库任务体需要的东西凑齐：引擎（仍经它唯一的 seam 构造）、外部库会话
// 管理器，以及资料库与外部库两个真实目录——传输任务真的在拷文件。
type externalRig struct {
	c           *Controller
	snapshots   func() []TaskStatus
	libraryID   int64
	externalDir string
}

// newExternalRig 造 books 本书：库目录里放出真实文件，并登记成一个系列的书目。
// now 决定引擎怎么读时钟，run 决定任务体何时执行（取消用例要在任务体开跑之前按下取消）。
func newExternalRig(t *testing.T, books int, now func() time.Time, run func(func())) *externalRig {
	t.Helper()

	libraryDir := t.TempDir()
	externalDir := t.TempDir()
	seriesDir := filepath.Join(libraryDir, "Series A")
	if err := os.MkdirAll(seriesDir, 0o755); err != nil {
		t.Fatalf("建系列目录: %v", err)
	}

	const libraryID = int64(7)
	store := &externalTaskStore{
		lib: database.Library{ID: libraryID, Name: "Library A", Path: libraryDir},
	}
	for i := 0; i < books; i++ {
		name := "vol" + strconv.Itoa(i+1) + ".cbz"
		path := filepath.Join(seriesDir, name)
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatalf("写书文件: %v", err)
		}
		store.books = append(store.books, database.ExternalLibraryBookRow{
			BookID: int64(i + 1), SeriesID: 1, SeriesName: "Series A", Path: path,
		})
		store.transfers = append(store.transfers, database.ListExternalTransferBooksBySeriesRow{
			ID: int64(i + 1), SeriesID: 1, LibraryID: libraryID, Path: path, Volume: name,
		})
	}

	engine, snapshots := newBackgroundTestEngine(run)
	engine.now = now

	return &externalRig{
		c: &Controller{
			taskEngine: engine,
			store:      store,
			external:   external.NewManager(store, time.Hour),
		},
		snapshots:   snapshots,
		libraryID:   libraryID,
		externalDir: externalDir,
	}
}

// frozenClock 是不动的时钟：窗口内的一切都该被吞掉。
func frozenClock() func() time.Time {
	return (&fakeClock{now: time.Unix(1700000000, 0)}).Now
}

// windowSteppingClock 每读一次跨过一个投递窗口：每一帧都该被放行。
func windowSteppingClock() func() time.Time {
	return (&steppingClock{clock: &fakeClock{now: time.Unix(1700000000, 0)}, step: taskProgressPublishInterval}).Now
}

// readySession 建一个会话并扫成 ready 态——传输的规划阶段只认 ready。
func (r *externalRig) readySession(t *testing.T) string {
	t.Helper()
	snap, err := r.c.external.CreateSession(context.Background(), r.libraryID, r.externalDir, false)
	if err != nil {
		t.Fatalf("建外部库会话: %v", err)
	}
	if _, err := r.c.external.ScanSession(context.Background(), snap.SessionID, nil); err != nil {
		t.Fatalf("扫描外部库会话: %v", err)
	}
	return snap.SessionID
}

func (r *externalRig) plan(t *testing.T, sessionID string) external.TransferPlan {
	t.Helper()
	plan, err := r.c.external.PrepareTransfer(context.Background(), r.libraryID, sessionID, []int64{1})
	if err != nil {
		t.Fatalf("规划传输: %v", err)
	}
	return plan
}

// countPublishedWithCode 数一数该任务键带指定文案码的载荷投递了几条。
func countPublishedWithCode(snapshots []TaskStatus, key, code string) int {
	count := 0
	for _, snapshot := range snapshots {
		if snapshot.Key == key && snapshot.MessageCode == code {
			count++
		}
	}
	return count
}

const transferItemCode = "task.msg.transfer_external_library.transferring"

// TestExternalTransferProgressObeysEngineWaterLevelOnly 是本票的核心断言：这条路径上的投递
// 节奏只由引擎水位决定，任务体里没有第二层节流。
//
// 两个时钟跑同一段传输：不动的时钟只放行首帧（其余帧展示态一字不差，该被吞掉），每读一次就跨过
// 一个窗口的时钟放行全部三帧。任务体里若还留着按墙上时钟计时的那层 500ms 节流，后者仍然只有
// 一帧出得去——三本书的拷贝在真实时间里连 1ms 都用不到。
func TestExternalTransferProgressObeysEngineWaterLevelOnly(t *testing.T) {
	itemFramesPublished := func(t *testing.T, now func() time.Time) int {
		t.Helper()
		rig := newExternalRig(t, 3, now, runTaskBodySynchronously)
		sessionID := rig.readySession(t)
		plan := rig.plan(t, sessionID)
		if len(plan.Operations) != 3 {
			t.Fatalf("规划出 %d 条操作, want 3", len(plan.Operations))
		}
		if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); err != nil {
			t.Fatalf("启动传输失败: %v", err)
		}
		key := externalLibraryTransferTaskKey(rig.libraryID, sessionID)
		return countPublishedWithCode(rig.snapshots(), key, transferItemCode)
	}

	if got := itemFramesPublished(t, frozenClock()); got != 1 {
		t.Fatalf("时钟不动时逐条目帧投递了 %d 条, want 1 —— 后两条与首帧展示态一字不差，必须被水位吞掉", got)
	}
	if got := itemFramesPublished(t, windowSteppingClock()); got != 3 {
		t.Fatalf("每帧都跨过窗口时只投递了 %d 条, want 3 —— 说明还有一层不认注入时钟的节流", got)
	}
}

// TestExternalTransferClosingFrameSurvivesThrottle 守「文案跃迁即使在窗口内也必须投递」这条
// 水位规则在本路径上成立：收尾那一帧换了文案码，因此哪怕时钟一动不动也得出去。
//
// 它同时是「传输文件」这个指标的落点——终态只把 Current 拉到 Total，不动指标。
func TestExternalTransferClosingFrameSurvivesThrottle(t *testing.T) {
	rig := newExternalRig(t, 3, frozenClock(), runTaskBodySynchronously)
	sessionID := rig.readySession(t)
	plan := rig.plan(t, sessionID)
	key := externalLibraryTransferTaskKey(rig.libraryID, sessionID)

	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); err != nil {
		t.Fatalf("启动传输失败: %v", err)
	}

	closing := publishedTaskWithCode(t, rig.snapshots(), key, "task.msg.transfer_external_library.progress")
	if closing.MessageParams["done"] != "3" || closing.MessageParams["total"] != "3" {
		t.Fatalf("收尾帧的占位参数为 %v, want done=3 total=3", closing.MessageParams)
	}
	if closing.Metrics["transferred_files"] != 3 {
		t.Fatalf("传输文件指标为 %v, want 3 —— 少了收尾这一帧它会停在倒数第二本上", closing.Metrics)
	}

	done := lastPublishedTask(t, rig.snapshots(), key)
	if done.Status != "completed" || done.MessageCode != "task.msg.transfer_external_library.complete" {
		t.Fatalf("终态为 %q / %q, want completed + complete", done.Status, done.MessageCode)
	}
	if done.MessageParams["added"] != "3" || done.Current != 3 || done.Total != 3 {
		t.Fatalf("完成帧为 %v, %d/%d", done.MessageParams, done.Current, done.Total)
	}
}

// TestExternalTransferDeclarationLandsWhole 守任务诞生那一刻就带齐会话、系列数、**作用域**
// 显示名与总数。拆成启动之后的补写会留下一个「任务已在列表里、分母却还是 0」的窗口，
// 任务列表接口能观察到它。
func TestExternalTransferDeclarationLandsWhole(t *testing.T) {
	rig := newExternalRig(t, 2, frozenClock(), runTaskBodySynchronously)
	sessionID := rig.readySession(t)
	plan := rig.plan(t, sessionID)
	key := externalLibraryTransferTaskKey(rig.libraryID, sessionID)

	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); err != nil {
		t.Fatalf("启动传输失败: %v", err)
	}

	first := firstPublishedTask(t, rig.snapshots(), key)
	if first.ScopeName != "Library A" || first.Scope != "library" {
		t.Fatalf("首帧的作用域为 %q / %q, want library + Library A", first.Scope, first.ScopeName)
	}
	if first.Params["session_id"] != sessionID || first.Params["series_count"] != "1" {
		t.Fatalf("首帧的任务参数为 %v", first.Params)
	}
	if first.Total != 2 || !first.CanCancel || !first.CanPause {
		t.Fatalf("首帧为 total=%d canCancel=%v canPause=%v, want 2/true/true", first.Total, first.CanCancel, first.CanPause)
	}
}

// TestExternalTransferAllExistNamesItsOwnVariant 守「目标已全部存在」这条变体：它是**完成**，
// 不是失败，且用自己那条文案码而不是常规完成文案。
func TestExternalTransferAllExistNamesItsOwnVariant(t *testing.T) {
	rig := newExternalRig(t, 0, frozenClock(), runTaskBodySynchronously)
	sessionID := rig.readySession(t)
	key := externalLibraryTransferTaskKey(rig.libraryID, sessionID)

	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, external.TransferPlan{}); err != nil {
		t.Fatalf("启动传输失败: %v", err)
	}

	done := lastPublishedTask(t, rig.snapshots(), key)
	if done.Status != "completed" || done.MessageCode != "task.msg.transfer_external_library.all_exist" {
		t.Fatalf("终态为 %q / %q, want completed + all_exist", done.Status, done.MessageCode)
	}
}

// TestExternalTransferPartialFailureNamesItsOwnVariant 守「部分成功」这条变体：它是失败态、
// 带自己那条文案码与成败计数，技术错误串留在 Error 里供诊断。
func TestExternalTransferPartialFailureNamesItsOwnVariant(t *testing.T) {
	rig := newExternalRig(t, 2, frozenClock(), runTaskBodySynchronously)
	sessionID := rig.readySession(t)
	plan := rig.plan(t, sessionID)
	key := externalLibraryTransferTaskKey(rig.libraryID, sessionID)

	// 把第一本的源文件抽走：拷贝在 os.Open 上失败，第二本照常传完。
	if err := os.Remove(plan.Operations[0].SourcePath); err != nil {
		t.Fatalf("移除源文件: %v", err)
	}

	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); err != nil {
		t.Fatalf("启动传输失败: %v", err)
	}

	done := lastPublishedTask(t, rig.snapshots(), key)
	if done.Status != "failed" || done.MessageCode != "task.msg.transfer_external_library.complete_with_failures" {
		t.Fatalf("终态为 %q / %q, want failed + complete_with_failures", done.Status, done.MessageCode)
	}
	if done.MessageParams["success"] != "1" || done.MessageParams["failed"] != "1" {
		t.Fatalf("成败计数为 %v, want success=1 failed=1", done.MessageParams)
	}
	if done.Error == "" {
		t.Fatal("失败态没留下技术错误串 —— 用户只会看到「有 1 本失败」，看不到是哪一本、为什么")
	}
	if _, err := os.Stat(plan.Operations[1].Destination); err != nil {
		t.Fatalf("第二本没传过去: %v —— 一本失败不该中止整批", err)
	}
}

// TestExternalTransferCancellationLandsCancelled 守取消由引擎裁决：任务体只把取消错误返回上去，
// 不自己写终态。任务体自己写的话，「忘了收尾」与「收了两次」都不会有编译错误。
func TestExternalTransferCancellationLandsCancelled(t *testing.T) {
	var body func()
	rig := newExternalRig(t, 2, frozenClock(), func(fn func()) { body = fn })
	sessionID := rig.readySession(t)
	plan := rig.plan(t, sessionID)
	key := externalLibraryTransferTaskKey(rig.libraryID, sessionID)

	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); err != nil {
		t.Fatalf("启动传输失败: %v", err)
	}
	if err := rig.c.taskEngine.cancel(key); err != nil {
		t.Fatalf("取消传输: %v", err)
	}
	body()

	done := lastPublishedTask(t, rig.snapshots(), key)
	if done.Status != "cancelled" || done.MessageCode != "task.msg.transfer_external_library.cancelled" {
		t.Fatalf("终态为 %q / %q, want cancelled + cancelled", done.Status, done.MessageCode)
	}
	if _, err := os.Stat(plan.Operations[0].Destination); err == nil {
		t.Fatal("取消之后还是把书拷过去了")
	}
}

// TestExternalScanReportsWholeFramesAndCompletes 守扫描的逐条目帧：计数、指标与占位参数同时
// 前进，一份报文一条载荷。拆开报会被水位撕断——同一条载荷里指标停在第 N-1 个文件、进度条已到第 N 个。
func TestExternalScanReportsWholeFramesAndCompletes(t *testing.T) {
	rig := newExternalRig(t, 0, windowSteppingClock(), runTaskBodySynchronously)
	for _, name := range []string{"a.cbz", "b.cbz"} {
		if err := os.WriteFile(filepath.Join(rig.externalDir, name), []byte(name), 0o644); err != nil {
			t.Fatalf("写外部文件: %v", err)
		}
	}
	snap, err := rig.c.external.CreateSession(context.Background(), rig.libraryID, rig.externalDir, false)
	if err != nil {
		t.Fatalf("建外部库会话: %v", err)
	}
	key := externalLibraryScanTaskKey(rig.libraryID, snap.SessionID)

	if err := rig.c.launchExternalLibraryScanTask(rig.libraryID, snap.SessionID); err != nil {
		t.Fatalf("启动外部库扫描失败: %v", err)
	}

	frame := publishedTaskWithCode(t, rig.snapshots(), key, "task.msg.scan_external_library.progress")
	if frame.Current != 2 || frame.Total != 2 || frame.Phase != "discovering" {
		t.Fatalf("**计数推进**为 %d/%d phase=%q, want 2/2 discovering", frame.Current, frame.Total, frame.Phase)
	}
	if frame.MessageParams["count"] != strconv.Itoa(frame.Current) || frame.Metrics["scanned_files"] != int64(frame.Current) {
		t.Fatalf("占位参数与指标跟计数不是同一刻：params=%v metrics=%v current=%d", frame.MessageParams, frame.Metrics, frame.Current)
	}

	first := firstPublishedTask(t, rig.snapshots(), key)
	if first.ScopeName != "Library A" || first.Params["session_id"] != snap.SessionID {
		t.Fatalf("首帧为 scopeName=%q params=%v", first.ScopeName, first.Params)
	}
	done := lastPublishedTask(t, rig.snapshots(), key)
	if done.Status != "completed" || done.MessageCode != "task.msg.scan_external_library.complete" {
		t.Fatalf("终态为 %q / %q, want completed + complete", done.Status, done.MessageCode)
	}
}

// TestExternalScanEmptyNamesItsOwnVariant 守「未发现可处理资源」这条变体：一个文件都没扫到
// 不是失败，但也不该用常规完成文案——用户得知道那个目录里什么都没有。
func TestExternalScanEmptyNamesItsOwnVariant(t *testing.T) {
	rig := newExternalRig(t, 0, frozenClock(), runTaskBodySynchronously)
	snap, err := rig.c.external.CreateSession(context.Background(), rig.libraryID, rig.externalDir, false)
	if err != nil {
		t.Fatalf("建外部库会话: %v", err)
	}
	key := externalLibraryScanTaskKey(rig.libraryID, snap.SessionID)

	if err := rig.c.launchExternalLibraryScanTask(rig.libraryID, snap.SessionID); err != nil {
		t.Fatalf("启动外部库扫描失败: %v", err)
	}

	done := lastPublishedTask(t, rig.snapshots(), key)
	if done.Status != "completed" || done.MessageCode != "task.msg.scan_external_library.complete_empty" {
		t.Fatalf("终态为 %q / %q, want completed + complete_empty", done.Status, done.MessageCode)
	}
}

// TestExternalTasksRejectSecondLaunchOnSameKey 守**任务键**闸门在这两条路径上仍然生效，
// 且第二次启动返回的是哨兵错误——HTTP 层据此才分得清 409 与 500。
func TestExternalTasksRejectSecondLaunchOnSameKey(t *testing.T) {
	var body func()
	rig := newExternalRig(t, 1, frozenClock(), func(fn func()) { body = fn })
	sessionID := rig.readySession(t)
	plan := rig.plan(t, sessionID)

	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); err != nil {
		t.Fatalf("首次启动传输失败: %v", err)
	}
	if err := rig.c.launchExternalLibraryTransferTask(rig.libraryID, sessionID, plan); !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("同键第二次启动返回 %v, want errTaskAlreadyRunning", err)
	}
	body()

	if err := rig.c.launchExternalLibraryScanTask(rig.libraryID, sessionID); err != nil {
		t.Fatalf("首次启动扫描失败: %v", err)
	}
	if err := rig.c.launchExternalLibraryScanTask(rig.libraryID, sessionID); !errors.Is(err, errTaskAlreadyRunning) {
		t.Fatalf("同键第二次启动返回 %v, want errTaskAlreadyRunning", err)
	}
}

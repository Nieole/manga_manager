// 守 KOReader 三个任务经启动入口之后的三件事：**两阶段**的匹配刷新每一步各报自己的失败文案码、
// 无论停在哪一步取消都落同一条取消码，以及一次进度回调只投递一条帧内自洽的载荷。
//
// 失败码串了，用户不知道是**指纹**没建成还是进度没对上；取消码被专属失败码盖过，一次关服会被
// 报成故障；帧撕开了，任务面板上的指标与进度条来自两个不同的时刻。

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

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	ksvc "manga-manager/internal/koreader"
	"manga-manager/internal/storageio"
)

// koreaderTaskStore 只实现三个 KOReader 任务体真正会调到的那几个方法。其余方法留给内嵌的
// nil 接口——任务体若走到没实现的那条路会当场炸掉，而不是拿到一个零值继续跑完。
type koreaderTaskStore struct {
	database.Store

	clock *fakeClock

	candidates []database.BookIdentityCandidate
	unmatched  []database.KOReaderProgress

	listIdentityErr   error
	countUnmatchedErr error

	identities []database.UpdateBookIdentityParams
}

func (s *koreaderTaskStore) CountBooksMissingIdentity(context.Context, string) (int64, error) {
	return int64(len(s.candidates)), nil
}

func (s *koreaderTaskStore) ListBooksMissingIdentityBatch(_ context.Context, _ string, afterID int64, _ int) ([]database.BookIdentityCandidate, error) {
	if s.listIdentityErr != nil {
		return nil, s.listIdentityErr
	}
	var out []database.BookIdentityCandidate
	for _, candidate := range s.candidates {
		if candidate.ID > afterID {
			out = append(out, candidate)
		}
	}
	return out, nil
}

func (s *koreaderTaskStore) UpdateBookIdentity(_ context.Context, arg database.UpdateBookIdentityParams) error {
	s.advancePublishWindow()
	s.identities = append(s.identities, arg)
	return nil
}

func (s *koreaderTaskStore) CountUnmatchedKOReaderProgress(context.Context) (int64, error) {
	return int64(len(s.unmatched)), s.countUnmatchedErr
}

func (s *koreaderTaskStore) ListUnmatchedKOReaderProgressBatch(_ context.Context, afterID int64, _ int) ([]database.KOReaderProgress, error) {
	var out []database.KOReaderProgress
	for _, item := range s.unmatched {
		if item.ID > afterID {
			out = append(out, item)
		}
	}
	return out, nil
}

// FindBookByDocumentFingerprint 一律不命中：本文件关心的是进度与**终态**，不是匹配算法本身。
// 未命中的记录照样计入已处理，进度回调因此仍会逐条触发，而落库那几步一个都不会走到。
func (s *koreaderTaskStore) FindBookByDocumentFingerprint(context.Context, string, string, bool) (database.KOReaderBookMatch, error) {
	s.advancePublishWindow()
	return database.KOReaderBookMatch{}, sql.ErrNoRows
}

// advancePublishWindow 把假时钟推过一个投递窗口。任务体在服务的批循环里跑，用例够不到两次进度
// 事件之间的那一刻，只能由被调到的存储方法代劳。不推的话，同一**阶段**里连续几次进度只有第一次
// 能被投递出去，「每份报文各成一帧」这条断言就没有第二条载荷可看。
func (s *koreaderTaskStore) advancePublishWindow() {
	if s.clock != nil {
		s.clock.advance(taskProgressPublishInterval * 2)
	}
}

// newKOReaderTaskRig 拼出三个 KOReader 任务体需要的那几样：任务引擎（仍经它唯一的 seam 构造，
// 后台能力换成同步执行版）、KOReader 服务与配置。
//
// 匹配模式取**路径**而非二进制哈希：那条路只拼字符串、不读书文件，多数用例因此不必造临时文件，
// 而任务引擎这一侧走的是同一条路径。顺带让元数据断言看到一组非默认取值。
func newKOReaderTaskRig(t *testing.T, store *koreaderTaskStore) (*Controller, func() []TaskStatus) {
	t.Helper()
	return newKOReaderTaskRigWithMode(t, store, config.KOReaderMatchModeFilePath)
}

// newKOReaderTaskRigWithMode 是同一套装置，但由用例挑匹配模式：二进制哈希那条路要逐本读书文件，
// 只有它会走到**磁盘作业**。
func newKOReaderTaskRigWithMode(t *testing.T, store *koreaderTaskStore, matchMode string) (*Controller, func() []TaskStatus) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	store.clock = clock
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)
	e.now = clock.Now

	cfg := &config.Config{}
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = matchMode
	cfg.KOReader.PathIgnoreExtension = true
	config.NormalizeConfig(cfg)
	manager := config.NewManager(cfg)

	// 调度器新建而非取包级实例：包级实例会让用例经由按卷计数的限流器互相污染。
	diskWork := diskwork.NewRunner(manager.Snapshot, storageio.NewScheduler())

	return &Controller{
		taskEngine: e,
		store:      store,
		config:     manager,
		koreader:   ksvc.NewService(store, manager, diskWork),
	}, snapshots
}

// seedKOReaderCandidates / seedUnmatchedProgress 造 n 条待处理记录。ID 从 1 起连号：两个批循环
// 都靠「ID 大于游标」切批、靠「空批」收尾，ID 为零的记录会让它们原地转下去。
func seedKOReaderCandidates(n int) []database.BookIdentityCandidate {
	out := make([]database.BookIdentityCandidate, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, database.BookIdentityCandidate{ID: int64(i), LibraryPath: "/lib", Path: "/lib/series/vol" + strconv.Itoa(i) + ".cbz"})
	}
	return out
}

func seedUnmatchedProgress(n int) []database.KOReaderProgress {
	out := make([]database.KOReaderProgress, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, database.KOReaderProgress{ID: int64(i), Username: "kindle", Document: "doc" + strconv.Itoa(i)})
	}
	return out
}

// TestRefreshMatchingNamesTheFailedStage 守两阶段各自的失败文案码：两步的技术错误串长得一样，
// 只报一句「匹配刷新失败」的话，用户不知道该去看指纹索引还是未匹配记录。
func TestRefreshMatchingNamesTheFailedStage(t *testing.T) {
	cases := []struct {
		name     string
		store    *koreaderTaskStore
		wantCode string
	}{
		{"指纹重建", &koreaderTaskStore{listIdentityErr: errors.New("volume offline")}, "task.msg.refresh_koreader_matching.rebuild_failed"},
		{"进度对账", &koreaderTaskStore{countUnmatchedErr: errors.New("volume offline")}, "task.msg.refresh_koreader_matching.reconcile_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots := newKOReaderTaskRig(t, tc.store)

			if err := c.launchRefreshKOReaderMatchingTask(); err != nil {
				t.Fatalf("启动匹配刷新失败: %v", err)
			}

			task := lastPublishedTask(t, snapshots(), "refresh_koreader_matching")
			if task.Status != "failed" {
				t.Fatalf("任务停在 %q, want failed", task.Status)
			}
			if task.MessageCode != tc.wantCode {
				t.Fatalf("失败文案码为 %q, want %q", task.MessageCode, tc.wantCode)
			}
			if task.Error != "volume offline" {
				t.Fatalf("技术错误串没有留在任务上：%q", task.Error)
			}
		})
	}
}

// TestRefreshMatchingCancellationSharesOneCode 守「专属失败文案必须挡在取消之外」：取消同样以
// ctx.Err() 的形式从两步里返回，无条件带上专属码的话，用户按下取消看到的会是「索引重建失败」。
func TestRefreshMatchingCancellationSharesOneCode(t *testing.T) {
	cases := []struct {
		name  string
		store *koreaderTaskStore
	}{
		{"指纹重建", &koreaderTaskStore{listIdentityErr: context.Canceled}},
		{"进度对账", &koreaderTaskStore{countUnmatchedErr: context.Canceled}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots := newKOReaderTaskRig(t, tc.store)

			if err := c.launchRefreshKOReaderMatchingTask(); err != nil {
				t.Fatalf("启动匹配刷新失败: %v", err)
			}

			task := lastPublishedTask(t, snapshots(), "refresh_koreader_matching")
			if task.Status != "cancelled" {
				t.Fatalf("任务停在 %q, want cancelled", task.Status)
			}
			if task.MessageCode != "task.msg.refresh_koreader_matching.cancelled" {
				t.Fatalf("取消文案码为 %q, want ...refresh_koreader_matching.cancelled", task.MessageCode)
			}
		})
	}
}

// TestRefreshMatchingWalksBothPhases 守两阶段任务的进度条数的是**阶段**而不是条目：
// 阶段跃迁把它从 0/2 推到 1/2，而两步内部的逐条目回调一次都不该动它——逐条目计数只进
// 文案占位参数与指标。数错了，用户会看到进度条跑到 40 本 / 共 2 的荒唐读数。
func TestRefreshMatchingWalksBothPhases(t *testing.T) {
	store := &koreaderTaskStore{candidates: seedKOReaderCandidates(2), unmatched: seedUnmatchedProgress(2)}
	c, snapshots := newKOReaderTaskRig(t, store)

	if err := c.launchRefreshKOReaderMatchingTask(); err != nil {
		t.Fatalf("启动匹配刷新失败: %v", err)
	}
	const key = "refresh_koreader_matching"

	rebuildStart := publishedTaskWithCode(t, snapshots(), key, "task.msg.refresh_koreader_matching.rebuild_start")
	if rebuildStart.Phase != "hashing" || rebuildStart.Current != 0 || rebuildStart.Total != 2 {
		t.Fatalf("第一阶段开工帧为 phase=%q %d/%d, want hashing 0/2", rebuildStart.Phase, rebuildStart.Current, rebuildStart.Total)
	}

	rebuilding := publishedTaskWithCode(t, snapshots(), key, "task.msg.koreader_rebuild_hashes.progress")
	if rebuilding.Current != 0 || rebuilding.Total != 2 {
		t.Fatalf("逐本进度动了**阶段**计数：%d/%d, want 0/2", rebuilding.Current, rebuilding.Total)
	}
	if rebuilding.MessageParams["updated"] != "2" || rebuilding.Metrics["processed_books"] != 2 {
		t.Fatalf("逐本计数没走占位参数与指标：params=%v metrics=%v", rebuilding.MessageParams, rebuilding.Metrics)
	}

	reconcileStart := publishedTaskWithCode(t, snapshots(), key, "task.msg.refresh_koreader_matching.reconcile_start")
	if reconcileStart.Phase != "reconciling_progress" || reconcileStart.Current != 1 || reconcileStart.Total != 2 {
		t.Fatalf("第二阶段开工帧为 phase=%q %d/%d, want reconciling_progress 1/2", reconcileStart.Phase, reconcileStart.Current, reconcileStart.Total)
	}
	if reconcileStart.MessageParams["updated"] != "2" || reconcileStart.MessageParams["total"] != "2" {
		t.Fatalf("第二阶段开工帧没带上第一阶段的成果：%v", reconcileStart.MessageParams)
	}

	reconciling := publishedTaskWithCode(t, snapshots(), key, "task.msg.reconcile_koreader_progress.progress")
	if reconciling.Current != 1 || reconciling.Phase != "reconciling_progress" {
		t.Fatalf("逐条对账动了阶段计数或阶段名：%d/%d phase=%q", reconciling.Current, reconciling.Total, reconciling.Phase)
	}
	if reconciling.Metrics["processed_progress"] != 2 {
		t.Fatalf("对账指标为 %v, want processed_progress=2", reconciling.Metrics)
	}

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "completed" || task.MessageCode != "task.msg.refresh_koreader_matching.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...refresh_koreader_matching.complete", task.Status, task.MessageCode)
	}
	if task.MessageParams["updatedBooks"] != "2" || task.MessageParams["totalBooks"] != "2" ||
		task.MessageParams["updatedProgress"] != "0" || task.MessageParams["totalProgress"] != "2" {
		t.Fatalf("完成文案的四个占位参数为 %v", task.MessageParams)
	}
	if len(store.identities) != 2 {
		t.Fatalf("落库了 %d 本书的**指纹**, want 2", len(store.identities))
	}
}

// TestKOReaderTasksCarryMatchConfigMetadata 守三个任务诞生那一刻就带齐匹配配置：任务面板按
// match_mode 与 path_ignore_extension 渲染「路径索引 / 二进制哈希索引」那块标签，缺一个键就
// 回落到默认标签，用户看到的索引类型是错的。并发上限那一列的有无同样按转换前的样子。
func TestKOReaderTasksCarryMatchConfigMetadata(t *testing.T) {
	cases := []struct {
		name      string
		key       string
		launch    func(*Controller) error
		wantLimit bool
	}{
		{"指纹重建", "rebuild_book_hashes", (*Controller).launchRebuildBookHashesTask, true},
		{"进度对账", "reconcile_koreader_progress", (*Controller).launchReconcileKOReaderProgressTask, false},
		{"匹配刷新", "refresh_koreader_matching", (*Controller).launchRefreshKOReaderMatchingTask, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots := newKOReaderTaskRig(t, &koreaderTaskStore{})

			if err := tc.launch(c); err != nil {
				t.Fatalf("启动任务失败: %v", err)
			}

			first := firstPublishedTask(t, snapshots(), tc.key)
			if first.Params["match_mode"] != config.KOReaderMatchModeFilePath || first.Params["path_ignore_extension"] != "true" {
				t.Fatalf("首帧的匹配配置元数据为 %v", first.Params)
			}
			if (first.EffectiveLimit != nil) != tc.wantLimit {
				t.Fatalf("首帧的并发上限存在性为 %v, want %v", first.EffectiveLimit != nil, tc.wantLimit)
			}
		})
	}
}

// TestRebuildBookHashesFrameIsPublishedWhole 守一次进度回调只投递一条载荷，且那条载荷内部自洽：
// 计数、指标与占位参数都来自同一本书。拆成 Advance / Metrics 两次分报即变红——后一次会被投递
// 水位吞掉（阶段与文案码一字未变），载荷里的指标就此停在上一本（撕开的样子见 TaskProgress.Report）。
func TestRebuildBookHashesFrameIsPublishedWhole(t *testing.T) {
	store := &koreaderTaskStore{candidates: seedKOReaderCandidates(2)}
	c, snapshots := newKOReaderTaskRig(t, store)

	if err := c.launchRebuildBookHashesTask(); err != nil {
		t.Fatalf("启动**指纹**重建失败: %v", err)
	}
	const key = "rebuild_book_hashes"

	progress := publishedTaskWithCode(t, snapshots(), key, "task.msg.koreader_rebuild_hashes.progress")
	if progress.Current != 2 || progress.Total != 2 || progress.Phase != "hashing" {
		t.Fatalf("**计数推进**为 %d/%d, phase=%q, want 2/2 hashing", progress.Current, progress.Total, progress.Phase)
	}
	if progress.Metrics["processed_books"] != int64(progress.Current) {
		t.Fatalf("指标与计数不是同一本书：metrics=%v current=%d", progress.Metrics, progress.Current)
	}
	if progress.MessageParams["updated"] != strconv.Itoa(progress.Current) {
		t.Fatalf("占位参数与计数不是同一本书：params=%v current=%d", progress.MessageParams, progress.Current)
	}

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "completed" || task.MessageCode != "task.msg.koreader_rebuild_hashes.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...koreader_rebuild_hashes.complete", task.Status, task.MessageCode)
	}
	if task.MessageParams["updated"] != "2" || task.MessageParams["total"] != "2" {
		t.Fatalf("完成文案的占位参数为 %v, want updated=2 total=2", task.MessageParams)
	}
}

// TestRebuildBookHashesReportsDiskWorkIO 守本包这一侧的接线：每本读盘的实况要从 koreader 的批
// 循环流回本包的任务指标，并落到**任务参数**那条通道上——存储 IO 面板按参数名读它，帧里的指标
// 到不了那一行。
//
// 断言压在档位、卷键与「计算哈希」上而不是等待毫秒上：无人争用时等待本就是 0，而这三项只要
// 接线断了就一起消失。
func TestRebuildBookHashesReportsDiskWorkIO(t *testing.T) {
	bookPath := filepath.Join(t.TempDir(), "vol1.cbz")
	if err := os.WriteFile(bookPath, []byte("book"), 0o644); err != nil {
		t.Fatalf("写书文件失败: %v", err)
	}
	store := &koreaderTaskStore{candidates: []database.BookIdentityCandidate{{ID: 1, Path: bookPath}}}
	c, snapshots := newKOReaderTaskRigWithMode(t, store, config.KOReaderMatchModeBinaryHash)

	if err := c.launchRebuildBookHashesTask(); err != nil {
		t.Fatalf("启动**指纹**重建失败: %v", err)
	}

	task := lastPublishedTask(t, snapshots(), "rebuild_book_hashes")
	if task.Params["hashed_files"] != "1" {
		t.Fatalf("任务参数里 hashed_files=%q, want \"1\" —— 磁盘作业的实况没有汇回任务指标：%v", task.Params["hashed_files"], task.Params)
	}
	if task.Params["storage_profile"] == "" || task.Params["volume_key"] == "" {
		t.Fatalf("存储档位与卷键没落到**任务参数**上：%v", task.Params)
	}
}

// TestReconcileProgressCompletesWithCounts 走完一遍进度对账：逐条计数经句柄落地，**完成**文案
// 带上「对上了几条 / 共几条」。一条都没对上不是失败——那正是这个任务最常见的结果。
func TestReconcileProgressCompletesWithCounts(t *testing.T) {
	store := &koreaderTaskStore{unmatched: seedUnmatchedProgress(3)}
	c, snapshots := newKOReaderTaskRig(t, store)

	if err := c.launchReconcileKOReaderProgressTask(); err != nil {
		t.Fatalf("启动进度对账失败: %v", err)
	}
	const key = "reconcile_koreader_progress"

	progress := publishedTaskWithCode(t, snapshots(), key, "task.msg.reconcile_koreader_progress.progress")
	if progress.Current != 3 || progress.Total != 3 || progress.Phase != "reconciling_progress" {
		t.Fatalf("**计数推进**为 %d/%d, phase=%q, want 3/3 reconciling_progress", progress.Current, progress.Total, progress.Phase)
	}
	if progress.Metrics["processed_progress"] != int64(progress.Current) {
		t.Fatalf("指标与计数不是同一条记录：metrics=%v current=%d", progress.Metrics, progress.Current)
	}

	task := lastPublishedTask(t, snapshots(), key)
	if task.Status != "completed" || task.MessageCode != "task.msg.reconcile_koreader_progress.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...reconcile_koreader_progress.complete", task.Status, task.MessageCode)
	}
	if task.MessageParams["updated"] != "0" || task.MessageParams["total"] != "3" {
		t.Fatalf("完成文案的占位参数为 %v, want updated=0 total=3", task.MessageParams)
	}
}

// TestRebuildBookHashesCancellationLandsCancelled 守单阶段任务的取消分支：任务体把取消错误返回
// 上去，由引擎裁决**终态**，任务体自己不写终态。
func TestRebuildBookHashesCancellationLandsCancelled(t *testing.T) {
	c, snapshots := newKOReaderTaskRig(t, &koreaderTaskStore{listIdentityErr: context.Canceled})

	if err := c.launchRebuildBookHashesTask(); err != nil {
		t.Fatalf("启动**指纹**重建失败: %v", err)
	}

	task := lastPublishedTask(t, snapshots(), "rebuild_book_hashes")
	if task.Status != "cancelled" || task.MessageCode != "task.msg.koreader_rebuild_hashes.cancelled" {
		t.Fatalf("终态为 %q / %q, want cancelled + ...koreader_rebuild_hashes.cancelled", task.Status, task.MessageCode)
	}
}

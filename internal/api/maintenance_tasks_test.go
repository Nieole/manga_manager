// 守维护子域四个任务经启动入口之后的两件事：三条**终态**分支落对了，以及进度是一整帧报出去的。
//
// 终态分支破了，用户看到的是「重建索引失败」却不知道哪个索引没建成，或者一次关服把任务写成失败；
// 帧撕开了，任务面板上的指标与进度条来自两个不同的时刻。

package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	ksvc "manga-manager/internal/koreader"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskrun"
)

// maintenanceStore 只实现维护任务体真正会调到的那几个方法。其余方法留给内嵌的 nil 接口——
// 任务体若走到没实现的那条路会当场炸掉，而不是拿到一个零值继续跑完。
type maintenanceStore struct {
	database.Store

	libraries      []database.Library
	seriesIndexErr error
	bookIndexErr   error

	coverPaths []string

	candidates []database.BookIdentityCandidate
	listErr    error
	updated    []database.UpdateBookIdentityParams
	// askedMatchMode 记下最后一次被问到的匹配模式：全量哈希回填按哪一档盘算缺口，只有从这里看得见。
	askedMatchMode string
}

func (s *maintenanceStore) ListLibraries(context.Context) ([]database.Library, error) {
	return s.libraries, nil
}

func (s *maintenanceStore) RebuildSeriesSearchIndex(context.Context) error { return s.seriesIndexErr }

func (s *maintenanceStore) RebuildBookSearchIndex(context.Context) error { return s.bookIndexErr }

func (s *maintenanceStore) ForEachReferencedCoverPath(_ context.Context, fn func(string) error) error {
	for _, path := range s.coverPaths {
		if err := fn(path); err != nil {
			return err
		}
	}
	return nil
}

func (s *maintenanceStore) CountBooksMissingQuickHash(context.Context) (int64, error) {
	return int64(len(s.candidates)), nil
}

func (s *maintenanceStore) ListBooksMissingQuickHashBatch(_ context.Context, afterID int64, _ int) ([]database.BookIdentityCandidate, error) {
	return s.batch(afterID)
}

func (s *maintenanceStore) CountBooksMissingIdentity(_ context.Context, matchMode string) (int64, error) {
	s.askedMatchMode = matchMode
	return int64(len(s.candidates)), nil
}

func (s *maintenanceStore) ListBooksMissingIdentityBatch(_ context.Context, matchMode string, afterID int64, _ int) ([]database.BookIdentityCandidate, error) {
	s.askedMatchMode = matchMode
	return s.batch(afterID)
}

// batch 按游标切一批：生产的两个回填循环都靠「返回空批」收尾，返回全量会让它们永远转下去。
func (s *maintenanceStore) batch(afterID int64) ([]database.BookIdentityCandidate, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []database.BookIdentityCandidate
	for _, candidate := range s.candidates {
		if candidate.ID > afterID {
			out = append(out, candidate)
		}
	}
	return out, nil
}

func (s *maintenanceStore) UpdateBookIdentity(_ context.Context, arg database.UpdateBookIdentityParams) error {
	s.updated = append(s.updated, arg)
	return nil
}

// newMaintenanceRig 拼出维护任务体需要的那几样：任务引擎（仍经它唯一的 seam 构造，后台能力换成
// 同步执行版）、存储与配置。扫描器按需另装，只有缩略图清理会用到。
// newMaintenanceRig 拼出维护任务体需要的那几样。tune 可改写配置——全量哈希回填是否跟着配置里的
// 匹配模式走，只有把它改掉才看得出来。
func newMaintenanceRig(t *testing.T, store database.Store, tune ...func(*config.Config)) (*Controller, func() []TaskStatus, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	e, snapshots := newBackgroundTestEngine(runTaskBodySynchronously)
	e.now = clock.Now

	cfg := &config.Config{}
	cfg.Cache.Dir = t.TempDir()
	for _, apply := range tune {
		apply(cfg)
	}
	config.NormalizeConfig(cfg)

	manager := config.NewManager(cfg)
	c := &Controller{taskEngine: e, store: store, config: manager}
	// 两处哈希回填走**磁盘作业**入口。调度器**必须**新建而不能用包级实例：后者按卷计数，
	// 用例之间会经它互相污染。
	c.diskWork = diskwork.NewRunner(c.currentConfig, storageio.NewScheduler())
	// 全量哈希回填走的是 KOReader 那套**指纹**重建，两者共用同一个仓储。
	c.koreader = ksvc.NewService(store, manager)
	// 引擎交给任务体的**任务句柄**要能发起磁盘作业：全量哈希回填的每一本书都经它读。
	e.diskWork = c.diskWork
	return c, snapshots, clock
}

// recordingTaskHandle 是两处哈希回填的批循环收下的**任务句柄**的用例替身：上报的三条通道都
// 接空，只记下**计数推进**来过几次以及那一刻的 IO 实况；**磁盘作业**交给内嵌的真句柄，
// 「已哈希文件数」这条计数规则因此在用例里真的走了一遍。
type recordingTaskHandle struct {
	*taskrun.Handle
	advances int
	lastIO   taskrun.IOMetrics
}

func (h *recordingTaskHandle) Advance(current, total int) {
	h.advances++
	h.lastIO = h.IOMetrics()
}

// newRecordingTaskHandle 造一个不挂在任何任务上的句柄：它不经启动入口，因此写不进任务表，
// 上报去向空处；给那些只关心批循环本身、不关心上报的用例用。
func newRecordingTaskHandle(disk *diskwork.Runner) *recordingTaskHandle {
	return &recordingTaskHandle{Handle: taskrun.New(
		func(taskrun.Frame) {},
		func(map[string]string) {},
		func(map[string]int64, map[string]string) {},
		disk,
	)}
}

// TestRebuildIndexNamesTheFailedIndex 守两步索引重灌各自的失败文案码：只报一句「重建索引失败」
// 的话，用户不知道该去查系列索引还是书籍索引，而技术错误串本身不区分二者。
func TestRebuildIndexNamesTheFailedIndex(t *testing.T) {
	cases := []struct {
		name     string
		store    *maintenanceStore
		wantCode string
	}{
		{"系列索引", &maintenanceStore{seriesIndexErr: errors.New("fts5 corrupt")}, "task.msg.rebuild_index.series_failed"},
		{"书籍索引", &maintenanceStore{bookIndexErr: errors.New("fts5 corrupt")}, "task.msg.rebuild_index.book_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, snapshots, _ := newMaintenanceRig(t, tc.store)

			if err := c.launchRebuildIndexTask(); err != nil {
				t.Fatalf("启动索引重建失败: %v", err)
			}

			task := lastPublishedTask(t, snapshots(), "rebuild_index")
			if task.Status != "failed" {
				t.Fatalf("任务停在 %q, want failed", task.Status)
			}
			if task.MessageCode != tc.wantCode {
				t.Fatalf("失败文案码为 %q, want %q", task.MessageCode, tc.wantCode)
			}
			if task.Error != "fts5 corrupt" {
				t.Fatalf("技术错误串没有留在任务上：%q", task.Error)
			}
		})
	}
}

// TestRebuildIndexCancellationOutranksTheStepCode 守「专属失败文案必须挡在取消之外」：
// 取消同样以 ctx.Err() 的形式从索引重灌里返回，无条件带上专属码的话，一次关服会让用户看到
// 「系列索引重建失败」而不是取消。
func TestRebuildIndexCancellationOutranksTheStepCode(t *testing.T) {
	c, snapshots, _ := newMaintenanceRig(t, &maintenanceStore{seriesIndexErr: context.Canceled})

	if err := c.launchRebuildIndexTask(); err != nil {
		t.Fatalf("启动索引重建失败: %v", err)
	}

	task := lastPublishedTask(t, snapshots(), "rebuild_index")
	if task.Status != "cancelled" {
		t.Fatalf("任务停在 %q, want cancelled", task.Status)
	}
	if task.MessageCode != "task.msg.rebuild_index.cancelled" {
		t.Fatalf("取消文案码为 %q, want ...rebuild_index.cancelled", task.MessageCode)
	}
}

// TestRebuildIndexCompletes 守常规路径：两个索引都重灌成功、全库扫描跑完，任务落**完成**。
func TestRebuildIndexCompletes(t *testing.T) {
	c, snapshots, _ := newMaintenanceRig(t, &maintenanceStore{})

	if err := c.launchRebuildIndexTask(); err != nil {
		t.Fatalf("启动索引重建失败: %v", err)
	}

	task := lastPublishedTask(t, snapshots(), "rebuild_index")
	if task.Status != "completed" || task.MessageCode != "task.msg.rebuild_index.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...rebuild_index.complete", task.Status, task.MessageCode)
	}
	if err := c.launchRebuildIndexTask(); err != nil {
		t.Fatalf("落定终态之后同一任务键起不来了: %v", err)
	}
}

// TestCleanupThumbnailsReportsPhaseThenCounts 守缩略图清理的两类上报各走各的方法：
// 开工那一帧只播**阶段**、不碰计数，逐条目那帧才动计数；两帧的文案都是 i18n 码，
// 扫描器一侧只交计数、不渲染文字。
func TestCleanupThumbnailsReportsPhaseThenCounts(t *testing.T) {
	store := &maintenanceStore{}
	c, snapshots, _ := newMaintenanceRig(t, store)
	cfg := c.currentConfig()
	c.scanner = scanner.NewScanner(store, config.NewManager(&cfg))

	for _, name := range []string{"a.webp", "b.webp", "c.webp"} {
		if err := os.WriteFile(filepath.Join(cfg.Cache.Dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("造缩略图文件失败: %v", err)
		}
	}

	if err := c.launchCleanupThumbnailsTask(); err != nil {
		t.Fatalf("启动缩略图清理失败: %v", err)
	}

	opening := publishedTaskWithCode(t, snapshots(), "cleanup_thumbnails", "task.msg.cleanup_thumbnails.scanning")
	if opening.Phase != "cleanup" {
		t.Fatalf("开工那一帧的**阶段**为 %q, want cleanup", opening.Phase)
	}
	if opening.Current != 0 || opening.Total != 0 {
		t.Fatalf("开工那一帧动了计数：%d/%d", opening.Current, opening.Total)
	}

	progress := publishedTaskWithCode(t, snapshots(), "cleanup_thumbnails", "task.msg.cleanup_thumbnails.progress")
	if progress.Current != 3 || progress.Total != 3 {
		t.Fatalf("清理进度为 %d/%d, want 3/3", progress.Current, progress.Total)
	}
	if progress.MessageParams["deleted"] != "3" || progress.MessageParams["scanned"] != "3" {
		t.Fatalf("清理计数没有走占位参数：%v", progress.MessageParams)
	}

	task := lastPublishedTask(t, snapshots(), "cleanup_thumbnails")
	if task.Status != "completed" || task.MessageCode != "task.msg.cleanup_thumbnails.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...cleanup_thumbnails.complete", task.Status, task.MessageCode)
	}
}

// TestHashProgressFrameIsPublishedWhole 守一次哈希进度只投递一条载荷，且那条载荷内部自洽：
// 计数、阶段、指标与标签都来自同一本书。拆成 Advance / Phase / Metrics / Labels 四次分报即变红
// （撕开之后是什么样见 taskrun.Handle.Report）。
func TestHashProgressFrameIsPublishedWhole(t *testing.T) {
	c, snapshots, clock := newMaintenanceRig(t, &maintenanceStore{})
	const key = "rebuild_file_identities"
	progress := seedTask(t, c.taskEngine, taskSeed{Key: key, Type: key, CanCancel: true, CanPause: true})

	metrics := taskrun.IOMetrics{StorageProfile: "hdd_external", VolumeKey: "/srv", IOWaitMillis: 120, PausedMillis: 30, HashedFiles: 7}
	before := publishedCountFor(snapshots(), key)
	reportHashProgress(progress, 7, 40, "task.msg.rebuild_file_identities.progress", metrics)

	if got := publishedCountFor(snapshots(), key) - before; got != 1 {
		t.Fatalf("一次哈希进度投递了 %d 条载荷, want 1", got)
	}
	task := lastPublishedTask(t, snapshots(), key)
	if task.Current != 7 || task.Total != 40 {
		t.Fatalf("**计数推进**为 %d/%d, want 7/40", task.Current, task.Total)
	}
	if task.Phase != "hashing" || task.MessageCode != "task.msg.rebuild_file_identities.progress" {
		t.Fatalf("阶段/文案码没落地：phase=%q code=%q", task.Phase, task.MessageCode)
	}
	if task.Metrics["hashed_files"] != 7 || task.Metrics["io_wait_ms"] != 120 || task.Metrics["paused_ms"] != 30 {
		t.Fatalf("指标与计数不是同一本书：%v", task.Metrics)
	}
	if task.Labels["storage_profile"] != "hdd_external" || task.Labels["volume_key"] != "/srv" {
		t.Fatalf("存储画像标签没落地：%v", task.Labels)
	}

	// IO 参数走的是另一条通道（TaskStatus.Params），存储 IO 面板按参数名读它；
	// 它与上面那一帧各自投递一次，因此要推过节流窗口才看得见。
	clock.advance(taskProgressPublishInterval * 2)
	reportHashProgress(progress, 8, 40, "task.msg.rebuild_file_identities.progress", metrics)
	if task := lastPublishedTask(t, snapshots(), key); task.Params["hashed_files"] != "7" || task.Params["io_wait_ms"] != "120" {
		t.Fatalf("IO 参数没有走到**任务参数**那条通道：%v", task.Params)
	}
}

// TestRebuildFileIdentitiesCompletesWithCounts 走完一遍文件身份重建：任务声明首帧带齐元数据与
// 并发上限，逐本进度经句柄落地，**完成**文案带上 updated / total 两个占位参数。
func TestRebuildFileIdentitiesCompletesWithCounts(t *testing.T) {
	store := &maintenanceStore{candidates: seedIdentityCandidates(t, 2)}
	c, snapshots, _ := newMaintenanceRig(t, store)

	if err := c.launchRebuildFileIdentitiesTask(); err != nil {
		t.Fatalf("启动文件身份重建失败: %v", err)
	}

	first := firstPublishedTask(t, snapshots(), "rebuild_file_identities")
	if first.Params["profile"] != "quick_hash" {
		t.Fatalf("首帧没带上元数据：%v", first.Params)
	}
	if first.EffectiveLimit == nil {
		t.Fatal("首帧没带上并发上限 —— 任务面板上会缺一块")
	}
	if len(store.updated) != 2 {
		t.Fatalf("落库了 %d 本书的身份, want 2", len(store.updated))
	}

	task := lastPublishedTask(t, snapshots(), "rebuild_file_identities")
	if task.Status != "completed" || task.MessageCode != "task.msg.rebuild_file_identities.complete" {
		t.Fatalf("终态为 %q / %q, want completed + ...complete", task.Status, task.MessageCode)
	}
	if task.MessageParams["updated"] != "2" || task.MessageParams["total"] != "2" {
		t.Fatalf("完成文案的占位参数为 %v, want updated=2 total=2", task.MessageParams)
	}
}

// TestRebuildFileIdentitiesCancellationLandsCancelled 守取消分支：任务体把取消错误返回上去，
// 由引擎裁决终态，任务体自己不写终态。
func TestRebuildFileIdentitiesCancellationLandsCancelled(t *testing.T) {
	c, snapshots, _ := newMaintenanceRig(t, &maintenanceStore{listErr: context.Canceled})

	if err := c.launchRebuildFileIdentitiesTask(); err != nil {
		t.Fatalf("启动文件身份重建失败: %v", err)
	}

	task := lastPublishedTask(t, snapshots(), "rebuild_file_identities")
	if task.Status != "cancelled" || task.MessageCode != "task.msg.rebuild_file_identities.cancelled" {
		t.Fatalf("终态为 %q / %q, want cancelled + ...cancelled", task.Status, task.MessageCode)
	}
}

// TestBackfillFullHashesPinsBinaryHashMode 守低优先级回填的口径：它由**资料库扫描**收尾时按
// 「缺全量哈希的书有几本」发起，任务声明里报的也是这一档，因此不跟着配置里的匹配模式走——
// 配置是路径模式时它仍然补全量哈希，而不是掉头去写路径**指纹**、把总数也换成另一批书。
func TestBackfillFullHashesPinsBinaryHashMode(t *testing.T) {
	store := &maintenanceStore{candidates: seedIdentityCandidates(t, 1)}
	c, _, _ := newMaintenanceRig(t, store, func(cfg *config.Config) {
		cfg.KOReader.MatchMode = config.KOReaderMatchModeFilePath
	})

	updated, total, err := c.runBackfillFullHashesLowPriority(context.Background(), 500, 0, newRecordingTaskHandle(c.diskWork))
	if err != nil {
		t.Fatalf("回填返回 %v, want nil", err)
	}
	if updated != 1 || total != 1 {
		t.Fatalf("回填结果为 updated=%d total=%d, want 1/1", updated, total)
	}
	if store.askedMatchMode != config.KOReaderMatchModeBinaryHash {
		t.Fatalf("按 %q 盘算缺口, want %q —— 配置改成路径模式不该让它改口径", store.askedMatchMode, config.KOReaderMatchModeBinaryHash)
	}
	if len(store.updated) != 1 || store.updated[0].FileHash == "" || store.updated[0].PathFingerprint != "" {
		t.Fatalf("落库的是 %+v, want 只写 file_hash", store.updated)
	}
}

// TestHashBackfillStartsFromInsideATaskBody 守**串联**：低优先级哈希回填由资料库扫描的任务体
// 在收尾前发起，是唯一一个由另一个任务的任务体启动的任务。启动入口若在任务体内不可用
// （比如死锁或静默丢弃），扫描完成后就再也不会有人补算哈希。
func TestHashBackfillStartsFromInsideATaskBody(t *testing.T) {
	store := &maintenanceStore{candidates: seedIdentityCandidates(t, 1)}
	c, snapshots, _ := newMaintenanceRig(t, store)
	cfg := c.currentConfig()
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeBinaryHash
	c.config = config.NewManager(&cfg)

	var chainErr error
	if err := c.taskEngine.Run(TaskSpec{Key: "scan_library_1", Type: "scan_library"}, func(context.Context, *taskrun.Handle) (TaskResult, error) {
		chainErr = c.launchLowPriorityBookHashBackfillTask("scan_library")
		return TaskResult{}, nil
	}); err != nil {
		t.Fatalf("启动资料库扫描失败: %v", err)
	}

	if chainErr != nil {
		t.Fatalf("扫描任务体没能发起哈希回填（%v）—— 扫描完成后再也不会有人补算哈希", chainErr)
	}
	task := lastPublishedTask(t, snapshots(), lowPriorityBookHashTaskKey)
	if task.Status != "completed" || task.MessageCode != "task.msg.book_hash_backfill.complete" {
		t.Fatalf("回填终态为 %q / %q, want completed + ...book_hash_backfill.complete", task.Status, task.MessageCode)
	}
	if task.Params["reason"] != "scan_library" {
		t.Fatalf("发起理由没有落进任务参数：%v", task.Params)
	}
}

// TestHashBackfillStaysSilentWhenNothingIsMissing 守前置条件仍然挡在启动之前：没有书缺哈希时
// 一条任务都不该出现，否则每次扫描收尾都会在任务中心留下一条空跑的回填。
func TestHashBackfillStaysSilentWhenNothingIsMissing(t *testing.T) {
	c, snapshots, _ := newMaintenanceRig(t, &maintenanceStore{})
	cfg := c.currentConfig()
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeBinaryHash
	c.config = config.NewManager(&cfg)

	if err := c.launchLowPriorityBookHashBackfillTask("scan_library"); err != nil {
		t.Fatalf("没有书缺哈希不是错误，却返回了 %v", err)
	}
	if got := publishedCountFor(snapshots(), lowPriorityBookHashTaskKey); got != 0 {
		t.Fatalf("没有书缺哈希却发起了回填任务，投递了 %d 条载荷", got)
	}
}

// seedIdentityCandidates 造 n 本指向真实临时文件的候选书：哈希任务体要真的读到文件才会
// 上报进度并落库，指向不存在的路径只会走进「记一条警告然后跳过」那条分支。
//
// LibraryPath 留空无妨：**磁盘作业**的卷键与存储策略都由书自己的路径决定，不看它。
func seedIdentityCandidates(t *testing.T, n int) []database.BookIdentityCandidate {
	t.Helper()
	dir := t.TempDir()
	candidates := make([]database.BookIdentityCandidate, 0, n)
	for i := 1; i <= n; i++ {
		path := filepath.Join(dir, "vol"+string(rune('0'+i))+".cbz")
		if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
			t.Fatalf("造书文件失败: %v", err)
		}
		candidates = append(candidates, database.BookIdentityCandidate{ID: int64(i), Path: path})
	}
	return candidates
}

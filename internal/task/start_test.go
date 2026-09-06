// 守启动仪式：身份四要素必须显式给全，整份声明原子落地，运行句柄的三条写入通道各自接对了地方。
// 声明拆成启动之后的多次补写会留下一个「运行已经在列表里、却还没有上限与入参」的窗口。

package task

import (
	"context"
	"errors"
	"testing"

	"manga-manager/internal/taskrun"
)

func TestIdentityMustBeCompleteAndConsistent(t *testing.T) {
	cases := []struct {
		name string
		id   Identity
	}{
		{"缺类型", Identity{Scope: ScopeLibrary, ScopeID: 1}},
		{"未知作用域", Identity{Type: "scan_library", Scope: "shelf", ScopeID: 1}},
		{"库级缺作用域 id", Identity{Type: "scan_library", Scope: ScopeLibrary}},
		{"系列级缺作用域 id", Identity{Type: "scan_series", Scope: ScopeSeries}},
		{"系统级却带了作用域 id", Identity{Type: "cleanup", Scope: ScopeSystem, ScopeID: 3}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.id.Validate(); !errors.Is(err, ErrInvalidIdentity) {
				t.Fatalf("Validate() = %v, want ErrInvalidIdentity", err)
			}
		})
	}
}

func TestSystemScopeIdentityIsValid(t *testing.T) {
	id := Identity{Type: "prune_runs", Scope: ScopeSystem, Variant: VariantPrimary}
	if err := id.Validate(); err != nil {
		t.Fatalf("系统级身份被判为无效: %v", err)
	}
}

// TestVariantMakesASecondIdentity 守**变体**是第二个身份而不是同一个任务的第二次运行——
// 跑法不同的两条工作因此可以同时在跑，而不是互相挡住。
func TestVariantMakesASecondIdentity(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)

	foreground := libraryScanSpec(1)
	foreground.Identity.Type = "rebuild_book_hashes"
	backfill := foreground
	backfill.Identity.Variant = "backfill"

	first := h.start(t, foreground, idleBody)
	second := h.start(t, backfill, idleBody)

	if first.TaskID == second.TaskID {
		t.Fatal("两个变体被并成了同一个任务，重启函数会把回填重启成前台档")
	}
	if got := h.load(t, second.ID).Status; got != StatusRunning {
		t.Fatalf("第二个变体被闸门挡下了，状态为 %q", got)
	}
}

// TestSameIdentityReusesTheSameTaskRow 守身份按四要素懒建，重复发起不会建出第二条身份。
func TestSameIdentityReusesTheSameTaskRow(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	first := h.start(t, libraryScanSpec(1), idleBody)
	second := h.start(t, libraryScanSpec(1), idleBody)

	if first.TaskID != second.TaskID {
		t.Fatalf("同一身份建出了两条任务：%d 与 %d", first.TaskID, second.TaskID)
	}
}

func TestTriggerMustBeDeclared(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)

	spec := libraryScanSpec(1)
	spec.Trigger = ""
	if _, err := h.engine.Start(context.Background(), spec, idleBody); !errors.Is(err, ErrInvalidRunSpec) {
		t.Fatalf("缺发起方的声明返回 %v, want ErrInvalidRunSpec", err)
	}

	spec.Trigger = "cron"
	if _, err := h.engine.Start(context.Background(), spec, idleBody); !errors.Is(err, ErrInvalidRunSpec) {
		t.Fatalf("未知发起方的声明返回 %v, want ErrInvalidRunSpec", err)
	}
}

// TestSpecLandsAtomically 守整份声明在首帧之前就已落地：上限、入参、标签、总数、文案码
// 一样都不能等到启动之后再补写。
func TestSpecLandsAtomically(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)

	spec := libraryScanSpec(1)
	spec.Trigger = TriggerScheduled
	spec.StartCode = "task.msg.scan.started"
	spec.Args = map[string]string{"force": "true"}
	spec.Labels = map[string]string{"source": "anilist"}
	spec.Limits = Limits{ScanConcurrency: 4, StorageProfile: "hdd"}

	run := h.start(t, spec, idleBody)

	first := h.snapshots()[0]
	if first.Run.ID != run.ID {
		t.Fatalf("首帧不是这条运行：%d", first.Run.ID)
	}
	if first.Run.Trigger != TriggerScheduled || first.Run.Total != 100 || first.Run.MessageCode != "task.msg.scan.started" {
		t.Fatalf("首帧缺字段：trigger=%q total=%d code=%q", first.Run.Trigger, first.Run.Total, first.Run.MessageCode)
	}
	if !first.Capabilities.CanPause || !first.Capabilities.CanCancel {
		t.Fatalf("首帧没带上控制能力：%+v", first.Capabilities)
	}
	if got := h.store.argsOf(run.ID)["force"]; got != "true" {
		t.Fatalf("重启入参没落地：%q", got)
	}
	if got := h.store.labelsOf(run.ID)["source"]; got != "anilist" {
		t.Fatalf("展示标签没落地：%q", got)
	}
	if got := h.store.limitsOf(run.ID); got.ScanConcurrency != 4 || got.StorageProfile != "hdd" {
		t.Fatalf("并发上限没落地：%+v", got)
	}
}

// TestZeroLimitsAreNotPersisted 守零值上限不落盘：多数维护类工作没有上限可报，
// 凭空落一份全零的上限会让界面显示「并发 0」。
func TestZeroLimitsAreNotPersisted(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	run := h.start(t, libraryScanSpec(1), idleBody)

	if got := h.store.limitsOf(run.ID); got != (Limits{}) {
		t.Fatalf("没声明上限却落了一份：%+v", got)
	}
}

// TestHandleChannelsLandInTheirOwnPlaces 守**运行句柄**的三条写入通道各自接对了地方：
// 整帧走运行行，入参走重启入参，指标走累加。接反不会有编译错误。
func TestHandleChannelsLandInTheirOwnPlaces(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)

	run := h.start(t, libraryScanSpec(1), func(_ context.Context, handle *taskrun.Handle) (Result, error) {
		handle.Phase("hashing", "task.msg.scan.hashing", nil)
		handle.Report(taskrun.Frame{Item: "vol-01.cbz", Metrics: map[string]int64{"books": 12}})
		handle.MergeParams(map[string]string{"mode": "quick"})
		handle.AddMetrics(map[string]int64{"io_wait_ms": 30}, map[string]string{"volume": "disk-1"})
		handle.AddMetrics(map[string]int64{"io_wait_ms": 12}, nil)
		return Result{}, nil
	})

	settled := h.load(t, run.ID)
	if settled.Phase != "hashing" || settled.CurrentItem != "vol-01.cbz" {
		t.Fatalf("展示态没进运行行：phase=%q item=%q", settled.Phase, settled.CurrentItem)
	}
	args := h.store.argsOf(run.ID)
	if args["mode"] != "quick" || args["volume"] != "disk-1" {
		t.Fatalf("入参没进重启入参：%v", args)
	}
	metrics := h.store.metricsOf(run.ID)
	if metrics["books"] != 12 {
		t.Fatalf("整帧里的指标是「设」，得到 %d, want 12", metrics["books"])
	}
	// 跨资料库的报文只覆盖其中一个库，全局总量只能加出来：两次 30 与 12 必须是 42。
	if metrics["io_wait_ms"] != 42 {
		t.Fatalf("累加通道没累加，得到 %d, want 42", metrics["io_wait_ms"])
	}
}

// TestStartRejectsAMissingBody 守没有任务体的声明发不起来——那会建出一条永远不会推进的运行。
func TestStartRejectsAMissingBody(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	if _, err := h.engine.Start(context.Background(), libraryScanSpec(1), nil); !errors.Is(err, ErrInvalidRunSpec) {
		t.Fatalf("缺任务体的发起返回 %v, want ErrInvalidRunSpec", err)
	}
}

// TestSequenceIsMonotonicAndRestored 守序号单调且跨重启接得上：它是任务中心的主排序键，
// 从 0 重来的话新起的运行会排在全部历史之后，用户在第一页里一条新的都看不到。
func TestSequenceIsMonotonicAndRestored(t *testing.T) {
	h := newTestEngine(t, runBodySynchronously, 0)
	first := h.start(t, libraryScanSpec(1), idleBody)
	if got := h.load(t, first.ID).Sequence; got <= first.Sequence {
		t.Fatalf("终态没有取新序号：%d ≤ %d", got, first.Sequence)
	}

	h.start(t, libraryScanSpec(2), idleBody)
	highest, err := h.store.MaxRunSequence(context.Background())
	if err != nil {
		t.Fatalf("取最大序号失败: %v", err)
	}
	if highest == 0 {
		t.Fatal("库里一个序号都没有，这条用例守不住任何东西")
	}

	// 换一台引擎接着同一份落盘：它必须从库里已用掉的最大值往下发。
	restarted := New(Config{Store: h.store, RunBackground: registerOnly})
	restarted.mu.Lock()
	resumed := restarted.nextSequenceLocked()
	restarted.mu.Unlock()
	if resumed != highest+1 {
		t.Fatalf("新引擎发出的第一个序号是 %d, want %d", resumed, highest+1)
	}
}

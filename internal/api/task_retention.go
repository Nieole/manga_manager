// **分层保留**在 api 这一侧的落点：每天一次的节拍、三个阈值从运行时配置读，
// 以及清理本身那条运行的启动点与任务体。裁剪选中哪些行由 taskstore 回答。

package api

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

const (
	// cleanupRunHistoryTaskKey 是清理运行历史的**任务键**，同时也是它的任务类型名。
	cleanupRunHistoryTaskKey = "cleanup_run_history"

	// runHistoryCleanupInterval 是清理的节拍：每天一次。
	//
	// 一天一次而不是更密：它删的是阈值之外的那些历史，早一小时晚一小时没有区别，
	// 而每跑一次它自己也要占一个**运行槽位**、在任务中心留一行。
	runHistoryCleanupInterval = 24 * time.Hour

	// maxRetentionDays 是保留天数的上限（100 年），用来挡住溢出而不是表达策略。
	//
	// 天数是配置里的一个整数，乘成 time.Duration 会在十万天量级溢出 int64——绕回来的那个数
	// 可能是个很小的正数，那等于把全部历史当场删光。两个方向的溢出都要挡：负得足够多同样绕得回来，
	// 而配置文件是手写的。
	maxRetentionDays = 36500
)

// startRunHistoryJanitor 每天发起一次清理运行，随 Controller 生命周期退出
// （经 runBackground 登记 backgroundWG，关闭时会退出）。
//
// 启动就先跑一次而不是等满一天：一台每晚关机的机器永远等不到那个 tick，而它恰恰是最需要清理的
// 那一类——历史照常在长，清理却一次都不会发生。重复发起本身没有代价：撞上还没跑完的那条会进
// **排队中**或被**合并**，而清理运行自己也受同一套保留策略约束，不给它开特例。
func (c *Controller) startRunHistoryJanitor() {
	ticker := time.NewTicker(runHistoryCleanupInterval)
	defer ticker.Stop()
	for {
		if err := c.launchCleanupRunHistoryTask(); err != nil {
			slog.Warn("Run history cleanup could not be started", "error", err)
		}
		select {
		case <-c.lifecycleDone():
			return
		case <-ticker.C:
		}
	}
}

// retentionPolicyOf 把设置里的三个数翻成**分层保留**策略。
//
// 收的是一份配置快照而不是自己去读：调用方还要按同一份快照报出「这次按什么口径清的」，
// 各读各的会让报出来的阈值与真正生效的那份对不上。阈值在设置里可改，因此调用方每次现读一遍——
// 改完对**下一次清理**生效，不必重启（理由同 taskSlots）。
//
// 非正数一律交出 0，也就是「这一层不裁剪」（端口认的就是这条）。归一化本该已经把它们补成默认值，
// 这里再收一道是因为算错方向的代价不对称：多留一批历史只是占地方，少留一批是把用户的记录删了。
func retentionPolicyOf(cfg config.Config) task.RetentionPolicy {
	return task.RetentionPolicy{
		RunsPerTask: max(cfg.Tasks.RetainRunsPerTask, 0),
		TerminalAge: retentionAge(cfg.Tasks.RetainTerminalRunDays),
		SampleAge:   retentionAge(cfg.Tasks.RetainSampleDays),
	}
}

// retentionAge 把配置里的天数翻成时长：非正数交出 0，大到会溢出的按 maxRetentionDays 收。
func retentionAge(days int) time.Duration {
	if days > maxRetentionDays {
		days = maxRetentionDays
	}
	return time.Duration(max(days, 0)) * 24 * time.Hour
}

// launchCleanupRunHistoryTask 是清理运行历史的启动点，走引擎的启动入口。
//
// **发起方是串联**：它不是用户点的，也不为某个资料库而跑，而是这套模型自己带出来的一件收尾工作。
// 它建成一条**可见的运行**而不是一句静默的 DELETE，为的是「它什么时候跑的、清了多少」有据可查。
//
// 不可暂停、不可取消：整个裁剪是一笔事务，中间没有可中断点，报一个按不动的按钮比没有按钮更糟。
func (c *Controller) launchCleanupRunHistoryTask() error {
	cfg := c.currentConfig()
	policy := retentionPolicyOf(cfg)
	spec := RunSpec{
		Key:       cleanupRunHistoryTaskKey,
		StartCode: "task.msg.cleanup_run_history.start",
		Total:     1,
		// 这次清理实际生效的三个阈值，走**展示标签**那条通道：它们启动时就已知、整次运行不变，
		// 而且没有任何**重启函数**会去读它们——那是重启入参那一格的用途。
		// 落了它们，「这一次是按什么口径清的」在事后仍答得出：阈值改过之后再回头看这条运行，
		// 看到的是当时那一份。
		Labels: map[string]string{
			"runs_per_task":     strconv.Itoa(cfg.Tasks.RetainRunsPerTask),
			"terminal_run_days": strconv.Itoa(cfg.Tasks.RetainTerminalRunDays),
			"sample_days":       strconv.Itoa(cfg.Tasks.RetainSampleDays),
		},
		CompleteCode: "task.msg.cleanup_run_history.complete",
		CancelCode:   "task.msg.cleanup_run_history.cancelled",
		FailCode:     "task.msg.cleanup_run_history.failed",
	}

	return c.taskEngine.Run(systemTask(cleanupRunHistoryTaskKey, variantSole), task.TriggerChained, spec,
		func(ctx context.Context, handle *runhandle.Handle) (RunResult, error) {
			result, err := c.taskEngine.pruneHistory(ctx, policy)
			if err != nil {
				return RunResult{}, err
			}
			// 清了多少既进**指标**（可聚合查询），也进终态文案的占位参数（用户直接读得到）。
			// 一次运行只裁一次，因此这一帧握着的就是全量当前值。
			done := 1
			handle.Report(runhandle.Frame{
				Current: &done,
				Total:   &done,
				Phase:   "cleanup",
				Metrics: map[string]int64{
					"pruned_runs":    result.Runs,
					"pruned_events":  result.Events,
					"pruned_samples": result.Samples,
				},
			})
			return RunResult{Params: map[string]string{
				"runs":    strconv.FormatInt(result.Runs, 10),
				"events":  strconv.FormatInt(result.Events, 10),
				"samples": strconv.FormatInt(result.Samples, 10),
			}}, nil
		})
}

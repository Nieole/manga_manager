// **分层保留**在 api 这一侧的落点：每天一次的节拍、三个阈值从运行时配置读，
// 以及清理本身那条运行的启动点与任务体。裁剪选中哪些行由 taskstore 回答。

package api

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
)

const (
	// cleanupRunHistoryTaskKey 是清理运行历史的**任务键**，同时也是它的任务类型名。
	cleanupRunHistoryTaskKey = "cleanup_run_history"

	// runHistoryCleanupInterval 是清理的节拍：每天一次。
	//
	// 一天一次而不是更密：它删的是 90 天与 20 次之外的历史，早一小时晚一小时没有区别，
	// 而每跑一次它自己也要占一个**运行槽位**、在任务中心留一行。
	runHistoryCleanupInterval = 24 * time.Hour

	// maxRetentionDays 是保留天数的上限（100 年），用来挡住溢出而不是表达策略。
	//
	// 天数是配置里的一个整数，乘成 time.Duration 会在约 106751 天处溢出 int64——绕回来的那个数
	// 可能是个很小的正数，那等于把全部历史当场删光。要「永久保留」有专门的表达（负数），
	// 不必靠一个大到溢出的天数。
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

// runRetention 读此刻生效的**分层保留**三个阈值。
//
// 每次现读一遍配置快照，不在装配期取一份收起来：阈值在设置里可改，改完要对**下一次清理**生效
// （理由同 taskSlots）。天数为负即这一层不清理，原样交给落盘端口——那里认的就是「零或负数不裁剪」。
func (c *Controller) runRetention() task.RetentionPolicy {
	tasks := c.currentConfig().Tasks
	return task.RetentionPolicy{
		RunsPerTask: tasks.RetainRunsPerTask,
		TerminalAge: retentionDays(tasks.RetainTerminalRunDays),
		SampleAge:   retentionDays(tasks.RetainSampleDays),
	}
}

// retentionDays 把配置里的天数翻成时长，超出 maxRetentionDays 的按上限收。
func retentionDays(days int) time.Duration {
	if days > maxRetentionDays {
		days = maxRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// launchCleanupRunHistoryTask 是清理运行历史的启动点，走引擎的启动入口。
//
// **发起方是串联**：它不是用户点的，也不为某个资料库而跑，而是这套模型自己带出来的一件收尾工作。
// 它建成一条**可见的运行**而不是一句静默的 DELETE，为的是「它什么时候跑的、清了多少」有据可查。
//
// 不可暂停、不可取消：整个裁剪是一笔事务，中间没有可中断点，报一个按不动的按钮比没有按钮更糟。
func (c *Controller) launchCleanupRunHistoryTask() error {
	policy := c.runRetention()
	spec := RunSpec{
		Key:       cleanupRunHistoryTaskKey,
		StartCode: "task.msg.cleanup_run_history.start",
		Total:     1,
		// 这次清理实际生效的三个阈值。它们随运行落盘，因此「这一次是按什么口径清的」
		// 在事后仍答得出——阈值改过之后再回头看那条运行，看到的是当时那一份。
		Metadata: map[string]string{
			"runs_per_task":     strconv.Itoa(policy.RunsPerTask),
			"terminal_run_days": strconv.Itoa(int(policy.TerminalAge / (24 * time.Hour))),
			"sample_days":       strconv.Itoa(int(policy.SampleAge / (24 * time.Hour))),
		},
		CompleteCode: "task.msg.cleanup_run_history.complete",
		CancelCode:   "task.msg.cleanup_run_history.cancelled",
		FailCode:     "task.msg.cleanup_run_history.failed",
	}

	return c.taskEngine.Run(systemTask(cleanupRunHistoryTaskKey, variantSole), task.TriggerChained, spec,
		func(ctx context.Context, tp *runhandle.Handle) (TaskResult, error) {
			result, err := c.taskEngine.pruneHistory(ctx, policy)
			if err != nil {
				return TaskResult{}, err
			}
			// 清了多少既进**指标**（可聚合查询），也进终态文案的占位参数（用户直接读得到）。
			// 一次运行只裁一次，因此这一帧握着的就是全量当前值。
			done := 1
			tp.Report(runhandle.Frame{
				Current: &done,
				Total:   &done,
				Phase:   "cleanup",
				Metrics: map[string]int64{
					"pruned_runs":    result.Runs,
					"pruned_events":  result.Events,
					"pruned_samples": result.Samples,
				},
			})
			return TaskResult{Params: map[string]string{
				"runs":    strconv.FormatInt(result.Runs, 10),
				"events":  strconv.FormatInt(result.Events, 10),
				"samples": strconv.FormatInt(result.Samples, 10),
			}}, nil
		})
}

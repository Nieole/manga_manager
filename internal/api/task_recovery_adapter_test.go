package api

import (
	"context"
	"log/slog"
	"manga-manager/internal/task"
)

func (e *taskEngine) markInterrupted(ctx context.Context) {
	if e.runtime != nil {
		if err := e.runtime.RecoverRuns(ctx); err != nil {
			slog.Warn("Failed to recover interrupted runs", "error", err)
		}
		return
	}

	live, err := e.runStore.ListRuns(ctx, task.RunFilter{Statuses: task.LiveStatuses()})
	if err != nil {
		slog.Warn("Failed to list live runs for recovery", "error", err)
	} else {
		taskIDs := make([]int64, 0, len(live))
		for _, run := range live {
			taskIDs = append(taskIDs, run.TaskID)
		}
		if _, err := e.resolveIdentities(ctx, taskIDs); err != nil {
			slog.Warn("Failed to resolve identities for recovery", "error", err)
		}
	}

	outcome, err := e.engine.MarkInterrupted(ctx)
	if err != nil {
		slog.Warn("Failed to recover interrupted runs", "error", err)
	}
	if outcome.Marked > 0 {
		slog.Info("Recovered interrupted runs", "count", outcome.Marked)
	}
	e.resumeRuns(ctx, outcome.Resume)
}

// resumeRuns 把**可续跑**白名单挑出来的那几条中断运行重新发起一次，**发起方记恢复**。
//
// 恢复出来的是**新一次运行**：原来那条留在**中断**，不改回运行中——「上一次断在哪」是用户要的答案。
// 重排队走的是同一个启动入口，因此槽位上限照样管着它们：一次重启恢复出十条也不会同时开跑。
//
// **一条失败不拖累其余**：库被删了、路径没了都会让某个重启函数当场出错，而这一切发生在开机那一刻、
// 用户还没登录。整批中断的话，剩下那些既不会跑，也没有人知道它们本该跑。
func (e *taskEngine) resumeRuns(ctx context.Context, snapshots []task.Snapshot) {
	if len(snapshots) == 0 {
		return
	}
	runs, err := e.statusesFrom(ctx, snapshots)
	if err != nil {
		slog.Warn("Failed to resolve runs for resume", "error", err)
		return
	}
	resumed := 0
	for _, run := range runs {
		relaunch, ok := e.relauncherFor(run.Type, run.Variant)
		// 白名单与重启函数同出一张注册表，因此走不到这里；留着是因为「白名单里有、却没人发得起」
		// 的类型只会静默什么都不做，而那种漏配值得留一行日志。
		if !ok {
			slog.Warn("Resumable run has no relauncher", "run_id", run.RunID, "task_id", run.TaskID, "task_type", run.Type)
			continue
		}
		if err := relaunch(ctx, run, task.TriggerResumed); err != nil {
			slog.Warn("Failed to resume interrupted run", "run_id", run.RunID, "task_id", run.TaskID, "task_type", run.Type, "error", err)
			continue
		}
		resumed++
	}
	if resumed > 0 {
		slog.Info("Resumed interrupted runs", "count", resumed)
	}
}

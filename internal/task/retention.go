// 分层保留这一半：三个阈值的默认组合，与裁剪历史的入口。
// 选中哪些行由落盘端口回答（见 Store.PruneRuns），本文件只管「按什么裁」与「什么时候裁」。

package task

import (
	"context"
	"time"
)

// DefaultRetention 是分层保留的三个默认阈值：每任务留最近 20 次**终态**运行、终态运行 ≤90 天
// （两者取先到者）、**采样**≤7 天。
//
// 它是「装配方什么都没说」时的兜底。同一组数字在配置那边另有一份默认（配置位于本包的依赖下游，
// 引用不了这里），两份必须相等，`TestConfigDefaultRetentionMatchesTheEngineDefault` 守着这条。
//
// 数字的依据是量级而不是口味：5 个库、每小时一次守护扫描，运行每年约 4.4 万行、事件约 44 万行、
// 采样若不清理约 260 万行。这是个单机 SQLite，不裁剪的话这套设计跑不过一年。
func DefaultRetention() RetentionPolicy {
	return RetentionPolicy{
		RunsPerTask: 20,
		TerminalAge: 90 * 24 * time.Hour,
		SampleAge:   7 * 24 * time.Hour,
	}
}

// PruneHistory 按分层保留裁剪历史，返回各层清掉的行数。
//
// **活动态与排队中的运行永不被带走**：判据写死在落盘侧，不经策略。清理运行因此不会清掉自己——
// 它自己那条运行此刻正在**运行中**。这条不变量由「裁剪发生在清理运行的任务体**之内**」这个位置
// 保证：挪到任务体之外（收尾之后再裁一次）就不成立了，那一刻它已经是终态，与别的历史没有区别。
func (e *Engine) PruneHistory(ctx context.Context, policy RetentionPolicy) (PruneResult, error) {
	return e.store.PruneRuns(ctx, policy)
}

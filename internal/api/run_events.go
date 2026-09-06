// 运行详情那条按需拉取的读取面：一次运行的**运行事件**流，与由相邻两条阶段事件相减得出的
// 阶段时间线。事件**不进推送通道**（那里只有全量运行快照与**实况汇总**），详情页打开时才来问。

package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"manga-manager/internal/task"
)

// maxRunEventsPerRequest 是一次取回的**运行事件**条数上限。
//
// 它比条目失败那道上限（task.MaxItemFailureEvents）宽出一截，好让同一次运行的阶段、控制动作
// 与告警都还装得下。取头部而不是尾部：阶段时间线是从头往后相减出来的，从中间截断的话，
// 第一段的起点就没了。
const maxRunEventsPerRequest = 2000

// RunEvent 是详情页事件流上的一条**运行事件**。种类是**封闭枚举**（阶段 / 条目 / 控制 / 告警），
// 各种类只带自己那几格——它不是一条日志行，也不该长成一条日志行。
type RunEvent struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	// Phase 是切换**之后**的阶段名，只有 phase 那一类带它。
	Phase string `json:"phase,omitempty"`
	// Item 与 Reason 是失败的条目与原因，只有 item 那一类带它们。
	Item   string `json:"item,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Action 是控制动作（暂停 / 恢复 / 取消 / 被**合并**），只有 control 那一类带它。
	Action string `json:"action,omitempty"`
	// Code 是告警的短码（前端按它取文案），Detail 是补充说明，只有 warn 那一类带它们。
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail,omitempty"`
	// Count 是计数类告警的 N，典型是「还有 N 条条目失败未列出」。
	Count int64 `json:"count,omitempty"`
}

// RunPhaseSpan 是阶段时间线上的一段：这个**阶段**从什么时候开始、跑了多久。
//
// 耗时由相邻两条阶段事件相减算出，**不另存一列**：多存一份就要维护它与事件的一致，
// 而两者一旦分叉，界面上的段长与事件流里的时刻会互相打脸。
type RunPhaseSpan struct {
	Phase          string    `json:"phase"`
	StartedAt      time.Time `json:"started_at"`
	DurationMillis int64     `json:"duration_ms"`
	// Current 为真表示这一段还在跑：它是最后一段，而这条运行尚未进**终态**，
	// 因此那个耗时量到此刻为止，下次再问会更大。
	Current bool `json:"current,omitempty"`
}

// RunEventsResponse 是运行详情那一屏按需拉回来的东西：整条事件流，加上由它算出的阶段时间线
// 与「还有多少条失败没列出」。
//
// 失败明细不另发一份：它就是事件流里 kind 为 item 的那些。同一份东西发两遍，
// 两份只要错开一次就再也说不清哪份是真的。
type RunEventsResponse struct {
	RunID  int64          `json:"run_id"`
	Events []RunEvent     `json:"events"`
	Phases []RunPhaseSpan `json:"phases"`
	// OmittedFailures 是被 500 条上限挡掉、没有列进事件流的条目失败条数。
	//
	// 它是一个**独立的字段**而不是让前端去事件流里找那条告警，理由有两个：跑着的运行那条告警
	// 还没落（计数此刻在引擎的内存里），而事件流撞上取回上限被截头部时，那条恰好是最后落下的
	// 告警会被切掉——两种情况下用户看到的都会是「失败就这 500 条」。
	OmittedFailures int64 `json:"omitted_failures,omitempty"`
	// Truncated 为真表示这次运行的事件多到撞上了取回上限，交出去的只是靠前的那一段。
	Truncated bool `json:"truncated,omitempty"`
}

// getRunEvents 取一条运行的事件流与阶段时间线。
func (c *Controller) getRunEvents(w http.ResponseWriter, r *http.Request) {
	runID, err := parseID(r, "runID")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}
	response, err := c.taskEngine.runEvents(r.Context(), runID)
	if err != nil {
		if errors.Is(err, task.ErrRunNotFound) {
			jsonError(w, http.StatusNotFound, "Run not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load run events")
		return
	}
	jsonResponse(w, http.StatusOK, response)
}

// runEvents 取这条运行的事件流，并顺手把阶段时间线算出来。
//
// 先取运行行再取事件：时间线最后一段的收口要么是这条运行的结束时刻、要么是此刻，
// 而那只有运行行答得出。运行不存在时把哨兵原样交出去，端点据此回 404。
func (e *taskEngine) runEvents(ctx context.Context, runID int64) (RunEventsResponse, error) {
	run, err := e.runStore.LoadRun(ctx, runID)
	if err != nil {
		return RunEventsResponse{}, err
	}
	// 多取一条来判「是不是真的还有更多」：正好取满上限时按 `>=` 判会误报一次截断，
	// 而那句「只列出了靠前的一段」在事件恰好装得下时是一句谎话。
	events, err := e.runStore.ListRunEvents(ctx, runID, maxRunEventsPerRequest+1)
	if err != nil {
		return RunEventsResponse{}, err
	}
	truncated := len(events) > maxRunEventsPerRequest
	if truncated {
		events = events[:maxRunEventsPerRequest]
	}

	items := make([]RunEvent, 0, len(events))
	for _, event := range events {
		items = append(items, RunEvent{
			At:     event.At,
			Kind:   string(event.Kind),
			Phase:  event.Phase,
			Item:   event.Item,
			Reason: event.Reason,
			Action: string(event.Action),
			Code:   event.Code,
			Detail: event.Detail,
			Count:  event.Count,
		})
	}
	// 被截断时最后一段不再拉到运行结束时刻、也不标「当前」：切掉的那些里可能还有几次阶段切换，
	// 把最后一条列出来的阶段一路拉到收尾，界面上会长出一段几小时的、根本没发生过的阶段。
	// 此时它只量到已知的最后一条事件为止。
	end, live := runEndOf(run, e.clock()), !run.Status.IsTerminal()
	if truncated {
		end, live = events[len(events)-1].At, false
	}
	return RunEventsResponse{
		RunID:           runID,
		Events:          items,
		Phases:          phaseTimeline(events, end, live),
		OmittedFailures: e.omittedFailures(runID, events),
		Truncated:       truncated,
	}, nil
}

// omittedFailures 数这条运行有多少条失败被上限挡掉了。
//
// 两个来源恰好互斥：运行还活着时计数在引擎的内存里（收尾那一刻才结算成事件），
// 收尾之后内存里那份已经删掉、只剩事件上那个 N。相加因此不会把同一批数两遍。
func (e *taskEngine) omittedFailures(runID int64, events []task.Event) int64 {
	omitted := e.engine.OmittedItemFailures(runID)
	for _, event := range events {
		if event.Kind == task.EventWarn && event.Code == task.EventCodeItemFailuresOmitted {
			omitted += event.Count
		}
	}
	return omitted
}

// runEndOf 是阶段时间线最后一段的收口：**终态**运行取它的结束时刻，仍会变化的取此刻。
//
// 收尾时刻缺席（重启前那些没来得及写结束时刻的运行）时退回最后一次上报的时刻——
// 那个字段本身就是心跳。退回此刻的话，一条上周就断掉的运行会显示成最后那段跑了一星期。
func runEndOf(run task.Run, now time.Time) time.Time {
	if !run.Status.IsTerminal() {
		return now
	}
	if run.FinishedAt != nil {
		return *run.FinishedAt
	}
	return run.UpdatedAt
}

// phaseTimeline 把阶段事件相减成一条时间线：第 n 段从第 n 条事件起，到第 n+1 条为止；
// 最后一段到 end 为止，live 说的是那一段还在不在跑。
//
// 只认 phase 那一类事件：别的种类不改变「此刻在做哪一道工序」，混进来会把一段切成两段。
// 段长夹在零以上——两个写入方与一次系统时钟回拨都能让相邻两条的时刻倒过来，
// 而一段负的耗时在界面上没有任何念法。
func phaseTimeline(events []task.Event, end time.Time, live bool) []RunPhaseSpan {
	spans := make([]RunPhaseSpan, 0, 8)
	for _, event := range events {
		if event.Kind != task.EventPhase || event.Phase == "" {
			continue
		}
		if last := len(spans) - 1; last >= 0 {
			spans[last].DurationMillis = spanMillis(spans[last].StartedAt, event.At)
		}
		spans = append(spans, RunPhaseSpan{Phase: event.Phase, StartedAt: event.At})
	}
	if last := len(spans) - 1; last >= 0 {
		spans[last].DurationMillis = spanMillis(spans[last].StartedAt, end)
		spans[last].Current = live
	}
	return spans
}

// spanMillis 是一段的毫秒长度，倒过来的一对时刻算作零长。
func spanMillis(from, to time.Time) int64 {
	if !to.After(from) {
		return 0
	}
	return to.Sub(from).Milliseconds()
}

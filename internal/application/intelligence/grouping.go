package intelligence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/metadata"
	"manga-manager/internal/organize"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"strconv"
)

// GroupLibrary generates collection suggestions for explicit later adjudication.
func (c *Service) GroupLibrary(ctx context.Context, libID int64, locale string, trigger task.Trigger) (task.Launched, error) {
	locale = metadata.LocaleFromContext(metadata.WithLocale(ctx, locale))
	scopeName := ""
	if lib, err := c.store.GetLibrary(ctx, libID); err == nil {
		scopeName = lib.Name
	}

	spec := task.RunSpec{
		Key:       fmt.Sprintf("ai_grouping_library_%d", libID),
		StartCode: "task.msg.ai_grouping.start",
		Total:     1,
		CanCancel: true,
		CanPause:  true,
		ScopeName: scopeName,
		// locale 必须落进任务参数（而不是起始文案的占位参数）：**重启函数**靠它恢复原始语言，
		// 重试优先使用这份原始语种，旧运行才回退到本次请求语种。
		Args:         map[string]string{"locale": locale},
		CompleteCode: "task.msg.ai_grouping.review_generated",
		CancelCode:   "task.msg.ai_grouping.cancelled",
		FailCode:     "task.msg.ai_grouping.fail_generate",
	}

	spec.Identity = task.Identity{Type: "ai_grouping", Scope: task.ScopeLibrary, ScopeID: libID}
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{libID}
	return c.runs.Start(ctx, spec, func(taskCtx context.Context, handle *runhandle.Handle) (task.Result, error) {
		ctx := metadata.WithLocale(taskCtx, locale)

		result, err := c.groups.Generate(ctx, libID, c.sources.Grouping, groupingObserver{Handle: handle, ctx: ctx, libraryID: libID})
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.ErrorContext(ctx, "AI grouping failed", "library_id", libID, "error", err)
			}
			var generationErr *organize.GroupingGenerationError
			if errors.As(err, &generationErr) {
				switch generationErr.Phase {
				case "fetch":
					return failure("task.msg.ai_grouping.fail_db_fetch", err), err
				case "review":
					return failure("task.msg.ai_grouping.fail_create_review", err), err
				}
			}
			return task.Result{}, err
		}
		if result.Candidates == 0 {
			return task.Result{Code: "task.msg.ai_grouping.all_already_grouped"}, nil
		}
		if result.Collections == 0 {
			return task.Result{Code: "task.msg.ai_grouping.no_review_collections"}, nil
		}

		c.publish()
		return task.Result{Params: map[string]string{
			"reviewId": strconv.FormatInt(result.Review.ID, 10),
			"count":    strconv.Itoa(result.Collections),
		}}, nil
	})
}

func failure(code string, err error) task.Result {
	if errors.Is(err, context.Canceled) {
		return task.Result{}
	}
	return task.Result{Code: code}
}

type groupingObserver struct {
	*runhandle.Handle
	ctx       context.Context
	libraryID int64
}

func (o groupingObserver) Collecting() {
	o.Phase("collecting_series", "task.msg.ai_grouping.collecting_series", nil)
}
func (o groupingObserver) Requesting(count int, provider string) {
	slog.InfoContext(o.ctx, "AI grouping: fetched candidate series", "library_id", o.libraryID, "count", count)
	o.Report(runhandle.Frame{Phase: "requesting_provider", Code: "task.msg.ai_grouping.requesting_provider", Metrics: map[string]int64{"candidate_series": int64(count)}, Labels: map[string]string{"provider": provider}})
}
func (o groupingObserver) Queueing() {
	done := 1
	o.Report(runhandle.Frame{Current: &done, Phase: "queueing_review", Code: "task.msg.ai_grouping.queueing_review"})
}

// 业务说明：本文件守卫元数据审核的终态状态机 —— pending 只能单向流转到 applied 或 rejected。
//
// 四条终态推进路径（单条 apply / 单条 reject / 批量 apply / 批量 reject）里，单条 reject
// 此前完全没有 pending 守卫：一条已 applied 的审核可以被改成 rejected，而 SQL 里
// applied_at/rejected_at 是「CASE ... ELSE 保留旧值」的累加写法，于是这一行同时带着
// applied_at 与 rejected_at，status 却只剩后写的那个。series_metadata_provenance.review_id
// 还指着它，审计链变成「系列的元数据来自一条已被拒绝的审核」，自相矛盾且无法复原。
//
// 更深一层是 TOCTOU：守卫只做在 HTTP 层的内存判断上，读到 pending 与真正写入之间存在窗口，
// 并发的 apply/reject 会写出同一种脏行，而 apply 那一路还会用事务外读到的旧 series 快照
// 把先提交者刚写好的字段回滚成旧值。

package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
)

// seedPendingMetadataReview 造一个系列 + 一条 pending 的元数据审核。
func seedPendingMetadataReview(t *testing.T, controller *Controller, store database.Store, name string) (database.Series, database.MetadataReview) {
	t.Helper()
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib-"+name, name, "book-"+name+".cbz", 10)

	review, _, _, err := controller.queueMetadataReview(context.Background(), series, &metadata.SeriesMetadata{
		Title:      "External " + name,
		Publisher:  "External Publisher",
		Summary:    "External summary",
		SourceID:   1,
		SourceURL:  "https://example.test/" + name,
		Provider:   "bangumi",
		Confidence: 0.7,
	}, "bangumi", name)
	if err != nil {
		t.Fatalf("queueMetadataReview: %v", err)
	}
	return series, review
}

// reviewTimestamps 读回 review 的状态与两个终态时间戳。
func reviewTimestamps(t *testing.T, store database.Store, id int64) (status string, appliedAt, rejectedAt sql.NullTime) {
	t.Helper()
	review, err := store.GetMetadataReview(context.Background(), id)
	if err != nil {
		t.Fatalf("GetMetadataReview: %v", err)
	}
	return review.Status, review.AppliedAt, review.RejectedAt
}

// TestMetadataReviewTerminalStatusIsGuarded 钉住「终态不可被再次改写」。
//
// 播种一律走 HTTP handler 而不是 store 方法：这样把修复回滚做缺陷还原时，用例是以
// **断言失败**报红，而不是因为 store 方法改了名而编译不过——后者证明不了任何事。
func TestMetadataReviewTerminalStatusIsGuarded(t *testing.T) {
	cases := []struct {
		name     string
		seed     string // 先把 review 推到的终态：applied / rejected / ""（保持 pending）
		action   string // 随后执行的动作：apply / reject
		wantCode int
		wantEnd  string // 期望的最终 status
	}{
		{"已应用后再拒绝", "applied", "reject", http.StatusConflict, "applied"},
		{"已拒绝后再应用", "rejected", "apply", http.StatusConflict, "rejected"},
		{"已拒绝后再拒绝", "rejected", "reject", http.StatusConflict, "rejected"},
		{"已应用后再应用", "applied", "apply", http.StatusConflict, "applied"},
		{"pending 正常拒绝", "", "reject", http.StatusOK, "rejected"},
		{"pending 正常应用", "", "apply", http.StatusOK, "applied"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			_, review := seedPendingMetadataReview(t, controller, store, "Series-"+tc.name)

			invoke := func(action string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := requestWithRouteParam(http.MethodPost, "/api/metadata/reviews/x", nil,
					"reviewId", strconv.FormatInt(review.ID, 10))
				if action == "apply" {
					controller.applyMetadataReview(rec, req)
				} else {
					controller.rejectMetadataReview(rec, req)
				}
				return rec
			}

			if tc.seed != "" {
				seedAction := "apply"
				if tc.seed == "rejected" {
					seedAction = "reject"
				}
				if rec := invoke(seedAction); rec.Code != http.StatusOK {
					t.Fatalf("播种 %s 失败：HTTP %d %s", tc.seed, rec.Code, rec.Body.String())
				}
			}

			rec := invoke(tc.action)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s 返回 %d，期望 %d（body=%s）", tc.action, rec.Code, tc.wantCode, rec.Body.String())
			}

			status, appliedAt, rejectedAt := reviewTimestamps(t, store, review.ID)
			if status != tc.wantEnd {
				t.Errorf("最终 status = %q，期望 %q —— 终态被再次改写了", status, tc.wantEnd)
			}
			// 审计链的核心不变量：两个终态时间戳不能同时非空。
			// 注意不是 XOR——pending 行两列皆空是合法的，用 XOR 断言会在将来加 pending 用例时误报。
			if appliedAt.Valid && rejectedAt.Valid {
				t.Errorf("applied_at 与 rejected_at 同时非空 —— 这一行既「已应用」又「已拒绝」，" +
					"而 series_metadata_provenance 还指着它，审计链自相矛盾")
			}
			switch tc.wantEnd {
			case "applied":
				if !appliedAt.Valid {
					t.Error("状态是 applied 却没有 applied_at")
				}
				if rejectedAt.Valid {
					t.Error("状态是 applied 却带着 rejected_at")
				}
			case "rejected":
				if !rejectedAt.Valid {
					t.Error("状态是 rejected 却没有 rejected_at")
				}
				if appliedAt.Valid {
					t.Error("状态是 rejected 却带着 applied_at")
				}
			}
		})
	}
}

// TestRejectDoesNotRefreshTerminalTimestamp 钉住「重复 reject 不会顶掉原始时间戳」。
//
// 单独成例是因为 SQLite 的 CURRENT_TIMESTAMP 只有秒精度：直接比对两次 reject 的时间戳
// 在缺陷代码下也会「相同」，是一条永不报红的死断言。这里先把 rejected_at 手工改成一个远古值，
// 缺陷代码会把它顶成当前时间，断言必红。
func TestRejectDoesNotRefreshTerminalTimestamp(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, review := seedPendingMetadataReview(t, controller, store, "Timestamp")

	rec := httptest.NewRecorder()
	controller.rejectMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("首次 reject 失败：%d %s", rec.Code, rec.Body.String())
	}

	const ancient = "2000-01-01 00:00:00"
	sqlStore, ok := store.(*database.SqlStore)
	if !ok {
		t.Fatalf("需要 *SqlStore 才能直改行，得到 %T", store)
	}
	if _, err := sqlStore.DB().Exec(
		`UPDATE metadata_reviews SET rejected_at = ? WHERE id = ?`, ancient, review.ID); err != nil {
		t.Fatalf("改写 rejected_at 失败：%v", err)
	}

	rec = httptest.NewRecorder()
	controller.rejectMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复 reject 返回 %d，期望 409", rec.Code)
	}

	var got string
	if err := sqlStore.DB().QueryRow(
		`SELECT rejected_at FROM metadata_reviews WHERE id = ?`, review.ID).Scan(&got); err != nil {
		t.Fatalf("读回 rejected_at 失败：%v", err)
	}
	// 驱动读回来会规范化成 RFC3339（2000-01-01T00:00:00Z），只比对日期部分即可判别是否被顶掉。
	if !strings.HasPrefix(got, "2000-01-01") {
		t.Fatalf("rejected_at 被第二次 reject 顶成了 %q —— 原始拒绝时间丢失，审计链断了", got)
	}
}

// TestApplyReviewedMetadataRejectsStaleSnapshot 构造 TOCTOU 窗口：
// 手里的 review 快照仍是 pending，但那一行已被别的请求推进到终态。
//
// 这正是并发 apply/reject 或用户在两个标签页里各点一次的真实形态。守卫若只做在
// HTTP 层的内存判断上，这里会成功写入元数据、把 rejected 覆写成 applied，
// 并留下同时带两个时间戳的脏行 + 一条指向「已拒绝审核」的 provenance。
func TestApplyReviewedMetadataRejectsStaleSnapshot(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	series, review := seedPendingMetadataReview(t, controller, store, "Stale")
	ctx := context.Background()

	fields, err := store.ListMetadataReviewFields(ctx, review.ID)
	if err != nil {
		t.Fatalf("ListMetadataReviewFields: %v", err)
	}
	if len(fields) == 0 {
		t.Fatal("审核没有字段提案，用例失去判别力")
	}

	// 另一个请求抢先把它拒了（这里直接走 handler，等价于并发的第二个请求先提交）。
	rec := httptest.NewRecorder()
	controller.rejectMetadataReview(rec, requestWithRouteParam(http.MethodPost, "/x", nil,
		"reviewId", strconv.FormatInt(review.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("抢先 reject 失败：%d %s", rec.Code, rec.Body.String())
	}

	before, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}

	// review 仍是事务外读到的那份 pending 快照。
	_, err = controller.applyReviewedMetadata(ctx, series, review, fields, fields)
	if !errors.Is(err, errMetadataReviewNotPending) {
		t.Fatalf("applyReviewedMetadata 返回 %v，期望 errMetadataReviewNotPending —— "+
			"事务外的 pending 判断是可以过期的，守卫必须在事务内的 SQL 上", err)
	}

	after, err := store.GetSeries(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if after.Title != before.Title || after.Publisher != before.Publisher || after.Summary != before.Summary {
		t.Errorf("冲突时元数据仍被写入了（title %v->%v）—— 事务没有整体回滚",
			before.Title.String, after.Title.String)
	}

	prov, err := store.GetSeriesMetadataProvenance(ctx, series.ID)
	if err != nil {
		t.Fatalf("GetSeriesMetadataProvenance: %v", err)
	}
	if len(prov) != 0 {
		t.Errorf("留下了 %d 条溯源记录，且指向一条已被拒绝的审核 —— 审计链自相矛盾", len(prov))
	}

	status, appliedAt, rejectedAt := reviewTimestamps(t, store, review.ID)
	if status != "rejected" {
		t.Errorf("status = %q，期望 rejected —— 抢先者的结果被覆写了", status)
	}
	if appliedAt.Valid {
		t.Error("行上残留了 applied_at —— 冲突的 apply 仍然写入了终态时间戳")
	}
	if !rejectedAt.Valid {
		t.Error("rejected_at 丢了")
	}
}

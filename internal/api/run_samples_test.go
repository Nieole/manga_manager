// 守吞吐曲线那条按需拉的读取面：曲线画的是这一次运行、界面能说出「曲线为什么不完整」，
// 外加端点契约（404 / 400）与「采样不进推送通道」。
//
// 取点的节奏由 `internal/task` 的用例守（那里有可控时钟），这里只守 api 这一层的形状。

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/task"
)

// runSamplesOf 走端点取一条运行的吞吐曲线。
func runSamplesOf(t *testing.T, c *Controller, runID int64) RunSamplesResponse {
	t.Helper()
	req := requestWithRouteParam(http.MethodGet, "/api/system/runs/1/samples", nil, "runID", strconv.FormatInt(runID, 10))
	rec := httptest.NewRecorder()
	c.getRunSamples(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("取采样返回 %d，想要 200（body=%s）", rec.Code, rec.Body.String())
	}
	var payload RunSamplesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解采样失败: %v（raw=%s）", err, rec.Body.String())
	}
	return payload
}

// TestRunSamplesDrawTheThroughputOfThisRun 走一遍完整的一条：取三次点，详情页拿回的曲线
// 逐点带着计数与那一段的吞吐，中间那段停滞如实是零。
//
// 这一条是本票的正题：「它是不是卡住了」不用靠猜——曲线上那个零就是答案。
func TestRunSamplesDrawTheThroughputOfThisRun(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	engine, _ := newClockedTestEngine(t, runTaskBodySynchronously, nil, clock.Now)
	controller := &Controller{taskEngine: engine}
	ctx := context.Background()

	handle := seedTask(t, engine, taskSeed{
		Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole), Total: 500,
	})
	runID := currentTask(t, engine, "scan_library_7").RunID

	handle.Advance(120, 500, "", nil)
	clock.advance(task.DefaultSampleInterval)
	engine.engine.SampleActiveRuns(ctx)

	// 一整个间隔一条都没动：这一段就是用户嘴里那个「是不是卡住了」。
	clock.advance(task.DefaultSampleInterval)
	engine.engine.SampleActiveRuns(ctx)

	handle.Advance(180, 500, "", nil)
	clock.advance(task.DefaultSampleInterval)
	engine.engine.SampleActiveRuns(ctx)

	payload := runSamplesOf(t, controller, runID)
	if payload.RunID != runID {
		t.Fatalf("载荷上的运行标识为 %d, want %d", payload.RunID, runID)
	}
	if len(payload.Samples) != 3 {
		t.Fatalf("曲线上有 %d 个点, want 3：%+v", len(payload.Samples), payload.Samples)
	}
	want := []struct {
		current int
		rate    float64
	}{{120, 720}, {120, 0}, {180, 360}}
	for i, expected := range want {
		got := payload.Samples[i]
		if got.Current != expected.current || got.RatePerMinute != expected.rate {
			t.Errorf("第 %d 个点是 %d 条 / %.2f 每分钟，想要 %d 条 / %.2f 每分钟",
				i+1, got.Current, got.RatePerMinute, expected.current, expected.rate)
		}
	}
}

// TestRunSamplesSayWhenTheEarlyCurveHasExpired 守「曲线缺失时界面明说，不画一条假的」，
// 以及它的反面：**曲线没缺的时候不许说它缺了**。
//
// 判据照抄裁剪那一刀（`at < 截止时刻`），不是「运行有多老」。清理每天才跑一次，因此
// 「开跑于保留期之外」与「早期的点真的没了」是两回事——前者当判据会为一条完整的曲线说谎。
func TestRunSamplesSayWhenTheEarlyCurveHasExpired(t *testing.T) {
	now := time.Unix(1700000000, 0)
	const retentionDays = 7
	cutoff := now.Add(-7 * 24 * time.Hour)
	interval := task.DefaultSampleInterval
	longAgo := now.Add(-8 * 24 * time.Hour)

	cases := []struct {
		name     string
		run      task.Run
		earliest time.Time
		want     bool
	}{
		{
			"早期的点已经在截止时刻之外被切掉",
			task.Run{StartedAt: longAgo, Status: task.StatusRunning},
			cutoff.Add(time.Hour),
			true,
		},
		{
			// 这一条是本票要挡的谎：运行够老了，但清理还没跑，点一个没少。
			"运行够老、点却一个没少",
			task.Run{StartedAt: longAgo, Status: task.StatusRunning},
			longAgo.Add(time.Minute),
			false,
		},
		{
			"整条曲线都没了：跑够一个间隔却一个点都没有",
			task.Run{StartedAt: longAgo, Status: task.StatusCompleted, FinishedAt: ptrTime(longAgo.Add(time.Hour))},
			time.Time{},
			true,
		},
		{
			// 跑不满一个取点间隔的运行本来就没有点可清，界面该说的是「还没有采样点」。
			"太短、本来就没有点可清",
			task.Run{StartedAt: longAgo, Status: task.StatusCompleted, FinishedAt: ptrTime(longAgo.Add(time.Second))},
			time.Time{},
			false,
		},
		{"开跑于保留期之内", task.Run{StartedAt: now.Add(-6 * 24 * time.Hour)}, time.Time{}, false},
		{"还没开跑的排队运行", task.Run{}, time.Time{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := samplesExpired(tc.run, tc.earliest, interval, retentionDays, now); got != tc.want {
				t.Fatalf("过期判定为 %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("这一层不裁剪时不会过期", func(t *testing.T) {
		ancient := task.Run{StartedAt: now.Add(-10 * 365 * 24 * time.Hour)}
		if samplesExpired(ancient, time.Time{}, interval, 0, now) {
			t.Fatal("保留天数是「这一层不裁剪」，却报了过期")
		}
	})
}

func ptrTime(at time.Time) *time.Time { return &at }

// TestRunSamplesCarryTheRetentionWindow 守保留天数随载荷发出去：界面要说得出
// 「一周之后运行还在、曲线没了」，就得知道是几天。
func TestRunSamplesCarryTheRetentionWindow(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	seedTask(t, controller.taskEngine, taskSeed{
		Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole),
	})
	runID := currentTask(t, controller.taskEngine, "scan_library_7").RunID

	payload := runSamplesOf(t, controller, runID)
	if payload.RetentionDays != controller.currentConfig().Tasks.RetainSampleDays {
		t.Fatalf("载荷上的保留天数为 %d，想要设置里那个 %d",
			payload.RetentionDays, controller.currentConfig().Tasks.RetainSampleDays)
	}
	// 一个点都还没取到：交出去的是空曲线而不是一条编出来的线。
	if len(payload.Samples) != 0 {
		t.Fatalf("还没取过点，曲线上却有 %d 个点", len(payload.Samples))
	}
}

// TestConfigDefaultSampleIntervalMatchesTheEngineDefault 守两个默认值相等：配置那个是
// 「配置文件里没写」时的默认，领域那个是「装配方什么都没说」时的兜底。错开一次，
// 默认部署与默认引擎会按两个节奏取点，而载荷上那个 interval_seconds 会与实际疏密对不上。
//
// config 位于 task 的依赖下游，因此那边引用不了这个常数，只能在这里对一次。
func TestConfigDefaultSampleIntervalMatchesTheEngineDefault(t *testing.T) {
	if got := time.Duration(config.DefaultSampleIntervalSeconds) * time.Second; got != task.DefaultSampleInterval {
		t.Fatalf("配置默认间隔 %v 与领域默认 %v 不一致", got, task.DefaultSampleInterval)
	}
}

// TestSampleIntervalComesFromTheRuntimeConfig 守间隔是**从配置读**的，改完设置对下一拍生效，
// 不必重启进程（同槽位与保留阈值的口径）。
//
// 两头的越界值都要落在安全的那一侧：零或负数会让 time.Ticker 当场 panic，
// 而大到溢出的秒数乘成 time.Duration 会绕回一个很小的正时长——那等于把取点变回跟着帧走。
func TestSampleIntervalComesFromTheRuntimeConfig(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if got := controller.runSampleInterval(); got != task.DefaultSampleInterval {
		t.Fatalf("默认配置下的取点间隔为 %v, want %v", got, task.DefaultSampleInterval)
	}

	cfg := controller.currentConfig()
	cfg.Tasks.SampleIntervalSeconds = 30
	controller.config.Replace(&cfg)
	if got := controller.runSampleInterval(); got != 30*time.Second {
		t.Fatalf("改成 30 秒后读回 %v", got)
	}

	for _, seconds := range []int{0, -1, 1 << 62} {
		cfg := controller.currentConfig()
		cfg.Tasks.SampleIntervalSeconds = seconds
		controller.config.Replace(&cfg)
		got := controller.runSampleInterval()
		if got <= 0 || got > maxSampleIntervalSeconds*time.Second {
			t.Fatalf("越界的 %d 秒读回 %v —— 它要么让节拍当场 panic，要么把取点变回跟着帧走", seconds, got)
		}
	}
}

// TestRunSamplesEndpointContract 钉端点的两条错误分支：运行不存在是 404，路径参数不是运行 id 是 400。
func TestRunSamplesEndpointContract(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	t.Run("运行不存在", func(t *testing.T) {
		req := requestWithRouteParam(http.MethodGet, "/api/system/runs/4242/samples", nil, "runID", "4242")
		rec := httptest.NewRecorder()
		controller.getRunSamples(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404（body=%s）", rec.Code, rec.Body.String())
		}
	})

	t.Run("路径参数不是运行 id", func(t *testing.T) {
		req := requestWithRouteParam(http.MethodGet, "/api/system/runs//samples", nil, "runID", "")
		rec := httptest.NewRecorder()
		controller.getRunSamples(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400（body=%s）", rec.Code, rec.Body.String())
		}
	})
}

// TestRunSamplesAreNotPushed 守规格边界（票 19）：**事件与采样不推**，详情页按需拉。
//
// 推送通道上的载荷只有全量运行快照与**实况汇总**两种。一条长跑的运行每十秒一个点，
// 挤进去等于让每个开着页面的浏览器一直收一条它多半没在看的曲线。
func TestRunSamplesAreNotPushed(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1700000000, 0)}
	var frames []string
	engine := newTaskEngine(taskEngineConfig{
		Store:         newTaskTestStore(t),
		Publish:       func(payload string) { frames = append(frames, payload) },
		RunBackground: runTaskBodySynchronously,
		Now:           clock.Now,
	})
	seedTask(t, engine, taskSeed{Key: "scan_library_7", Identity: libraryTask("scan_library", 7, variantSole)})

	before := len(frames)
	clock.advance(task.DefaultSampleInterval)
	engine.engine.SampleActiveRuns(context.Background())

	if len(frames) != before {
		t.Fatalf("取一次点投出了 %d 帧，想要 0 帧：%v", len(frames)-before, frames[before:])
	}
	for _, frame := range frames {
		if strings.Contains(frame, "rate_per_minute\":") && strings.Contains(frame, "samples") {
			t.Fatalf("推送通道上出现了采样载荷: %s", frame)
		}
	}
}

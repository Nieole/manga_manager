// **采样**在 api 这一侧的两端：每隔一个间隔催领域引擎取一次点的节拍，与运行详情那条按需拉的
// 吞吐曲线读取面。采样**不进推送通道**（那里只有全量运行快照与**实况汇总**）。
//
// **这一层一个判断都不读采样**：进度、终态与速率的事实来源始终是运行行本身，
// 采样丢一个点只该让曲线少一段。

package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/task"
)

const (
	// maxRunSamplesPerRequest 是一次取回的**采样**点数上限。
	//
	// 撞上限时交出去的是**最近的**那一段（与**运行事件**截头部相反）：曲线答的是
	// 「它此刻是不是卡住了」，掐掉最近的一段等于把唯一答得出这个问题的部分掐掉。
	// 按默认间隔算，这一段覆盖最近五个多小时；更早的那些留给保留期自己收走。
	maxRunSamplesPerRequest = 2000

	// maxSampleIntervalSeconds 是取点间隔的上限（一天），用来挡住溢出而不是表达策略。
	// 秒数是配置里的一个整数，乘成 time.Duration 会在几百年量级绕回负数，而配置文件是手写的。
	maxSampleIntervalSeconds = 86400
)

// RunSample 是吞吐曲线上的一个点：那一刻的计数，与**自上一个点以来**的吞吐。
//
// RatePerMinute 与运行卡片上那个 rate_per_minute 不是同一个数，两者答的不是同一个问题：
// 卡片上那个是这次运行至今的平均速率（分母里扣掉了**暂停**），这里这个是那一段窗口的吞吐
// （分母就是墙上时间）。暂停期间一条都没处理，因此这里如实是 0——那正是曲线要显示的东西。
type RunSample struct {
	At            time.Time `json:"at"`
	Current       int       `json:"current"`
	RatePerMinute float64   `json:"rate_per_minute"`
}

// RunSamplesResponse 是运行详情那条吞吐曲线按需拉回来的东西。
//
// 它与事件流分开发：两者的量级差着一个数量级，而事件那份在一条只跑了十秒的运行上也有内容，
// 采样那份则要等第一个间隔过去才有。合成一份会让其中一半的缺席说不清是哪一半。
type RunSamplesResponse struct {
	RunID   int64       `json:"run_id"`
	Samples []RunSample `json:"samples"`
	// IntervalSeconds 是这一刻生效的取点间隔。界面据它判断相邻两点之间是不是漏了点——
	// **漏了就断开，不连线**：没观测过的那一段连过去就是编一段没发生过的数据。
	// 「隔多远才算漏」由画曲线的那一侧定（见 web 的 runSamples），这里只把间隔如实交出去。
	IntervalSeconds int `json:"interval_seconds"`
	// RetentionDays 是**采样**的保留天数。它比运行本身短，因此「运行还在、曲线没了」是设计
	// 而不是缺陷——界面要能把这句话说出来，就得知道是几天。
	RetentionDays int `json:"retention_days"`
	// Expired 为真表示这条运行开跑于保留期之外，它早期的那段曲线已经被清理带走了。
	//
	// 它由**运行的开跑时刻**判定，不由「点少不少」猜：一条刚起步的运行同样没几个点，
	// 而那不是过期。界面据此明说曲线不完整，而不是把剩下的点连成一条看起来完整的线。
	Expired bool `json:"expired,omitempty"`
	// Truncated 为真表示点数撞上取回上限，交出去的是**最近的**那一段。
	Truncated bool `json:"truncated,omitempty"`
}

// startRunSampler 按取点间隔催领域引擎取一次**采样**，随 Controller 生命周期退出
// （经 runBackground 登记 backgroundWG，关闭时会退出）。
//
// 它只是**节拍**，不是判据：一条运行在一个间隔里最多落一个点，那道水位在领域引擎里
// （见 task.Engine.SampleActiveRuns）。因此这个节拍即使被拖慢或催快，写入量都不会失控。
//
// 每一拍之后重设周期，好让设置里改过的间隔对**下一拍**生效，不必重启进程（同 taskSlots 的口径）。
func (c *Controller) startRunSampler() {
	ticker := time.NewTicker(c.runSampleInterval())
	defer ticker.Stop()
	for {
		select {
		case <-c.lifecycleDone():
			return
		case <-ticker.C:
			c.taskEngine.engine.SampleActiveRuns(context.Background())
			ticker.Reset(c.runSampleInterval())
		}
	}
}

// runSampleInterval 读此刻生效的取点间隔。
func (c *Controller) runSampleInterval() time.Duration {
	return sampleIntervalOf(c.currentConfig())
}

// sampleIntervalOf 把设置里的秒数翻成间隔：非正数交给领域引擎的默认值，大到会溢出的按上限收。
//
// 归一化本该已经把非正数补成默认值，这里再收一道是因为它同时服务节拍——一个零间隔的
// time.Ticker 会当场 panic，而配置一路来自手写的文件。
func sampleIntervalOf(cfg config.Config) time.Duration {
	seconds := cfg.Tasks.SampleIntervalSeconds
	if seconds < 1 {
		return task.DefaultSampleInterval
	}
	if seconds > maxSampleIntervalSeconds {
		seconds = maxSampleIntervalSeconds
	}
	return time.Duration(seconds) * time.Second
}

// getRunSamples 取一条运行的吞吐曲线。
func (c *Controller) getRunSamples(w http.ResponseWriter, r *http.Request) {
	runID, err := parseID(r, "runID")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid run ID")
		return
	}
	cfg := c.currentConfig()
	response, err := c.taskEngine.runSamples(r.Context(), runID,
		sampleIntervalOf(cfg), cfg.Tasks.RetainSampleDays)
	if err != nil {
		if errors.Is(err, task.ErrRunNotFound) {
			jsonError(w, http.StatusNotFound, "Run not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to load run samples")
		return
	}
	jsonResponse(w, http.StatusOK, response)
}

// runSamples 取这条运行的采样点，并把界面用来说实话的那三样一起交出去：取点间隔、保留天数，
// 以及「早期那一段是不是已经过了保留期」。
//
// 先取运行行再取采样：过期与否判的是**运行的开跑时刻**，而那只有运行行答得出。
// 运行不存在时把哨兵原样交出去，端点据此回 404。
func (e *taskEngine) runSamples(ctx context.Context, runID int64, interval time.Duration, retentionDays int) (RunSamplesResponse, error) {
	run, err := e.runStore.LoadRun(ctx, runID)
	if err != nil {
		return RunSamplesResponse{}, err
	}
	// 多取一条来判「是不是真的还有更多」：正好取满上限时按 `>=` 判会误报一次截断，
	// 而那句「只画了最近的一段」在点数恰好装得下时是一句谎话。
	samples, err := e.engine.RunSamples(ctx, runID, maxRunSamplesPerRequest+1)
	if err != nil {
		return RunSamplesResponse{}, err
	}
	truncated := len(samples) > maxRunSamplesPerRequest
	if truncated {
		samples = samples[len(samples)-maxRunSamplesPerRequest:]
	}

	points := make([]RunSample, 0, len(samples))
	for _, sample := range samples {
		points = append(points, RunSample{
			At:            sample.At,
			Current:       sample.Current,
			RatePerMinute: sample.RatePerMinute,
		})
	}
	return RunSamplesResponse{
		RunID:           runID,
		Samples:         points,
		IntervalSeconds: int(interval.Seconds()),
		RetentionDays:   retentionDays,
		Expired:         samplesExpired(run, retentionDays, e.clock()),
		Truncated:       truncated,
	}, nil
}

// samplesExpired 判这条运行早期的那段曲线是不是已经被保留期清走了：开跑时刻落在截止时刻之前
// 就是。**排队中**的运行还没开跑（没有开跑时刻），谈不上过期。
//
// 判据取开跑时刻而不是「第一个点离开跑有多远」：后者在一条刚起步、还没攒够一个间隔的运行上
// 同样成立，而那不是过期。保留天数非正表示这一层不裁剪，因此也不会过期。
func samplesExpired(run task.Run, retentionDays int, now time.Time) bool {
	if retentionDays <= 0 || run.StartedAt.IsZero() {
		return false
	}
	return run.StartedAt.Before(now.Add(-retentionAge(retentionDays)))
}

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
			c.taskEngine.sampleActiveRuns(context.Background())
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
// 它只服务两处，且两处交出的是**同一个数**：装配期交给领域引擎的那个读间隔闭包，与取点的节拍。
// 读取面不走它——那一处问引擎要（见 runSamples），因此「此刻的间隔是多久」只有一个答案。
//
// 归一化本该已经把非正数补成默认值，这里再收一道是因为节拍容不下零：一个零间隔的
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
	response, err := c.taskEngine.runSamples(r.Context(), runID, c.currentConfig().Tasks.RetainSampleDays)
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
// 间隔问的是**领域引擎**而不是再算一遍配置：取点的水位读的就是它，而这里拿它判断
// 「一个点都没有」该不该算作被清走。各算各的话，改一次设置就会让这句话说反。
//
// 先取运行行再取采样：过期与否判的是**运行的开跑时刻**，而那只有运行行答得出。
// 运行不存在时把哨兵原样交出去，端点据此回 404。
func (e *taskEngine) runSamples(ctx context.Context, runID int64, retentionDays int) (RunSamplesResponse, error) {
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
	// 截断时一律不提过期：头上那一刀是**我们自己截的**，与保留期无关，而界面另有一句话说它。
	// 不挡这一下的话，每一条长跑运行都会同时被说成「被清理过」。
	expired := !truncated && samplesExpired(run, earliestSampleAt(samples),
		e.engine.SampleInterval(), retentionDays, e.clock())
	return RunSamplesResponse{
		RunID:         runID,
		Samples:       points,
		RetentionDays: retentionDays,
		Expired:       expired,
		Truncated:     truncated,
	}, nil
}

// earliestSampleAt 取手上最早那个点的时刻；一个点都没有时交回零值。
func earliestSampleAt(samples []task.Sample) time.Time {
	if len(samples) == 0 {
		return time.Time{}
	}
	return samples[0].At
}

// samplesExpired 判这条运行早期的那段曲线是不是**真的**被保留期清走了。
//
// 判据照抄裁剪那一刀本身（票 18：`DELETE FROM run_samples WHERE at < 截止时刻`），
// 而不是「这条运行有多老」：
//   - 开跑于截止时刻**之后** → 它的点一个都够不着那一刀。
//   - 手上最早的点已经落在截止时刻**之后**，而运行开跑于它之前 → 中间那一段正是被那一刀切掉的。
//   - 一个点都没有 → 跑够一个取点间隔的运行本该留下点，没有就是整条被清走了；
//     跑不够一个间隔的运行本来就没有点可清，那不是过期，界面该说的是另一句话。
//
// 光看开跑时刻不行：清理**每天才跑一次**，一条开跑于第 8 天、采样点一个没少的运行会被说成
// 「早于 7 天的采样已被清理」——那是为一条完整的曲线说了一句谎，而本票要的恰恰是不说谎。
//
// **排队中**的运行还没开跑，谈不上过期；保留天数非正表示这一层不裁剪，同样不会过期。
func samplesExpired(run task.Run, earliest time.Time, interval time.Duration, retentionDays int, now time.Time) bool {
	if retentionDays <= 0 || run.StartedAt.IsZero() {
		return false
	}
	cutoff := now.Add(-retentionAge(retentionDays))
	if !run.StartedAt.Before(cutoff) {
		return false
	}
	if earliest.IsZero() {
		return runEndOf(run, now).Sub(run.StartedAt) >= interval
	}
	return !earliest.Before(cutoff)
}

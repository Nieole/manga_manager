package scanning

import (
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"path/filepath"
)

// taskScanObserver 把一次扫描的报文写进它所属任务的**运行句柄**。
//
// 它只是句柄的一层翻译，不持有任何状态：谁有资格写这个任务，由「谁拿到了这个句柄」回答，
// 与句柄本身的所有权模型一致（见 runhandle.Handle）。
type taskScanObserver struct {
	progress *runhandle.Handle
}

// NewObserver 把运行句柄包成一个扫描观察者；句柄为 nil 时返回 nil 接口值，
// 于是「这次扫描的报文无处可报」在扫描器那边只有一个形状。
func NewObserver(progress *runhandle.Handle) scanner.ScanObserver {
	if progress == nil {
		return nil
	}
	return &taskScanObserver{progress: progress}
}

func (o *taskScanObserver) Progress(report scanner.ScanProgressReport) {
	o.progress.Report(ProgressFrame(report))
}

// ItemFailed 把一个条目的失败落成这条运行的**运行事件**：详情页的失败明细读的就是它。
func (o *taskScanObserver) ItemFailed(failure scanner.ItemFailure) {
	o.progress.ItemFailed(failure.Path, failure.Reason)
}

// Warn 把一次运行级告警落成这条运行的**运行事件**。
//
// 三项逐个搬过去，不把计数拼进补充说明：拼进去的话，详情页只能把一句英文错误串连同一个数字
// 原样甩给用户，而「这一批有多少本」正是它唯一说得清的那部分。
func (o *taskScanObserver) Warn(warning scanner.ScanWarning) {
	o.progress.Warn(warning.Code, warning.Detail, warning.Count)
}

// Metrics 把扫描收尾的那份报文定版进这条运行的**指标**。
//
// **一个数报到哪，判据是这个**：可聚合的计数与时长走指标——那张表可查询可聚合，「这个库最近
// 十次扫描平均处理了多少归档」因此是一句 SQL；描述「这次是在什么条件下跑的」走**任务参数**
// 或**上限**表；同一件事只报一次。报文里那四个描述性值因此不在这里——发起声明
// （见 Service.ScanLibrary）与上限表里已经各有一份，而这一路解析的是同一条存储策略。
//
// 走整帧上报而不是**累加指标**：单库扫描的每份进度报文送的已经是快照绝对值（见
// scanner.ScanProgressReport.Metrics），累加会把收尾的总量再加到那份绝对值上，翻一倍。
// 累加那条通道属于跨资料库的运行（见 rebuildThumbLibrary.Metrics），那里每份报文只覆盖一个库。
//
// 指标一次报完，不拆成多次写入——理由见 runhandle.Handle.Report。
func (o *taskScanObserver) Metrics(report scanner.ScanMetricsReport) {
	o.progress.Report(runhandle.Frame{Metrics: MetricValues(report)})
}

// MetricValues 把一份扫描指标报文转换成运行指标。
func MetricValues(report scanner.ScanMetricsReport) map[string]int64 {
	return map[string]int64{
		"discovered_archives":      report.DiscoveredArchives,
		"skipped_archives":         report.SkippedArchives,
		"processed_archives":       report.ProcessedArchives,
		"opened_archives":          report.OpenedArchives,
		"hashed_files":             report.HashedFiles,
		"queued_covers":            report.QueuedCovers,
		"failed_archives":          report.FailedArchives,
		"rehomed_books":            report.RehomedBooks,
		"stale_series_stats":       report.StaleSeriesStats,
		"format_filtered_archives": report.FormatFilteredArchives,
		"io_wait_ms":               report.IOWaitMillis,
		"paused_ms":                report.PausedMillis,
		"duration_ms":              report.DurationMillis,
	}
}

// ProgressFrame 把扫描器的一份进度报文翻成**一帧**任务进度。
//
// 一份报文里计数、阶段、当前条目与指标同时变，只能整帧报（理由见 runhandle.Handle.Report）。
func ProgressFrame(report scanner.ScanProgressReport) runhandle.Frame {
	metrics := make(map[string]int64, len(report.Metrics))
	for key, value := range report.Metrics {
		metrics[key] = value
	}
	current := int(report.Current)
	total := int(report.Total)
	frame := runhandle.Frame{
		Current: &current,
		Total:   &total,
		Phase:   report.Phase,
		Item:    report.CurrentItem,
		Code:    "task.msg.scan.scanning",
		Metrics: metrics,
	}
	if report.CurrentItem != "" {
		frame.Code = "task.msg.scan.scanning_item"
		frame.Params = map[string]string{"item": filepath.Base(report.CurrentItem)}
	}
	return frame
}

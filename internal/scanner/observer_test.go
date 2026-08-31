// 本包用例共用的**扫描观察者**探针。

package scanner

import (
	"context"
	"sync"
	"testing"
)

// spyObserver 记下一次扫描交给观察者的全部报文。
//
// 它自带一把锁：扫描 worker 与封面 worker 会并发调用两个方法，这正是 ScanObserver
// 对实现方的要求，探针不该是唯一违反它的那个。
type spyObserver struct {
	mu       sync.Mutex
	progress []ScanProgressReport
	metrics  []ScanMetricsReport
}

func (s *spyObserver) Progress(report ScanProgressReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress = append(s.progress, report)
}

func (s *spyObserver) Metrics(report ScanMetricsReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = append(s.metrics, report)
}

// lastMetrics 返回收尾指标；一次扫描恰好报一次，没报过时返回零值。
func (s *spyObserver) lastMetrics() ScanMetricsReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.metrics) == 0 {
		return ScanMetricsReport{}
	}
	return s.metrics[len(s.metrics)-1]
}

// phases 按到达顺序返回观察到的**阶段**序列。
func (s *spyObserver) phases() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.progress))
	for _, report := range s.progress {
		out = append(out, report.Phase)
	}
	return out
}

// TestScanReportsToTheObserverHandedToIt 守这条接缝本身：一次真实扫描把首尾两个**阶段**
// 与收尾指标交给**它这次收到的那个**观察者，而不是交给某个装配期注册的回调。
//
// 中间阶段不断言：它们受节流与文件数影响，钉住会变成在验证扫描器的实现细节。
func TestScanReportsToTheObserverHandedToIt(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	seedTwoFormats(t, libraryPath)
	s := newFormatTestScanner(t, store)

	observer := &spyObserver{}
	if err := s.ScanLibrary(context.Background(), lib.ID, lib.Path, true, observer); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	phases := observer.phases()
	if len(phases) < 2 {
		t.Fatalf("观察者只收到 %d 条进度 —— 交出去的观察者没被用上", len(phases))
	}
	if phases[0] != "loading_existing_books" || phases[len(phases)-1] != "completed" {
		t.Fatalf("首尾阶段为 %q..%q, want loading_existing_books..completed", phases[0], phases[len(phases)-1])
	}
	if got := len(observer.metrics); got != 1 {
		t.Fatalf("收尾指标报了 %d 次, want 1", got)
	}
	if observer.lastMetrics().DiscoveredArchives != 2 {
		t.Fatalf("收尾指标没到这个观察者手里：%+v", observer.lastMetrics())
	}
}

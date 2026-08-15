// **资料库扫描**与**系列扫描**任务的**进度句柄**保管处，按扫描对象索引。
//
// 这两个任务的写入者是扫描器回调：装配期注册一次、对所有扫描生效，不在任何任务体的调用栈上，
// 句柄因而必须先由任务体登记出来（所有权模型见 TaskProgress）。取不到句柄即「这次扫描
// 不属于任何任务」——守护扫描、watcher 触发的扫描与建库后的首扫都是如此。

package api

import "sync"

// scanTarget 是一次扫描作用的对象：一个资料库，或一个系列。
//
// 登记方与写入方必须用同一个身份，且这个身份不得是各自拼出来的**任务键**字符串——
// 拼法只要有一边对不上就不会有编译错误，后果是扫描全程进度条一动不动。
type scanTarget struct {
	Scope string
	ID    int64
}

func libraryScanTarget(libraryID int64) scanTarget {
	return scanTarget{Scope: "library", ID: libraryID}
}

func seriesScanTarget(seriesID int64) scanTarget {
	return scanTarget{Scope: "series", ID: seriesID}
}

// scanTargetOf 把扫描器报文的作用域归一成 scanTarget：扫描器只报 "library" 与 "series"，
// 其余一律按资料库处理。
func scanTargetOf(scope string, id int64) scanTarget {
	if scope == "series" {
		return seriesScanTarget(id)
	}
	return libraryScanTarget(id)
}

// scanProgressHandles 按扫描对象保管在跑的扫描任务的进度句柄。
//
// 须经 newScanProgressHandles 构造；所有方法对 nil 接收者安全，与 rebuildThumbAggregator 一致——
// 白盒测试手工拼装 Controller 时漏掉本组件，应表现为「进度无处可报」而不是 nil 解引用 panic。
type scanProgressHandles struct {
	mu      sync.Mutex
	handles map[scanTarget]*TaskProgress
}

func newScanProgressHandles() *scanProgressHandles {
	return &scanProgressHandles{handles: make(map[scanTarget]*TaskProgress)}
}

// track 登记本次扫描任务的进度句柄，返回交回句柄的函数；任务体须以 defer 调用它，
// 这样 panic 路径也会交回。
func (h *scanProgressHandles) track(target scanTarget, progress *TaskProgress) func() {
	if h == nil || progress == nil {
		return func() {}
	}
	h.mu.Lock()
	if h.handles == nil {
		h.handles = make(map[scanTarget]*TaskProgress)
	}
	h.handles[target] = progress
	h.mu.Unlock()

	// 只交回**自己那份**句柄。同一个对象若已被下一次扫描登记，无差别 delete 会连新任务的句柄
	// 一起抹掉，那次扫描从此一条进度也报不出来。
	return func() {
		h.mu.Lock()
		if h.handles[target] == progress {
			delete(h.handles, target)
		}
		h.mu.Unlock()
	}
}

// lookup 取出该扫描对象的进度句柄；nil 表示这次扫描不属于任何任务，其进度无处可报。
func (h *scanProgressHandles) lookup(target scanTarget) *TaskProgress {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.handles[target]
}

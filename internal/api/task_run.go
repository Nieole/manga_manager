package api

import (
	"errors"

	"manga-manager/internal/task"
)

// TaskVariant 是**变体**：同一类工作在同一个作用域上的第二个身份。
//
// 它是独立类型而不是 string，为的是让身份构造函数里那两个都叫「一串字符」的位置——任务类型与
// 变体——接反时有编译错误。
type TaskVariant string

const (
	// variantSole 是「这个类型在一个作用域上只有一个身份」。它是一次显式声明，不是省略：
	// 身份四要素没有「不填」这个选项，见 TaskIdentity。
	variantSole TaskVariant = ""

	// 书哈希重建的两个变体：前台重建大批次、无停顿；低优先级回填压低批次、批间停顿、
	// 匹配模式钉死二进制哈希。**重启函数**按（类型，变体）分发，两条跑法因此各自重启回自己。
	variantHashRebuildForeground TaskVariant = "foreground"
	variantHashRebuildBackfill   TaskVariant = "low_priority_backfill"

	// 刮削的两个变体：全库那条挂在系统作用域，单库那条带库作用域。
	variantScrapeAllLibraries TaskVariant = "all_libraries"
	variantScrapeOneLibrary   TaskVariant = "one_library"
)

// 任务的三个**作用域**。它们同时是身份行 scope 列的取值与任务列表的筛选值。
const (
	taskScopeSystem  = "system"
	taskScopeLibrary = "library"
	taskScopeSeries  = "series"
)

// TaskIdentity 是一个任务的**身份**：类型、**作用域**、作用域 id 与**变体**四项唯一确定它。
// 四项由启动点声明，引擎不推导其中任何一项——包括不从**任务键**的字符串里反解作用域。
//
// 字段不导出，因此建它只有 systemTask / libraryTask / seriesTask 三条路。三者都把四项收成
// 位置参数，少填一项是**编译错误**：结构体字面量漏一个字段只会得到零值，而一个默默判成系统级、
// 或默默丢掉作用域 id 的任务不会有任何报错，只会挂在任务中心里错的那个作用域下。
type TaskIdentity struct {
	taskType string
	scope    string
	scopeID  *int64
	variant  TaskVariant
}

// systemTask 声明一个系统级任务的身份。
func systemTask(taskType string, variant TaskVariant) TaskIdentity {
	return TaskIdentity{taskType: taskType, scope: taskScopeSystem, variant: variant}
}

// libraryTask 声明一个资料库级任务的身份。
//
// 作用域 id 是位置参数而不是一个可以留 nil 的字段：库级任务没有「不知道是哪个库」这种状态，
// 启动点调它的那一刻手里就握着 lib.ID。
func libraryTask(taskType string, libraryID int64, variant TaskVariant) TaskIdentity {
	return TaskIdentity{taskType: taskType, scope: taskScopeLibrary, scopeID: &libraryID, variant: variant}
}

// seriesTask 声明一个系列级任务的身份，作用域 id 的道理同 libraryTask。
func seriesTask(taskType string, seriesID int64, variant TaskVariant) TaskIdentity {
	return TaskIdentity{taskType: taskType, scope: taskScopeSeries, scopeID: &seriesID, variant: variant}
}

// taskLaunchError preserves the HTTP conflict contract for application launches.
func taskLaunchError(err error) error {
	if errors.Is(err, task.ErrRunAlreadyActive) || errors.Is(err, task.ErrRunAlreadyQueued) {
		return errTaskAlreadyRunning
	}
	return err
}

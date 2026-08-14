// 业务说明：本文件是任务引擎那**唯一一个 seam** 的共用测试装置。
//
// 该 seam 就是引擎的构造点 `newTaskEngine`：注入落盘存储、SSE 投递、停机信号与
// 「开一个受停机管辖的 goroutine」的能力。把后台能力换成同步执行版之后，任务的**终态**在
// 调用返回时就已落定，不必 sleep 或轮询去等一个真实 goroutine——这是契约用例能做到毫秒级、
// 且不需要数据库/配置/扫描器的全部原因。
//
// 观测面固定为**投递出去的载荷**：与 SSE 订阅者看到的完全一致，不断言内部字段布局。
// 本文件不含任何用例，只供 task_panic_test.go 与 task_run_test.go 共同消费。

package api

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock 让节流的时序断言可控。固定 sleep 的用例既慢，又杀不掉
// 「水位只在首次写入、之后再不更新」这类错误实现。
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newBackgroundTestEngine 造一个后台能力可控的引擎：run 决定任务体何时、乃至是否执行。
func newBackgroundTestEngine(run func(func())) (*taskEngine, func() []TaskStatus) {
	var mu sync.Mutex
	var published []TaskStatus
	e := newTaskEngine(nil, func(payload string) {
		var task TaskStatus
		if err := json.Unmarshal([]byte(strings.TrimPrefix(payload, "task_progress:")), &task); err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		published = append(published, task)
	}, nil, run)
	return e, func() []TaskStatus {
		mu.Lock()
		defer mu.Unlock()
		return append([]TaskStatus(nil), published...)
	}
}

// runTaskBodySynchronously 是「同步执行」版的后台能力：任务体在启动调用返回前就跑完。
func runTaskBodySynchronously(fn func()) { fn() }

// lastPublishedTask 返回该任务键最后一条被投递出去的快照。
func lastPublishedTask(t *testing.T, snapshots []TaskStatus, key string) TaskStatus {
	t.Helper()
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].Key == key {
			return snapshots[i]
		}
	}
	t.Fatalf("任务 %q 一条快照都没被投递出去", key)
	return TaskStatus{}
}

// firstPublishedTask 返回该任务键**第一条**被投递出去的快照，用于断言任务诞生那一刻就已带齐
// 作用域、元数据与并发上限——此前它们是启动之后三次独立的写入，中间存在可被观察到的空窗。
func firstPublishedTask(t *testing.T, snapshots []TaskStatus, key string) TaskStatus {
	t.Helper()
	for _, snapshot := range snapshots {
		if snapshot.Key == key {
			return snapshot
		}
	}
	t.Fatalf("任务 %q 一条快照都没被投递出去", key)
	return TaskStatus{}
}

// 守「清除终态任务」不会被在途落盘写回来：flushTaskPersist 把待写集合换出去之后才在锁外写库，
// 那一批已经不在待写集合里，清除只能等它写完再删。
// 漏掉这条约束时，用户点下「清除终态任务」列表当场空掉，刷新一下任务又回来几条。

package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// upsertHookStore 在每笔 UpsertTask 落库之前把控制权交给用例，用来把「清除」精确地放进
// 一批在途落盘的中间——这正是待写集合里的 delete 够不着的那个窗口。
type upsertHookStore struct {
	database.Store
	onUpsert func(key string)
}

func (s *upsertHookStore) UpsertTask(ctx context.Context, task database.TaskRecord) error {
	if s.onUpsert != nil {
		s.onUpsert(task.Key)
	}
	return s.Store.UpsertTask(ctx, task)
}

// waitForTaskClearedFromMemory 等清除把这条任务从内存表里摘掉。
//
// 判据是条件而不是时长，因此不看机器快慢：清除先清内存、再删库，两种实现都会走到这一步，
// 于是「删库」与「在途落盘」的先后被固定下来——正确实现的删库排在落盘之后，错误实现排在之前。
// 超时只是死等的保险丝，走到它即说明清除的两步顺序被调换过。
func waitForTaskClearedFromMemory(t *testing.T, e *taskEngine, key string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.mutex.Lock()
		_, present := e.tasks[key]
		e.mutex.Unlock()
		if !present {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("清除迟迟没把 %q 从内存表里摘掉：它把删库排在了清内存之前，本用例的时序前提不再成立", key)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestClearIsNotUndoneByInFlightPersist 钉住清除与落盘的互斥：清除返回之后，库里不得再有那条任务。
func TestClearIsNotUndoneByInFlightPersist(t *testing.T) {
	cases := []struct {
		name    string
		filters database.TaskFilters
	}{
		{"清除全部终态任务", database.TaskFilters{}},
		{"按状态清除", database.TaskFilters{Status: "completed"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, store, _, _ := newTestController(t)
			hooked := &upsertHookStore{Store: store}
			engine := newTaskEngine(hooked, nil, nil, runTaskBodySynchronously, nil)

			const key = "scan_library_7"
			seedTask(t, engine, taskSeed{Key: key, Type: "scan_library", Total: 1, Terminal: "completed"})

			var (
				once     sync.Once
				cleared  = make(chan struct{})
				removed  int64
				clearErr error
			)
			hooked.onUpsert = func(string) {
				// 这一刻快照已被换出待写集合、尚未写进库：用户恰在此时点下「清除终态任务」。
				once.Do(func() {
					go func() {
						defer close(cleared)
						removed, clearErr = engine.clear(context.Background(), tc.filters)
					}()
				})
				waitForTaskClearedFromMemory(t, engine, key)
			}

			engine.flushTaskPersist()
			<-cleared
			hooked.onUpsert = nil

			if clearErr != nil {
				t.Fatalf("清除终态任务失败: %v", clearErr)
			}
			records, err := store.ListTasks(context.Background(), database.TaskFilters{})
			if err != nil {
				t.Fatalf("读回任务表失败: %v", err)
			}
			for _, record := range records {
				if record.Key == key {
					t.Fatalf("清除（removed=%d）之后 %q 又被在途落盘写回库里：用户刷新一下，清掉的任务回来了", removed, key)
				}
			}
		})
	}
}

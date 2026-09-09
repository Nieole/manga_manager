// 守重试重放的是**哪一条运行**的入参：这个任务最近一条进入**终态**的运行。
//
// 重试的意思是「那次跑完的，再跑一次」——**排队中**与**运行中**的都还没跑完，身上没有可重放的东西。
// 而序号在入队与每次**合并**时都会重取，只要这个任务有一条排队中的运行，它就永远是「最近那一条」：
// 按序号取而不挑状态的话，用户点的是那条刚失败的卡片，重放的却是一条还没开跑的运行的入参。

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// seedScanLibrary 建一个真的能查回来的资料库：扫描的**重启函数**要读它的名字与存储画像，
// 读不到就一条运行都发不起来（那是 500，不是重放了错的东西）。
func seedScanLibrary(t *testing.T, c *Controller) (database.Library, string, TaskIdentity) {
	t.Helper()
	lib, err := c.store.CreateLibrary(t.Context(), database.CreateLibraryParams{
		Name:         "Main",
		Path:         t.TempDir(),
		ScanMode:     "manual",
		ScanInterval: 60,
		ScanFormats:  config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("建资料库失败: %v", err)
	}
	return lib, fmt.Sprintf("scan_library_%d", lib.ID), libraryTask("scan_library", lib.ID, variantSole)
}

// TestRetryReplaysTheForcedScanThatJustFailed 钉住刚失败的那次带**强制**时，重试重放的仍是强制扫描。
//
// **强制**是一次运行的属性而不是一个身份（见 CONTEXT.md 的**变体**）：同一个任务的两次运行，
// 一次强制一次不强制。丢掉它的后果是用户点重试之后跑的是一次增量扫描——那次扫描会跳过它本来要
// 重读的每一个归档，看上去几秒就「成功」了，而用户当初点强制正是因为增量拦截漏了东西。
func TestRetryReplaysTheForcedScanThatJustFailed(t *testing.T) {
	c, _, _, _ := newTestController(t)
	_, key, identity := seedScanLibrary(t, c)

	// 刚失败的那一次是**强制**扫描。
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 1,
		Metadata: map[string]string{"force": "true"},
		Terminal: "failed",
	})
	// 它后面又起了一条普通扫描，此刻正在跑：序号比那条失败的新，但它还没跑完。
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 1,
		Metadata:  map[string]string{"force": "false"},
		CanCancel: true,
	})

	if rec := retryTaskByKey(t, c, key); rec.Code != http.StatusAccepted {
		t.Fatalf("重试返回 %d, want 202: %s", rec.Code, rec.Body.String())
	}

	replay := currentTask(t, c.taskEngine, key)
	if replay.Params["force"] != "true" {
		t.Fatalf("重放出来的那条运行 force=%q, want \"true\" —— 重试跑成了增量扫描，"+
			"而用户当初点强制正是因为增量漏了东西：%v", replay.Params["force"], replay.Params)
	}
}

// TestRetryReplaysTheTerminalRunWhileAnotherIsQueued 钉住一个任务同时有一条**运行中**与一条
// **排队中**的运行时，重试重放的仍是最近那条**终态**运行的入参。
//
// 这个局面是队列的常态而不是巧合：**排队中**既不在活动集也不是终态，因此一条在跑、一条排队
// 可以同时成立，而排队那条的序号恰恰是全场最新的——入队与每次**合并**都会重取。
// 按序号取而不挑状态的话，重放的是一条还没开跑的运行的入参；只排除排队中的话，重放的是那条
// 正在跑的。两种都不是用户点的那张卡片。
//
// **这一条断在取快照这个缝上，而不是端点上**，是这个局面自己决定的：已经排着一条运行时，
// 重试会被**合并**进那一条（不新建运行，被并掉的是后来那份声明）。因此无论取快照挑中了哪一条，
// 端点侧的结果都一样——202、库里不多一条运行、排队那条的入参一字不变。唯一有分别的是
// 「读了谁的入参」。同一条规则在端点上看得见的那一半由 TestRetryReplaysTheForcedScanThatJustFailed
// 守着：那里没有排队的运行，重放因此真的落成一条新的。
func TestRetryReplaysTheTerminalRunWhileAnotherIsQueued(t *testing.T) {
	c, _, _, _ := newTestController(t)
	const key = "scan_series_42"
	identity := seriesTask("scan_series", 42, variantSole)

	// 用户看着的那一条：跑完了、失败了，带着它自己那份入参。
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 1,
		Metadata: map[string]string{"force": "true"}, Terminal: "failed",
	})
	// 之后又起了一条，此刻正在跑。
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 1,
		Metadata:  map[string]string{"force": "false"},
		CanCancel: true,
	})
	// 再起一条：撞上活动运行，被准入闸门拦在**排队中**。它的序号是全场最新的。
	if _, err := trySeedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: identity, Total: 1,
		Metadata: map[string]string{"force": "false"},
	}); !errors.Is(err, errSeededRunQueued) {
		t.Fatalf("第三次发起落成了 %v, want 排队中 —— 这条用例要的那个局面没成立", err)
	}

	snapshot, err := c.taskEngine.snapshotForRetry(t.Context(), taskIDForKey(t, c.taskEngine, key))
	if err != nil {
		t.Fatalf("取重试快照失败: %v", err)
	}
	if snapshot.Status != "failed" || snapshot.Params["force"] != "true" {
		t.Fatalf("取回的是一条 %q 的运行、入参 %v, want failed + force=true —— 重试重放的不是用户点的那张卡片",
			snapshot.Status, snapshot.Params)
	}
}

// TestRetryWithoutATerminalRunSaysThereIsNothingToReplay 钉住这个任务一次都没跑完时，
// 重试回的是 404 加一句**真的原因**。
//
// 「跑完过的一次都没有」不是内部错误，因此不该是 500；也不是「这个任务不存在」——它就列在任务
// 中心里，正跑着。答成后者的话，用户对着屏幕上明明白白的那一行被告知「任务不存在」，
// 于是去找一个并不存在的数据丢失问题。
func TestRetryWithoutATerminalRunSaysThereIsNothingToReplay(t *testing.T) {
	c, _, _, _ := newTestController(t)
	const key = "scan_series_43"
	seedTask(t, c.taskEngine, taskSeed{
		Key: key, Identity: seriesTask("scan_series", 43, variantSole), Total: 1, CanCancel: true,
	})

	rec := retryTaskByKey(t, c, key)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("重试一个一次都没跑完的任务返回 %d, want 404: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解响应体失败: %v (raw=%s)", err, rec.Body.String())
	}
	if payload["error"] != "No finished run to retry" {
		t.Fatalf("响应说的是 %q —— 这个任务就在任务中心里正跑着，说它不存在会把用户支去查一个不存在的问题",
			payload["error"])
	}
}

# 06 — 测试装置也在构造期收下磁盘作业入口

**What to build:** `newBackgroundTestEngine` 增加一个 `diskWork *diskwork.Runner` 形参，
三处靠 `e.diskWork = ...` 补接线的测试装置改走形参；该函数 doc 里那句「装置直接赋值」随之删除。

**Blocked by:** 无（`refactor/task-engine-run` 的五张票已全部落地）

**Status:** done

## 为什么

生产的 `newTaskEngine` 在构造期收下磁盘作业入口，而测试装置靠调用方事后赋值字段。
两条路子并存时，「这个引擎的句柄能不能发起**磁盘作业**」要读 doc 才知道，而漏接线的后果是
句柄的 `disk` 为 nil、任务体一发起磁盘作业就 panic——票 03 的 `comicinfo_task_test.go` 已经踩过一次。

本票不改任何生产代码，也不改任何断言。

## 取用点

`newBackgroundTestEngine` 约 32 个调用点。绝大多数传 `nil` 即可——只有确实要发起磁盘作业的
那几处传真 runner，它们今天正是靠事后赋值 `e.diskWork` 认出来的，grep 得到。

## 验收

- [ ] `newBackgroundTestEngine` 的签名带 `diskWork *diskwork.Runner`，全部调用点已补实参
- [ ] 全仓再无 `e.diskWork = ` 这种事后赋值
- [ ] 该函数 doc 里交代「装置直接赋值」的那句已删（主语不存在了）
- [ ] 断言一条未改：`GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [ ] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过

## 由来

`.scratch/task-run-handle/deferred.md` 的 D2。票 02 的 code-review 提过，当时以「32 个调用点、
收益不抵 churn」挂账，由用户在批次收尾后裁定为单独一票。

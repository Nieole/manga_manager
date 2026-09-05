# 04 — 外部库传输扫描收下句柄

**What to build:** `internal/external` 的传输扫描改成收下一个只有**计数推进**的窄接口，
替掉 `progress func(current, total int)` 形参；`api` 侧用嵌入**任务句柄**的小适配器承载帧构造；
包内用例改用手写假体。

**本票零对外行为变化。**

**Blocked by:** 02

**Status:** done

## 窄接口

`external` **自己声明**接口，**不** import `internal/taskrun`。它只需要一个方法：
**计数推进**。这个包不发起**磁盘作业**（它不 import `diskwork`），包内也没有闸门调用——
传输循环那一处闸门在 `api` 侧，已由票 03 处理。

接口的 doc 直接沿用今天那个形参的 doc 论断：进度只报两个计数、不带展示文案，
用户可见文字由调用方按语种渲染，本包渲染的话英文用户会看到中文。**这条理由原样适用，不必重写。**

这一票是全套里最小的一处，它的价值在于让 `progress func(current, total int)` 这个形状在全仓
只剩一种含义——否则票 02 之后 `external` 会是最后一个还在用旧形状的包。

## `api` 侧适配器

1 处调用点，1 个适配器，写法与票 02 一致：嵌入 `*taskrun.Handle`，只遮蔽计数推进。
这个任务的帧构造今天写在闭包里，搬进适配器时**不改帧的内容**。

## 包内用例

`internal/external` 的传输扫描用例改用手写假体驱动窄接口——本来就不需要调度器，
所以这里的收益只是把「协作方是什么」写清楚，用例结构基本不动。

## 会变红的既有用例

- `api.external_tasks_test.go` 里驱动传输扫描的那几条 —— 形参类型变化，帧内容不变。
- `internal/external/prepare_transfer_test.go` —— 与本票正交（它测的是传输规划，不是扫描进度），
  若变红说明改动溢出了。

## 验收

- [ ] `internal/external` 声明了自己的单方法窄接口，且**不** import `internal/taskrun`
- [ ] `progress func(current, total int)` 形参已删；全仓不再有这个形状的裸回调
- [ ] `api` 侧 1 个适配器，帧内容逐字不变
- [ ] 任务面板输出逐字不变——本票不产生任何对外行为变更
- [ ] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [ ] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过

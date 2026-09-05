# 03 — api 自己的八处闸门与两处磁盘作业改走句柄

**What to build:** `internal/api` 任务体里 8 处手抄的**暂停闸门**调用与 2 处直接发起的
**磁盘作业**改成经**任务句柄**；这几处的 `taskIOMetrics` 声明与穿参数随之消失。

**本票零对外行为变化。** 文件身份重建今天就是在错误检查之后才计数，与票 01 钉下的规则一致；
另一处磁盘作业今天丢弃实况，本票之后**仍然不上报**——上报是显式的，不改这一处的选择。

**Blocked by:** 02

**Status:** done

## 取用点

8 处闸门调用分布在四个文件里：刮削任务 3 处、维护任务 3 处、外部库传输 1 处、AI 分组 1 处。
它们全是循环顶部或工序之间的可中断点，形状一致，逐处换成句柄上的过闸门方法即可。

2 处磁盘作业：

| 取用点 | 工种 | 实况今天怎么处理 | 本票之后 |
| --- | --- | --- | --- |
| 文件身份重建 | `identity_hash` | 手工吸收进 `taskIOMetrics` | 句柄自己吸收 |
| ComicInfo 回写 | `metadata_scan` | **丢弃**（`if _, err := ...`） | 仍不上报，但句柄会吸收 |

ComicInfo 回写那一处只是不再显式丢弃返回值；它**不**开始上报 IO 指标，因为上报由任务体决定，
而这个任务今天没有选择上报。这样本票的任务面板输出逐字不变。

## 随之消失的样板

文件身份重建与低优先级哈希回填两个函数今天各有一个
`progress func(current, total int, metrics taskIOMetrics)` 形参——第二个形参是为了把实况穿回
调用方。句柄自己持累加器之后，这两个形参收窄回 `func(current, total int)`，
或者干脆由适配器承载（与票 02 同一种写法）。

`taskIOMetrics` 这个类型本身在票 05 才删——本票之后它应当**只剩下类型声明与那三个方法**，
不再有任何取用点声明它的实例。

## 会变红的既有用例

- `api.TestHashProgressFrameIsPublishedWhole` —— 守「哈希进度整帧一次报出」。形状不变，
  实况来源改为句柄。**断言的行为必须保持。**
- `api.TestRebuildFileIdentitiesCompletesWithCounts` / `...CancellationLandsCancelled` ——
  终态与计数不变，只是任务体内部换了取用方式；应当只需最小改造。
- ComicInfo 回写那组用例 —— 输出逐字不变，若变红说明本票多做了事。

## 验收

- [ ] `internal/api` 生产代码里不再有手抄的闸门调用（8 处清零）
- [ ] 2 处磁盘作业改走句柄；ComicInfo 回写的任务面板输出逐字不变
- [ ] 两个 `progress func(..., metrics taskIOMetrics)` 形参的第二个参数消失
- [ ] `taskIOMetrics` 已无实例声明点（类型与方法留到票 05 删）
- [ ] 任务面板输出逐字不变——本票不产生任何对外行为变更
- [ ] 上述三组既有用例按上节处置
- [ ] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [ ] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过

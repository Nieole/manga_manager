# 外接机械盘 IO 基准采集指南

更新日期：2026-05-25

本指南配套 `cmd/storageiobench`，用于采集外接机械盘资源库在扫描、抽样读取、并发读取、阅读延迟探针和缓存小文件写入场景下的基线数据。工具只读取资源库文件，不会修改漫画目录；写入测试只在指定 `cache` 目录下创建临时目录，结束后自动删除。

## 推荐命令

推荐优先使用封装脚本，它会固定输出到 `docs/performance-baselines/`，并自动生成带时间戳的报告文件：

```powershell
.\scripts\storage-io-baseline.ps1 `
  -Library "E:\Manga" `
  -Cache "C:\Users\saber\dev\manga_manager\data\storage-io-bench" `
  -Label "external-hdd" `
  -Profile "hdd_external" `
  -Notes "USB 3.0 external 2.5-inch HDD"
```

如需同时采集 SSD 对照组：

```powershell
.\scripts\storage-io-baseline.ps1 `
  -Library "E:\Manga" `
  -Cache "C:\Users\saber\dev\manga_manager\data\storage-io-bench" `
  -Label "external-hdd" `
  -Profile "hdd_external" `
  -CompareLibrary "D:\MangaSample" `
  -CompareLabel "internal-ssd" `
  -CompareProfile "ssd"
```

底层工具也可以直接运行：

```powershell
go run ./cmd/storageiobench `
  -library "E:\Manga" `
  -cache "C:\Users\saber\dev\manga_manager\data\storage-io-bench" `
  -out "docs\performance-baselines\2026-05-25-external-hdd-storage-io.md" `
  -label "external-hdd" `
  -profile "hdd_external" `
  -notes "USB 3.0 external 2.5-inch HDD" `
  -read-mb 512 `
  -max-files 300 `
  -write-files 512 `
  -write-kb 64 `
  -cover-samples 128 `
  -cover-read-kb 512 `
  -cover-write-kb 96 `
  -cover-concurrency 1 `
  -reader-probes 40 `
  -reader-kb 256 `
  -background-readers 2
```

## 输出指标

- `walk+stat`：目录遍历和文件属性读取耗时，接近日常 fast scan 的文件系统压力。
- `sequential-read-c1`：单并发顺序读取抽样归档，接近外接 HDD 的低冲击读取模式。
- `concurrent-read-c2` / `concurrent-read-c4`：多并发读取对比，用于判断提高并发是否真的提升吞吐。
- `small-file-write`：缓存目录小文件写入能力，接近批量缩略图写入压力。
- `cover-rebuild-sim`：按样本归档读取少量数据并写出模拟缩略图，用于估算全量封面重建的读写压力。
- `reader-latency-unthrottled`：后台读取无低冲击调度时的阅读探针延迟。
- `reader-latency-low-impact`：后台读取进入 reader-priority 调度后的阅读探针延迟。
- `Decision Summary`：根据吞吐、阅读 P95 和小文件写入结果给出并发与缓存位置建议。

## 判断规则

- 如果 `c2/c4` 相比 `c1` 吞吐没有明显提升，外接 HDD 应保持 `archive_open_concurrency = 1`。
- 如果 `small-file-write` 很慢，且 cache 与 library 同盘，应把 `cache.dir` 移到 SSD，或保持同盘页面缓存关闭。
- 如果 `reader-latency-low-impact` 的 P95 明显低于 `reader-latency-unthrottled`，说明低冲击调度对阅读翻页有保护效果。
- 如果 `Decision Summary` 建议 `archive_open_concurrency = 1`，不要为了吞吐提高外接 HDD 并发。
- SSD 与 HDD 策略对比时，对两个路径分别运行同一命令，只修改 `-library`、`-cache`、`-label` 和 `-profile`，然后对比两份报告的 `Decision Summary`。
- 采集真实外接 HDD 时，需要同时记录 Windows 任务管理器中的活动时间、平均响应时间和用户主观卡顿。

## 建议记录

- 资源库所在盘符和连接方式。
- cache 目录所在盘符。
- 是否同盘。
- 运行期间 Windows 任务管理器磁盘活动时间峰值。
- 运行期间手动打开外接盘目录是否卡顿。
- 运行期间阅读翻页是否有明显等待。

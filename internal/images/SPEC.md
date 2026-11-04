---
id: mm-images
type: module-design
status: active
title: 页图与封面派生处理
parent: mm-architecture
---

## 职责

把输入图片字节按选项解码、裁边、缩放及编码，生成页图或封面的派生字节；封装可选外部 AI 引擎与本进程图片处理并发控制。

## 公开契约

- `ProcessImage` 返回字节与内容类型，`ProcessImageDetailed` 额外表达透传和跳过滤镜等结果；支持 jpeg/png/webp/avif 输出。无有效处理需求时可原样透传，不能据请求的滤镜名断言必经重编码。
- `ProcessOptions.FitInside` 等比缩进框且不放大；未设置时双边尺寸采用画布精确缩放。`ValidateTargetDimensions` 限制单边与面积，解码前另有源图像素预检；`SnapTargetDimension` 为阅读器尺寸分档，不等于任意目标尺寸的校验。
- AI 滤镜通过临时沙盒运行外部引擎；失败会标记 `SkippedFilter`，有尺寸处理需要时可回退软件缩放，无需其他处理时可交还源字节，显式格式要求仍可执行。不承诺引擎存在或 AI 必成功。
- `InitProcessor` 重建 AI 与软件并发闸，软件上限取 CPU 核数；每次申领使用同一通道快照完成释放，重建不追溯限制已在旧通道执行的处理。

## 边界与限制

不认识书或资料库，不管理原始页读取、缓存、ETag、HTTP 或卷 IO 许可；除 AI 临时沙盒外不承担文件读写。配置派生效果由 `runtimecfg` 转入，不能在这里另建配置来源。图片 CPU/AI 并发闸既不是任务运行槽位，也不是卷 IO 调度。

## 证据

`ProcessImageDetailed`、`ProcessOptions`、`ValidateTargetDimensions`、`InitProcessor`；`processor_process_test.go` 的 `TestProcessImageFitInsidePreservesAspect`、`TestProcessImageFitInsideDoesNotUpscale`、`TestProcessImageAIFailureDoesNotFakeUpscale`、`TestProcessImageRejectsDecodeBomb`，以及 `processor_semaphore_test.go`、`processor_stride_test.go`。

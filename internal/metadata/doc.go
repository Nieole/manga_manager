// Package metadata 把外部漫画数据源与 LLM 收拢到 Provider / AIProvider 两个契约：按标题取回候选
// 元数据（SeriesMetadata），并基于候选系列产出 AI 推荐与合集分组结果，输出语言取自 WithLocale。
//
// 出站请求的自我约束在包内：按数据源的进程级并发闸门、认 Retry-After 且可被取消的重试退避、
// 错误里的密钥脱敏与上游响应体截断；跨源的连载状态口径在 NormalizeStatusCodeOptional 归一，
// 数据源没给或写法认不出时留空、不折成 unknown。
//
// 边界：只产出内存里的结果，不碰数据库、不落盘、不下载封面。把结果变成提案、裁决后写入系列、
// 以及刮削任务的调度都属于 internal/api；归档内嵌的 ComicInfo.xml 属于 internal/parser。
package metadata

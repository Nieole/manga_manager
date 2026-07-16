// 业务说明：本文件集中重建所有从运行时配置派生的进程级资源（归档句柄池、图像处理并发、日志级别），
// 使「配置文件热重载」与「经 API/UI 保存配置」两条路径应用完全一致的副作用——避免此前只有其一生效
// 导致的隐性 bug（例如经 UI 改 archive_pool_size / max_ai_concurrency 却不重建对应池，需重启才生效）。
// 维护要点：新增任何「随配置变化需要重建/重设的进程级资源」时，只在此登记一处即可让两条路径同步生效。

package runtimecfg

import (
	"manga-manager/internal/config"
	"manga-manager/internal/images"
	"manga-manager/internal/logger"
	"manga-manager/internal/parser"
)

// Apply 按给定配置重建所有配置派生的运行时资源。两条配置生效路径（文件热重载、API 保存）都调用它，
// 使它成为这些副作用的唯一事实来源。InitPool / InitProcessor 均为幂等（按新值重建/调整），可安全重复调用。
func Apply(cfg *config.Config) error {
	parser.InitPool(cfg.Scanner.ArchivePoolSize)
	images.InitProcessor(cfg.Scanner.MaxAiConcurrency)
	return logger.SetLevel(cfg.Logging.Level)
}

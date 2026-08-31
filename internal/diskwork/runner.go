package diskwork

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

// slowAcquireThreshold 是「等令牌等了很久」的观测阈值。没有它，慢盘上的排队对用户是无声的。
const slowAcquireThreshold = 250 * time.Millisecond

// Work 是一次磁盘作业的声明：干哪一类活、字节落在哪。
type Work struct {
	Kind storageio.WorkKind
	// Path 是字节落在哪，决定卷键。
	Path string
	// PolicyFrom 表达「和谁抢同一块盘」：与 Path 同卷时，IO 策略改从这个路径解析，卷键仍由 Path
	// 决定；留空表示策略也从 Path 解析。存储策略按路径前缀匹配资料库，而缓存目录通常落不进任何
	// 资料库的前缀、只能拿到全局默认档，可它和藏书在同一块盘上转——这个字段是那条规则的唯一表达。
	PolicyFrom string
}

// Stats 是一次磁盘作业在限流上的实况，由调用方折进自己的指标。
type Stats struct {
	// Wait 是申领令牌总共等了多久；PausedWait 是其中因暂停（后台整体暂停、为阅读让路）而等的部分。
	Wait       time.Duration
	PausedWait time.Duration
	// StorageProfile 与 VolumeKey 来自本次解析出的存储策略；短路未申领令牌时它们照样如实填好。
	StorageProfile string
	VolumeKey      string
}

// Runner 执行磁盘作业。它不认识资料库、系列与书，构造期只要两样能力：读取当前配置的函数、
// 发放令牌的调度器。两者都不得为 nil。
type Runner struct {
	config    func() config.Config
	scheduler *storageio.Scheduler
}

func NewRunner(cfg func() config.Config, sched *storageio.Scheduler) *Runner {
	return &Runner{config: cfg, scheduler: sched}
}

// Do 依次过暂停闸门、解析策略、按工种定上限、申领令牌，执行 fn，并在 fn 的每条出口（含 panic）
// 归还令牌。
//
// 闸门在未暂停时返回上下文错误，因此这一步同时是取消检查：上下文已取消时 fn 不执行，也不占用
// 令牌槽位。上限为零或卷键为空时不申领令牌，fn 照常执行——默认档与 SSD 档走的正是这条短路。
// 返回的 error 是闸门与令牌申领的错误，或 fn 自己的返回值；两者的分辨由调用方按需要自行安排。
func (r *Runner) Do(ctx context.Context, w Work, fn func() error) (Stats, error) {
	if err := taskcontrol.Wait(ctx); err != nil {
		return Stats{}, err
	}

	policy := r.resolvePolicy(w)
	stats := Stats{StorageProfile: policy.StorageProfile, VolumeKey: policy.VolumeKey}

	limit := concurrencyLimit(w.Kind, policy.IOPolicy)
	if limit <= 0 || strings.TrimSpace(policy.VolumeKey) == "" {
		return stats, fn()
	}

	lease, err := r.scheduler.Acquire(ctx, storageio.Request{
		VolumeKey:        policy.VolumeKey,
		Limit:            limit,
		Kind:             w.Kind,
		PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnly:         policy.IOPolicy.IdleOnlyHeavyTasks,
	})
	stats.Wait = lease.Wait
	stats.PausedWait = lease.PausedWait
	// 先记日志再判错：等了很久最终被取消的申领，正是这条观测要治的场景。
	if lease.Wait >= slowAcquireThreshold {
		slog.Info("Disk work waited for storage IO token",
			"work_kind", string(w.Kind),
			"path", w.Path,
			"storage_profile", policy.StorageProfile,
			"volume_key", policy.VolumeKey,
			"io_wait_ms", lease.Wait.Milliseconds(),
		)
	}
	if err != nil {
		return stats, err
	}
	defer lease.Release()

	return stats, fn()
}

// resolvePolicy 挑出本次作业生效的存储策略：字节落在哪决定卷键，和谁抢同一块盘决定 IO 策略。
func (r *Runner) resolvePolicy(w Work) config.ResolvedStoragePolicy {
	cfg := r.config()
	if strings.TrimSpace(w.PolicyFrom) != "" && config.SameVolume(w.PolicyFrom, w.Path) {
		policy := config.ResolveStoragePolicy(cfg, w.PolicyFrom)
		policy.VolumeKey = config.VolumeKey(w.Path)
		return policy
	}
	return config.ResolveStoragePolicy(cfg, w.Path)
}

// concurrencyLimit 按工种裁定并发上限：生效的那一项取决于这个工种到底在干什么，与发起它的是
// 哪一处无关。表外的工种不限流——前台的阅读取页不是磁盘作业，不经本包。
func concurrencyLimit(kind storageio.WorkKind, policy config.StorageIOPolicy) int {
	switch kind {
	case storageio.WorkKindMetadataScan: // 打开归档读元数据
		return policy.ArchiveOpenConcurrency
	case storageio.WorkKindIdentityHash: // 流式读整个文件，不开归档
		return policy.HashConcurrency
	case storageio.WorkKindCoverBuild: // 打开归档读一页
		return minPositive(policy.ArchiveOpenConcurrency, policy.CoverConcurrency)
	case storageio.WorkKindCacheWrite: // 写缓存文件，不开归档
		return policy.CoverConcurrency
	default:
		return 0
	}
}

// minPositive 取诸项里最小的正值；全非正时返回 0，即不限流。
func minPositive(values ...int) int {
	limit := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if limit == 0 || value < limit {
			limit = value
		}
	}
	return limit
}

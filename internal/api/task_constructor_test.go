package api

import (
	"context"
	"manga-manager/internal/application"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/task"
	"time"
)

func newTaskEngine(cfg taskEngineConfig) *taskEngine {
	// 领域引擎自己也拦这一道，但它收到的是本层那个转发闭包——非 nil，于是拦不住。
	// 不在这里补一句，装配期漏掉后台能力就要等到第一个任务体启动时才炸。
	if cfg.RunBackground == nil {
		panic("api: taskEngineConfig.RunBackground 不得为 nil")
	}
	e := &taskEngine{
		runStore:       cfg.Store,
		runBackground:  cfg.RunBackground,
		now:            cfg.Now,
		slots:          cfg.Slots,
		backoff:        cfg.Backoff,
		sampleInterval: cfg.SampleInterval,
		resumeEnabled:  cfg.ResumeEnabled,
		identities:     make(map[int64]TaskIdentity),
	}
	e.engine = task.New(task.Config{
		Store:              cfg.Store,
		Publish:            e.publisher(cfg.Publish),
		PublishLive:        e.livePublisher(cfg.Publish),
		RunBackground:      func(fn func()) { e.runBackground(fn) },
		DiskWork:           cfg.DiskWork,
		DecorateRunContext: decorateRunContext,
		Now:                e.clock,
		Slots:              e.slotLimit,
		Backoff:            e.backoffPolicy,
		SampleInterval:     e.samplingInterval,
		Resume:             e.buildResumePolicy,
		ControlCodes: task.ControlCodes{
			Paused:      "task.msg.control.paused",
			Resumed:     "task.msg.control.resumed",
			Cancelling:  "task.msg.control.cancelling",
			Panicked:    taskPanicMessageCode,
			Interrupted: taskInterruptedMessageCode,
		},
	})
	e.runs = application.NewRuns(e.engine, func(ctx context.Context, id task.Identity) error {
		owner, err := e.runStore.EnsureTask(ctx, id)
		if err != nil {
			return err
		}
		identity := TaskIdentity{taskType: string(id.Type), scope: string(id.Scope), variant: TaskVariant(id.Variant)}
		if id.Scope != task.ScopeSystem {
			scopeID := id.ScopeID
			identity.scopeID = &scopeID
		}
		e.rememberIdentity(owner.ID, identity)
		return nil
	})
	return e
}

// taskEngineConfig 是任务引擎的全部外部依赖，一次性在装配期交齐。
//
// 收成结构体而不是位置参数：这几项里有三个都是函数，接反了不会有编译错误。
type taskEngineConfig struct {
	// Store 是任务与运行的落盘端口，不得为 nil：准入判据在它那里。
	Store task.Store
	// Publish 是推送通道本身：一帧已经序列化好的事件（运行快照与**实况汇总**都走它）。
	// 为 nil 时整条通道不接。
	Publish func(string)
	// RunBackground 开一个受停机管辖的 goroutine，不得为 nil。
	RunBackground func(func())
	// DiskWork 是交给**运行句柄**的**磁盘作业**入口，留 nil 的后果见 runhandle.New。
	DiskWork *diskwork.Runner
	// Now 让测试注入可控时钟；为 nil 时走 time.Now。
	Now func() time.Time
	// Slots 读**运行槽位**上限。它是函数而不是数：上限在设置里可改，改了要对**新的放行**生效，
	// 而不打断已经在跑的。为 nil 时领域引擎取它的默认值（task.DefaultSlots）。
	Slots func() int
	// Backoff 读**退避**的三个阈值，同样是函数而不是值，理由同 Slots。
	// 为 nil 时领域引擎取它的默认值（task.DefaultBackoff）。
	Backoff func() task.BackoffPolicy
	// SampleInterval 读**采样**的取点间隔，同样是函数而不是值，理由同 Slots。
	// 为 nil 时领域引擎取它的默认值（task.DefaultSampleInterval）。
	SampleInterval func() time.Duration
	// ResumeEnabled 读**可续跑**的全局开关（默认开），为 nil 即开着。
	// 它只管开关：哪些类型可续跑由重启函数注册表那一列说了算。
	ResumeEnabled func() bool
}

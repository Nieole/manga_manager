// **退避**与禁用这一半：三个阈值、退避曲线、**停发**的判定、两条复位规则，与引擎上认它们的
// 那几个入口。它们挂在**任务**上（规格关键决定 6），只约束自动**发起**——一条已经在跑的运行
// 不会因为退避被打断。

package task

import (
	"context"
	"log/slog"
	"time"
)

// backoffBaseInterval 是**退避**曲线的**基准**：连败 0 次时自动发起的节拍。曲线在它之上翻倍，
// 因此第一次连败要等的是它乘以倍率（默认 2h），不是它本身。
//
// 它不是三个可配数之一。三个数（倍率、封顶、停发阈值）决定曲线的形状，基准则取自动发起本身的
// 自然节拍——资料库扫描间隔的默认值 config.DefaultScanInterval 就是这个量级。
const backoffBaseInterval = time.Hour

// backoffMaxSteps 是退避曲线最多翻几次倍。它挡的不是策略而是**算术**：倍率被配成 1 时曲线
// 永远追不上封顶，循环次数就等于连败次数——而那是一个外部输入。
const backoffMaxSteps = 63

// BackoffPolicy 是**退避**的三个阈值，全部可在设置里改。
//
// 三者共同决定「连败之后还自动发起几次、各隔多久」：延迟从 backoffBaseInterval 起按 Factor
// 逐次翻倍、封顶在 MaxDelay，连败到 StopAfter 次就**停发**——此后一次都不自动发起，等人来修。
type BackoffPolicy struct {
	// Factor 是倍率：每多连败一次，自动发起的间隔乘以它。
	Factor int
	// MaxDelay 是封顶：间隔再怎么翻也不超过它。
	MaxDelay time.Duration
	// StopAfter 是停发阈值：连败到这个次数就不再自动发起。
	StopAfter int
}

// DefaultBackoff 是三个阈值的默认组合：×2、封顶 24h、连败 6 次停发。
//
// 它是「装配方什么都没说」时的兜底。同一组数字在配置那边另有一份默认（配置位于本包的依赖下游，
// 引用不了这里），两份必须相等，`TestConfigDefaultBackoffMatchesTheEngineDefault` 守着这条。
//
// 默认这一档的曲线是 2h、4h、8h、16h、24h（封顶咬住），第 6 次连败停发——一块被拔掉的移动硬盘
// 因此在两天多之内从「每小时白转一遍」收敛到不再转，而不是一年。
func DefaultBackoff() BackoffPolicy {
	return BackoffPolicy{Factor: 2, MaxDelay: 24 * time.Hour, StopAfter: 6}
}

// Delay 交出连败 streak 次之后，下一次自动发起要等多久；streak 非正即不等。
//
// 乘法**边乘边判**而不是先算出 Factor^streak 再取小：指数在 int64 上会溢出，绕回来的可能是个
// 很小的正数（退避当场失效），也可能是负数（到期时刻落在过去，同样失效）。判在乘之前，
// 每一步的乘积因此恒小于封顶，溢出不可能发生。
func (p BackoffPolicy) Delay(streak int) time.Duration {
	if streak <= 0 {
		return 0
	}
	policy := p.orDefault()
	if streak > backoffMaxSteps {
		streak = backoffMaxSteps
	}
	factor := time.Duration(policy.Factor)
	delay := backoffBaseInterval
	for range streak {
		if delay >= policy.MaxDelay/factor {
			return policy.MaxDelay
		}
		delay *= factor
	}
	if delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

// StallReason 是一个任务**停发**的原因，也是界面上那句「为什么」的取值来源。
// 它是一个**封闭枚举**：三条各自对应用户要做的一件不同的事，混成一个布尔值就只剩一个红点。
type StallReason string

const (
	// StallNone 是「没有停发」，自动发起照常。
	StallNone StallReason = ""
	// StallDisabled 是人工禁用：用户自己关掉了这个任务的自动发起。
	StallDisabled StallReason = "disabled"
	// StallFailLimit 是连败到了停发阈值：修好之前不会再自动发起。
	StallFailLimit StallReason = "fail_limit"
	// StallBackoff 是**退避**还没到期：会自动发起，只是还要再等一会儿。
	StallBackoff StallReason = "backoff"
)

// Stall 判这个任务此刻为什么不自动发起，StallNone 表示不挡。**它只答自动发起**——
// 手动发起从不经过这里。
//
// 三条的次序就是优先级：人工禁用盖过另外两条（用户自己按下的开关，界面写「已禁用」比写
// 「退避中」更接近他要做的事），停发盖过退避（连败到阈值之后，那个到期时刻已经不会再放行了）。
func (p BackoffPolicy) Stall(attrs TaskAttributes, now time.Time) StallReason {
	policy := p.orDefault()
	switch {
	case attrs.Disabled:
		return StallDisabled
	case attrs.FailStreak >= policy.StopAfter:
		return StallFailLimit
	case attrs.BackoffUntil != nil && now.Before(*attrs.BackoffUntil):
		return StallBackoff
	default:
		return StallNone
	}
}

// orDefault 把不合法的阈值逐项换成默认值。三个数一路来自手写的配置文件，而算错方向的代价
// 不对称：倍率取 0 会让曲线永远停在起点，封顶取 0 会让每次退避都是 0（等于没有退避），
// 停发阈值取 0 则会让**每一个**任务从第一天起就停发——一次都还没失败过也算数。
func (p BackoffPolicy) orDefault() BackoffPolicy {
	fallback := DefaultBackoff()
	if p.Factor < 1 {
		p.Factor = fallback.Factor
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = fallback.MaxDelay
	}
	if p.StopAfter < 1 {
		p.StopAfter = fallback.StopAfter
	}
	return p
}

// resetBackoff 把连败次数与**退避**清零，返回是否真的改动了什么。
//
// **人工禁用那条开关不动**：它是另一条开关，手动发起一次不等于用户想把自动发起也打开。
func (a *TaskAttributes) resetBackoff() bool {
	if a.FailStreak == 0 && a.BackoffUntil == nil {
		return false
	}
	a.FailStreak = 0
	a.BackoffUntil = nil
	return true
}

// recordSuccess 记下这一次成功：上次成功的时刻前移，连败与退避清零。
func (a *TaskAttributes) recordSuccess(now time.Time) {
	succeeded := now
	a.LastSuccessAt = &succeeded
	a.resetBackoff()
}

// recordFailure 记下这一次失败：连败加一，**退避**推到下一档。
func (a *TaskAttributes) recordFailure(policy BackoffPolicy, now time.Time) {
	a.FailStreak++
	until := now.Add(policy.Delay(a.FailStreak))
	a.BackoffUntil = &until
}

// gateLaunchLocked 是**退避与禁用**在发起这一侧的唯一落点：自动发起在这里被**停发**挡下，
// 手动发起在这里把连败与退避清零，其余**发起方**原样放行。调用方持锁。
//
// **持锁是必要条件而不是顺手**：这一行属性另有一个写入方（收尾那一侧的 recordOutcomeLocked），
// 两处都是「读回来、改几个字段、整行写回去」。不在同一把锁下，一次复位与一次失败计数会互相覆盖。
//
// 复位写在手动发起的这一刻而不是等它跑完：用户按下重试的意思就是「我修好了，从头再来」，
// 让他先等一次成功才解除退避，等于修好之后还要再空等一轮。
//
// 复位落盘失败只告警、不挡下这次发起：那次发起本身没有任何问题，为一次写失败拒绝它，
// 用户失去的是他刚按下的那件事，换来的只是一个迟早会被下一次成功覆盖的字段。
func (e *Engine) gateLaunchLocked(ctx context.Context, owner *Task, trigger Trigger) StallReason {
	if trigger.IsAutomatic() {
		return e.Backoff().Stall(owner.TaskAttributes, e.clock())
	}
	if trigger != TriggerManual || !owner.resetBackoff() {
		return StallNone
	}
	if err := e.store.SaveTaskAttributes(ctx, owner.ID, owner.TaskAttributes); err != nil {
		slog.Warn("Failed to reset task backoff", "task_id", owner.ID, "error", err)
	}
	return StallNone
}

// recordOutcomeLocked 把这次运行的**终态**记到它所属任务的长期属性上：**完成**即复位并记下
// 上次成功的时刻，**失败**即连败加一、把**退避**推到下一档。调用方持锁。
//
// **已取消**与**中断**都不算失败：前者是用户自己按下的，后者是进程没了。把它们算进连败，
// 一次关服、一次点错的取消就能把一个健康的任务推向停发，而它对着的那块盘一点问题都没有。
func (e *Engine) recordOutcomeLocked(ctx context.Context, taskID int64, status RunStatus, now time.Time) {
	if status != StatusCompleted && status != StatusFailed {
		return
	}
	owners, err := e.store.LoadTasks(ctx, []int64{taskID})
	if err != nil {
		slog.Warn("Failed to load task for run outcome", "task_id", taskID, "error", err)
		return
	}
	owner, ok := owners[taskID]
	if !ok {
		return
	}
	if status == StatusCompleted {
		owner.recordSuccess(now)
	} else {
		owner.recordFailure(e.Backoff(), now)
	}
	if err := e.store.SaveTaskAttributes(ctx, taskID, owner.TaskAttributes); err != nil {
		slog.Warn("Failed to persist task backoff", "task_id", taskID, "error", err)
	}
}

// SetTaskDisabled 翻转人工禁用开关，交回改动之后的那个任务；任务不存在返回 ErrTaskNotFound。
//
// 它作用在**任务**上（规格关键决定 15），一条运行都不动——禁用一个正在跑的任务，那次运行
// 照常跑到底。连败次数与**退避**同样不动：复位只有「手动发起一次」与「成功一次」两条路，
// 一个刚被重新启用、却仍然连败 6 次的任务在界面上如实写着它为什么还不自动跑。
func (e *Engine) SetTaskDisabled(ctx context.Context, taskID int64, disabled bool) (Task, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	owners, err := e.store.LoadTasks(ctx, []int64{taskID})
	if err != nil {
		return Task{}, err
	}
	owner, ok := owners[taskID]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	if owner.Disabled == disabled {
		return owner, nil
	}
	owner.Disabled = disabled
	if err := e.store.SaveTaskAttributes(ctx, taskID, owner.TaskAttributes); err != nil {
		return Task{}, err
	}
	return owner, nil
}

// StallOf 交出这个任务此刻的**停发**原因，供界面标红并写明为什么。
//
// 判据与自动发起那道闸门是同一处（BackoffPolicy.Stall），因此界面上那个红点与真正被挡下的
// 那次发起不会各说各话——两处各判一遍的话，用户会看到一个标红却仍在每小时白转的任务。
func (e *Engine) StallOf(attrs TaskAttributes) StallReason {
	return e.Backoff().Stall(attrs, e.clock())
}

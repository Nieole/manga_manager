// 身份这一半：任务行上除身份之外只有长期属性，没有任何一次运行的状态快照。
// 一次执行的那一半在 run.go。

package task

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidIdentity 是身份四要素填不齐或互相矛盾时的哨兵错误。
var ErrInvalidIdentity = errors.New("task identity is incomplete")

// Type 是任务的类型，例如资料库扫描、缩略图重建。取值由发起方声明，本包不认识具体有哪些。
type Type string

// Scope 是任务作用的对象层级，决定任务在界面上挂在哪里。
type Scope string

const (
	ScopeSystem  Scope = "system"
	ScopeLibrary Scope = "library"
	ScopeSeries  Scope = "series"
)

// Variant 是同一类工作在同一个作用域上的第二个身份：跑法不同，因此是两个任务而不是两次运行。
//
// 主变体是空串而不是「没有值」：身份的唯一约束要比较这一列，而 SQL 里 NULL 不等于 NULL——
// 用 NULL 表达「没有变体」会让同一个身份被建出任意多条。
type Variant string

// VariantPrimary 是一个类型在某个作用域上的头一个身份，没有第二种跑法时用它。
const VariantPrimary Variant = ""

// Identity 是一个任务的身份：四要素唯一确定一个任务，也就是落盘那条唯一约束的四列。
//
// 四项都要显式给出，不从**任务键**的字符串里反解——键怎么拼属于 api，而反解会在末段不是
// 作用域 id 时猜错。Validate 是运行期的兜底，声明处的强制属于调用方。
type Identity struct {
	Type    Type
	Scope   Scope
	ScopeID int64
	Variant Variant
}

// Validate 检查四要素齐备且自洽：系统级不带作用域 id，库级与系列级必须带。
func (id Identity) Validate() error {
	if strings.TrimSpace(string(id.Type)) == "" {
		return fmt.Errorf("%w: 缺少任务类型", ErrInvalidIdentity)
	}
	switch id.Scope {
	case ScopeSystem:
		if id.ScopeID != 0 {
			return fmt.Errorf("%w: 系统作用域不该带作用域 id，收到 %d", ErrInvalidIdentity, id.ScopeID)
		}
	case ScopeLibrary, ScopeSeries:
		if id.ScopeID <= 0 {
			return fmt.Errorf("%w: 作用域 %q 必须带正数作用域 id", ErrInvalidIdentity, id.Scope)
		}
	default:
		return fmt.Errorf("%w: 未知的作用域 %q", ErrInvalidIdentity, id.Scope)
	}
	return nil
}

// TaskAttributes 是身份的长期属性：它们回答「这活该不该继续自动发起」，与任何一次运行无关。
//
// 写入规则（**退避**曲线、连败到几次停发、什么算复位）不在本包实现，本包只承载这几个值。
type TaskAttributes struct {
	// Disabled 是人工开关：禁用后定时与监听不再为它发起，手动仍可。
	Disabled bool
	// LastSuccessAt 是最近一次**完成**的时刻，任务清单上「上次成功是什么时候」读的就是它。
	LastSuccessAt *time.Time
	// FailStreak 是连败次数，成功一次或手动发起一次即复位。
	FailStreak int
	// BackoffUntil 是**退避**到期时刻：在此之前不自动发起，已经在跑的运行不受影响。
	BackoffUntil *time.Time
}

// Task 是库里的一行任务：身份加它的长期属性，没有任何一次运行的状态快照。
//
// 行上不放状态快照是刻意的：这一行回答「这活该不该继续自动跑」，「上礼拜三那次跑成什么样」
// 由运行那张表回答（ADR 0004）。
type Task struct {
	ID int64
	Identity
	TaskAttributes
}

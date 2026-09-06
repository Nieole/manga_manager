// **退避**与人工禁用在 api 这一侧的落点：三个阈值从运行时配置读，禁用开关那两个端点
// **按任务 id 寻址**（规格关键决定 15：重试与禁用作用在**任务**上）。
// 曲线、停发判定与两条复位规则都在 `internal/task`，本文件不复述它们。

package api

import (
	"net/http"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/task"
)

// maxBackoffHours 是封顶小时数的上限（100 年），用来挡住溢出而不是表达策略。
//
// 封顶是配置里的一个整数，而 time.Duration 是纳秒计的 int64，装得下的上限约 256 万小时
// （约 292 年）——再大就溢出，绕回来的可能是个很小的正数（退避形同虚设），也可能是负数
// （到期时刻落在过去，同样形同虚设）。配置文件是手写的，因此这一道必须在翻译时就收住。
const maxBackoffHours = 24 * 36500

// backoffPolicyOf 把设置里的三个数翻成**退避**策略。
//
// 每次判定现读一遍配置快照（引擎收的是一个函数，不是一份值）：三个数在设置里可改，改完对
// **下一次自动发起**生效。已经写进任务行的那个到期时刻不会被重新计时——它在上一次失败时
// 就已经算好了。
func backoffPolicyOf(cfg config.Config) task.BackoffPolicy {
	return task.BackoffPolicy{
		Factor:    cfg.Tasks.BackoffFactor,
		MaxDelay:  backoffMaxDelay(cfg.Tasks.BackoffMaxHours),
		StopAfter: cfg.Tasks.BackoffStopAfter,
	}
}

// backoffMaxDelay 把配置里的小时数翻成封顶时长：非正数交出 0（领域侧据此退回默认封顶），
// 大到会溢出的按 maxBackoffHours 收。
func backoffMaxDelay(hours int) time.Duration {
	if hours > maxBackoffHours {
		hours = maxBackoffHours
	}
	if hours < 0 {
		hours = 0
	}
	return time.Duration(hours) * time.Hour
}

// taskBackoffPolicy 读此刻生效的三个阈值，理由同 taskSlots。
func (c *Controller) taskBackoffPolicy() task.BackoffPolicy {
	return backoffPolicyOf(c.currentConfig())
}

// setTaskAutoLaunch 生成禁用 / 启用两个端点：它们只差一个布尔值与成功文案。
//
// 寻址用**任务 id** 而不是**任务键**：禁用是一条长期属性，挂在身份上（规格关键决定 6），
// 而外部库那两类的键带着会话 id——同一身份的两次运行键并不相同，按键寻址会禁用不到它。
func (c *Controller) setTaskAutoLaunch(disabled bool, okMessage string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID, err := parseID(r, "taskID")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "Invalid task ID")
			return
		}
		if err := c.taskEngine.setTaskDisabled(r.Context(), taskID, disabled); err != nil {
			writeTaskControlError(w, err)
			return
		}
		jsonResponse(w, http.StatusAccepted, map[string]string{"message": okMessage})
	}
}

func (c *Controller) disableTask(w http.ResponseWriter, r *http.Request) {
	c.setTaskAutoLaunch(true, "Automatic launches disabled")(w, r)
}

func (c *Controller) enableTask(w http.ResponseWriter, r *http.Request) {
	c.setTaskAutoLaunch(false, "Automatic launches enabled")(w, r)
}

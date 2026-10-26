package application_test

import (
	"manga-manager/internal/application"
	"manga-manager/internal/config"
	"manga-manager/internal/task"
	"testing"
)

// 配置那份默认与领域那份兜底必须相等：错开一次，默认部署删数据的口径就与默认引擎不是一回事。
// config 位于 task 的依赖下游，引用不了那边的常数，只能在这里对一次。
func TestConfigDefaultRetentionMatchesTheEngineDefault(t *testing.T) {
	want := task.DefaultRetention()
	if config.DefaultRetainRunsPerTask != want.RunsPerTask {
		t.Fatalf("配置默认条数 %d 与领域默认 %d 不一致", config.DefaultRetainRunsPerTask, want.RunsPerTask)
	}
	if got := application.RetentionAge(config.DefaultRetainTerminalRunDays); got != want.TerminalAge {
		t.Fatalf("配置默认终态时长 %v 与领域默认 %v 不一致", got, want.TerminalAge)
	}
	if got := application.RetentionAge(config.DefaultRetainSampleDays); got != want.SampleAge {
		t.Fatalf("配置默认采样时长 %v 与领域默认 %v 不一致", got, want.SampleAge)
	}
}

// 三个阈值从**运行时配置**读，改完设置对下一次清理生效，不必重启进程。
//
// 两头的越界值都必须落在「不裁剪」这一侧：天数乘成 time.Duration 会溢出，而绕回来的那个数
// 可能是一个很小的**正**时长——照它办事，下一次清理会把 24 小时前的终态运行全部删掉。
// 归一化本该把这些值挡在配置那一层，这里守的是它漏过来时的方向。
func TestRunRetentionComesFromTheRuntimeConfig(t *testing.T) {
	cfg := config.Config{}
	cfg.Tasks.RetainRunsPerTask = config.DefaultRetainRunsPerTask
	cfg.Tasks.RetainTerminalRunDays = config.DefaultRetainTerminalRunDays
	cfg.Tasks.RetainSampleDays = config.DefaultRetainSampleDays

	if got := application.RetentionPolicyOf(cfg); got != task.DefaultRetention() {
		t.Fatalf("默认配置下的保留策略为 %+v, want %+v", got, task.DefaultRetention())
	}

	cfg.Tasks.RetainRunsPerTask, cfg.Tasks.RetainTerminalRunDays, cfg.Tasks.RetainSampleDays = 5, 30, 2
	want := task.RetentionPolicy{RunsPerTask: 5, TerminalAge: application.RetentionAge(30), SampleAge: application.RetentionAge(2)}
	if got := application.RetentionPolicyOf(cfg); got != want {
		t.Fatalf("改配置之后的保留策略为 %+v, want %+v", got, want)
	}

	cases := []struct {
		name           string
		runs, terminal int
		samples        int
	}{
		{name: "负数", runs: -1, terminal: -1, samples: -1},
		{name: "负得足够多因而会绕回来", runs: -1 << 40, terminal: -1 << 40, samples: -1 << 40},
	}
	for _, tc := range cases {
		cfg.Tasks.RetainRunsPerTask, cfg.Tasks.RetainTerminalRunDays, cfg.Tasks.RetainSampleDays = tc.runs, tc.terminal, tc.samples
		if got := application.RetentionPolicyOf(cfg); got != (task.RetentionPolicy{}) {
			t.Fatalf("%s的阈值算出了 %+v, want 零值（这一层不裁剪）—— 正的时长会当场删光历史", tc.name, got)
		}
	}

	cfg.Tasks.RetainRunsPerTask, cfg.Tasks.RetainTerminalRunDays, cfg.Tasks.RetainSampleDays = 1, 1<<40, 1<<40
	got := application.RetentionPolicyOf(cfg)
	if got.TerminalAge != application.RetentionAge(application.MaxRetentionDays) || got.SampleAge != application.RetentionAge(application.MaxRetentionDays) {
		t.Fatalf("大到溢出的天数没有被收在上限内：%+v", got)
	}
}

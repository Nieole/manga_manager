/**
 * @vitest-environment jsdom
 *
 * 守两层结构各自答各自的问题：实况区只装仍会变化的运行、任务清单一行一个任务、历次运行展开才出现。
 * 也守界面不说谎——速率与不定进度条只在后端真算得出时出现，还没有值的展示位（发起方、排队中、
 * 停发）一律不显示而不是画个零。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

import { TaskCenter, type RunLive, type RunStatus, type TaskSummary } from './TaskCenter';

// 词条只要能渲染出来即可：本文件断言的是某一格在不在、按钮调了谁，与译文无关。
vi.mock('../../i18n/LocaleProvider', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    locale: 'zh-CN',
    formatDateTime: (value: string) => value,
    formatRelativeTime: (value: string) => value,
  }),
}));

function makeRun(overrides: Partial<RunStatus>): RunStatus {
  return {
    run_id: 1,
    task_id: 1,
    key: 'scan_library_1',
    type: 'scan_library',
    scope: 'library',
    status: 'running',
    message: '',
    current: 600,
    total: 10000,
    percent: 6,
    can_cancel: true,
    can_pause: true,
    can_resume: false,
    retryable: true,
    started_at: '2026-08-31T00:00:00Z',
    updated_at: '2026-08-31T00:10:00Z',
    ...overrides,
  };
}

function makeSummary(overrides: Partial<TaskSummary>): TaskSummary {
  return {
    task_id: 1,
    type: 'scan_library',
    scope: 'library',
    disabled: false,
    fail_streak: 0,
    ...overrides,
  };
}

function makeLive(overrides: Partial<RunLive> = {}): RunLive {
  return { active: 0, queued: 0, slots: 0, paused: false, runs: [], ...overrides };
}

function renderCenter(props: Partial<Parameters<typeof TaskCenter>[0]> = {}) {
  return render(
    <TaskCenter
      live={makeLive()}
      tasks={[]}
      loading={false}
      taskActionKey={null}
      onRefresh={vi.fn()}
      onTaskAction={vi.fn()}
      {...props}
    />,
  );
}

// 实况区里放一条运行是多数用例的起手式：它同时给出 runs 与那两个计数。
function renderLiveRuns(runs: RunStatus[]) {
  return renderCenter({ live: makeLive({ runs, active: runs.length }) });
}

afterEach(cleanup);

describe('两层结构', () => {
  it('实况区装在跑的运行，任务清单一行一个任务', () => {
    renderCenter({
      live: makeLive({ active: 1, runs: [makeRun({ run_id: 9 })] }),
      tasks: [makeSummary({ task_id: 1, last_run: makeRun({ run_id: 8, status: 'completed' }) })],
    });

    expect(screen.getByText('logs.taskCenter.liveTitle')).toBeTruthy();
    expect(screen.getByText('logs.taskCenter.taskListTitle')).toBeTruthy();
    // 实况区那条还在跑，清单那行写的是上次结果：两处状态各说各的，不是同一份数据。
    expect(screen.getByText('logs.taskStatus.running')).toBeTruthy();
    expect(screen.getByText('logs.taskStatus.completed')).toBeTruthy();
  });

  it('只有实况没有清单时，清单说自己是空的而实况照画', () => {
    renderLiveRuns([makeRun({})]);
    expect(screen.getByText('logs.taskStatus.running')).toBeTruthy();
    expect(screen.getByText('settings.maintenance.noTasks')).toBeTruthy();
    expect(screen.queryByText('logs.taskCenter.noLiveRuns')).toBeNull();
  });

  it('只有清单没有实况时，实况区说现在没在跑而清单照画', () => {
    renderCenter({ tasks: [makeSummary({ last_run: makeRun({ status: 'completed' }) })] });
    expect(screen.getByText('logs.taskCenter.noLiveRuns')).toBeTruthy();
    expect(screen.getByText('logs.taskStatus.completed')).toBeTruthy();
    expect(screen.queryByText('settings.maintenance.noTasks')).toBeNull();
  });

  it('一次都没跑过的任务写「未跑过」，而不是借一个状态来充数', () => {
    renderCenter({ tasks: [makeSummary({})] });
    expect(screen.getByText('logs.taskCenter.neverRun')).toBeTruthy();
  });
});

describe('任务清单的折叠与展开', () => {
  it('收起时不列历次运行，展开调 onToggleTask', () => {
    const onToggleTask = vi.fn();
    renderCenter({
      tasks: [makeSummary({ task_id: 7, last_run: makeRun({ status: 'completed' }) })],
      onToggleTask,
    });

    expect(screen.queryByText('logs.taskCenter.runHistory')).toBeNull();
    fireEvent.click(screen.getByText('logs.taskStatus.completed'));
    expect(onToggleTask).toHaveBeenCalledWith(7);
  });

  it('展开之后列出这个任务的历次运行', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 7, last_run: makeRun({ run_id: 2, status: 'completed' }) })],
      expandedTaskId: 7,
      taskRuns: [
        makeRun({ run_id: 2, status: 'completed', message_code: 'run.two' }),
        makeRun({ run_id: 1, status: 'failed', message_code: 'run.one' }),
      ],
      onToggleTask: vi.fn(),
    });

    expect(screen.getByText('logs.taskCenter.runHistory')).toBeTruthy();
    expect(screen.getByText('run.two')).toBeTruthy();
    expect(screen.getByText('run.one')).toBeTruthy();
  });

  it('历史为空时明说没有留下记录，而不是画一片空白', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 7 })],
      expandedTaskId: 7,
      taskRuns: [],
      onToggleTask: vi.fn(),
    });
    expect(screen.getByText('logs.taskCenter.noRunHistory')).toBeTruthy();
  });

  it('还在取的时候写加载中，不先说一句「没有记录」', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 7 })],
      expandedTaskId: 7,
      taskRunsLoading: true,
      onToggleTask: vi.fn(),
    });
    expect(screen.getByText('common.loading')).toBeTruthy();
    expect(screen.queryByText('logs.taskCenter.noRunHistory')).toBeNull();
  });
});

describe('还没有值的那几个展示位', () => {
  it('槽位没有上限可报时只写占用数，不画一个凭空的分母', () => {
    renderCenter({ live: makeLive({ active: 2, slots: 0 }) });
    expect(screen.getByText('logs.taskCenter.activeRuns')).toBeTruthy();
    expect(screen.queryByText('logs.taskCenter.slots')).toBeNull();
  });

  it('上限一旦真的有值就画成几分之几', () => {
    renderCenter({ live: makeLive({ active: 2, slots: 2 }) });
    expect(screen.getByText('logs.taskCenter.slots')).toBeTruthy();
    expect(screen.queryByText('logs.taskCenter.activeRuns')).toBeNull();
  });

  it('一条排队都没有时不画排队那一格', () => {
    renderCenter({ live: makeLive({ active: 1, queued: 0, runs: [makeRun({})] }) });
    expect(screen.queryByText('logs.taskCenter.queued')).toBeNull();
  });

  it('有排队的运行时那一格出现，运行本身也在实况区里', () => {
    renderCenter({
      live: makeLive({ active: 1, queued: 1, runs: [makeRun({}), makeRun({ run_id: 2, status: 'queued', can_pause: false })] }),
    });
    expect(screen.getByText('logs.taskCenter.queued')).toBeTruthy();
    expect(screen.getByText('logs.taskStatus.queued')).toBeTruthy();
  });

  it('连败为零、没被停发时那两个徽章都不出现', () => {
    renderCenter({ tasks: [makeSummary({ last_run: makeRun({ status: 'completed' }) })] });
    expect(screen.queryByText('logs.taskCenter.failStreak')).toBeNull();
    expect(screen.queryByText('logs.taskCenter.stalled')).toBeNull();
  });

  it('连败与停发一旦有值就标出来', () => {
    renderCenter({
      tasks: [makeSummary({ fail_streak: 6, disabled: true, last_run: makeRun({ status: 'failed' }) })],
    });
    expect(screen.getByText('logs.taskCenter.failStreak')).toBeTruthy();
    expect(screen.getByText('logs.taskCenter.stalled')).toBeTruthy();
  });

  it('后端没发发起方就整格不显示', () => {
    renderLiveRuns([makeRun({ trigger: undefined })]);
    expect(screen.queryByText(/logs\.task\.trigger/)).toBeNull();
  });

  it('发起方发了就标出这活是谁叫来的', () => {
    renderLiveRuns([makeRun({ trigger: 'scheduled' })]);
    expect(screen.getByText('logs.task.trigger.scheduled')).toBeTruthy();
  });
});

describe('任务中心的处理速率', () => {
  it('后端发了速率就显示出来', () => {
    renderLiveRuns([makeRun({ rate_per_minute: 60 })]);
    expect(screen.getByText('60/min')).toBeTruthy();
  });

  // 一帧都没报过的运行后端算不出速率。夹具用它而不用某个状态：这一格的有无只跟着后端的
  // 那个字段走，前端不得自己按状态判一遍——判据一旦与后端不同步，这一格就会多出或少掉一个数。
  const neverReported = { run_id: 2, key: 'scan_library_2', current: 0, percent: 0, rate_per_minute: undefined };

  it('后端没发速率就整格不显示，也不回落成 0/min', () => {
    renderLiveRuns([makeRun(neverReported)]);
    expect(screen.queryByText('0/min')).toBeNull();
    expect(screen.queryAllByText(/\/min$/)).toHaveLength(0);
  });

  it('中断运行发了速率也照常显示', () => {
    renderCenter({
      tasks: [makeSummary({ last_run: makeRun({ status: 'interrupted', rate_per_minute: 60 }) })],
      expandedTaskId: 1,
      taskRuns: [makeRun({ status: 'interrupted', rate_per_minute: 60 })],
      onToggleTask: vi.fn(),
    });
    expect(screen.getByText('60/min')).toBeTruthy();
  });

  it('同一屏里一个有速率一个没有，只显示有的那个', () => {
    renderLiveRuns([makeRun({ rate_per_minute: 60 }), makeRun(neverReported)]);
    expect(screen.queryAllByText(/\/min$/)).toHaveLength(1);
    expect(screen.getByText('60/min')).toBeTruthy();
  });
});

describe('总数未知的运行的不定进度条', () => {
  // 总数未知时进度条给不出任何数字，它唯一说的话就是「还在动」。
  const indeterminate = () => document.querySelectorAll('.animate-pulse');

  it('活动态才画那条来回跑的进度条', () => {
    renderLiveRuns([makeRun({ status: 'running', total: 0, percent: undefined })]);
    expect(indeterminate()).toHaveLength(1);
  });

  it('收尾之后不再跑动画', () => {
    for (const status of ['completed', 'cancelled', 'failed', 'interrupted']) {
      cleanup();
      renderCenter({
        tasks: [makeSummary({ task_id: 1 })],
        expandedTaskId: 1,
        taskRuns: [makeRun({ status, total: 0, percent: undefined, rate_per_minute: undefined })],
        onToggleTask: vi.fn(),
      });
      expect(indeterminate(), `${status} 的运行还在跑不定进度条动画`).toHaveLength(0);
    }
  });
});

describe('最后一次有动静的时刻', () => {
  // 重启把中断运行的 updated_at 盖成了重启时刻；照它显示的话，八小时前就没动静的那条写着「刚刚」。
  const timestamps = { updated_at: '重启时刻', finished_at: '最后一次上报' };

  it('终态读收尾时刻，不读那一行最后被写的时刻', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 1 })],
      expandedTaskId: 1,
      taskRuns: [makeRun({ status: 'interrupted', ...timestamps })],
      onToggleTask: vi.fn(),
    });
    expect(screen.getByText(/最后一次上报/)).toBeTruthy();
    expect(screen.queryByText(/重启时刻/)).toBeNull();
  });

  it('还在跑的读最后一次上报的时刻——它没有收尾时刻', () => {
    renderLiveRuns([makeRun({ status: 'running', updated_at: '刚刚上报' })]);
    expect(screen.getByText(/刚刚上报/)).toBeTruthy();
  });
});

describe('顶部的全部暂停 / 全部恢复', () => {
  it('没给这对回调时不画那个按钮，也不写那句说明', () => {
    renderCenter({});
    expect(screen.queryByText('settings.maintenance.pauseAllRuns')).toBeNull();
    expect(screen.queryByText('settings.maintenance.pauseAllHint')).toBeNull();
  });

  it('两个按钮并排，各调各的；不可暂停的运行不受影响这句话写在旁边', () => {
    const onPauseAll = vi.fn();
    const onResumeAll = vi.fn();
    renderCenter({ live: makeLive({ paused: true }), onPauseAll, onResumeAll });

    fireEvent.click(screen.getByText('settings.maintenance.pauseAllRuns'));
    expect(onPauseAll).toHaveBeenCalledTimes(1);
    expect(onResumeAll).not.toHaveBeenCalled();

    fireEvent.click(screen.getByText('settings.maintenance.resumeAllRuns'));
    expect(onResumeAll).toHaveBeenCalledTimes(1);
    // 这句话必须写出来，而不是只藏在 title 里。
    expect(screen.getByText('settings.maintenance.pauseAllHint')).toBeTruthy();
  });

  // 一条已暂停、三条还在跑时，翻面的开关只剩「全部恢复」，想让盘安静下来的用户得先恢复再暂停。
  it('已经有运行被暂停时，全部暂停仍然按得下去', () => {
    const onPauseAll = vi.fn();
    renderCenter({ live: makeLive({ paused: true }), onPauseAll, onResumeAll: vi.fn() });

    const pauseAll = screen.getByText('settings.maintenance.pauseAllRuns').closest('button');
    expect(pauseAll?.hasAttribute('disabled')).toBe(false);
    fireEvent.click(screen.getByText('settings.maintenance.pauseAllRuns'));
    expect(onPauseAll).toHaveBeenCalledTimes(1);
  });

  it('一条都没暂停时全部恢复按不下去——按下去什么也不会发生', () => {
    const onResumeAll = vi.fn();
    renderCenter({ live: makeLive({ paused: false }), onPauseAll: vi.fn(), onResumeAll });

    fireEvent.click(screen.getByText('settings.maintenance.resumeAllRuns'));
    expect(onResumeAll).not.toHaveBeenCalled();
  });
});

describe('不可暂停的运行', () => {
  it('运行中却不可暂停的那条明说它不可暂停，而不是画一个暂停键', () => {
    renderLiveRuns([makeRun({ can_pause: false })]);
    expect(screen.getByText('settings.maintenance.taskNotPausable')).toBeTruthy();
    expect(screen.queryByText('settings.maintenance.pauseTask')).toBeNull();
  });

  it('可暂停的那条画暂停键，不写那句说明', () => {
    renderLiveRuns([makeRun({ can_pause: true })]);
    expect(screen.getByText('settings.maintenance.pauseTask')).toBeTruthy();
    expect(screen.queryByText('settings.maintenance.taskNotPausable')).toBeNull();
  });
});

describe('重试作用在任务上', () => {
  it('重试键画在任务行上，一个任务只有一个', () => {
    const onTaskAction = vi.fn();
    renderCenter({
      tasks: [makeSummary({ task_id: 7, last_run: makeRun({ run_id: 3, status: 'failed', retryable: true }) })],
      expandedTaskId: 7,
      taskRuns: [
        makeRun({ run_id: 3, status: 'failed', retryable: true }),
        makeRun({ run_id: 2, status: 'failed', retryable: true }),
      ],
      onToggleTask: vi.fn(),
      onTaskAction,
    });

    const retries = screen.queryAllByText('common.retry');
    expect(retries).toHaveLength(1);
    fireEvent.click(retries[0]);
    expect(onTaskAction).toHaveBeenCalledWith(expect.objectContaining({ run_id: 3 }), 'retry');
  });
});

describe('暂停的来由', () => {
  it('展开后写着这条运行是被谁按下的', () => {
    renderLiveRuns([makeRun({ status: 'paused', can_pause: false, can_resume: true, pause_reason: 'pause_all' })]);
    fireEvent.click(screen.getByText('common.viewDetails'));
    expect(screen.getByText(/logs\.task\.pauseReason\.pause_all/)).toBeTruthy();
  });

  it('没暂停的运行不写这一行', () => {
    renderLiveRuns([makeRun({ status: 'running' })]);
    fireEvent.click(screen.getByText('common.viewDetails'));
    expect(screen.queryByText(/logs\.task\.pauseReason/)).toBeNull();
  });
});

describe('筛选语义', () => {
  it('筛选条上写明筛的是任务清单，以及状态与关键词判在哪一次运行上', () => {
    renderCenter({
      filters: { status: 'ALL', scope: 'ALL', type: 'ALL', scopeId: '', query: '' },
      onFilterChange: vi.fn(),
    });
    expect(screen.getByText('logs.taskFilterScopeHint')).toBeTruthy();
  });

  it('两个输入框都不逐字符触发重取，只把值交给 onFilterChange', () => {
    const onFilterChange = vi.fn();
    const onRefresh = vi.fn();
    renderCenter({
      filters: { status: 'ALL', scope: 'ALL', type: 'ALL', scopeId: '', query: '' },
      onFilterChange,
      onRefresh,
    });

    fireEvent.change(screen.getByPlaceholderText('logs.taskSearchPlaceholder'), { target: { value: 'scan' } });
    fireEvent.change(screen.getByPlaceholderText('logs.taskScopeIdPlaceholder'), { target: { value: '3' } });
    expect(onFilterChange).toHaveBeenCalledTimes(2);
    expect(onRefresh).not.toHaveBeenCalled();

    fireEvent.keyDown(screen.getByPlaceholderText('logs.taskSearchPlaceholder'), { key: 'Enter' });
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });
});

/**
 * @vitest-environment jsdom
 *
 * 守两层结构各自答各自的问题：实况区只装仍会变化的运行、任务清单一行一个任务、历次运行展开才出现。
 * 也守界面不说谎——速率与不定进度条只在后端真算得出时出现，还没有值的展示位（发起方、排队中、
 * 停发）一律不显示而不是画个零。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

import { TaskCenter, type RunEventsResponse, type RunLive, type RunSample, type RunSamplesResponse, type RunStatus, type TaskSummary } from './TaskCenter';
import { runCardId } from '../../utils/runCard';

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
  return { active: 0, queued: 0, slots: 0, paused: false, paused_all: false, runs: [], ...overrides };
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
      history: {
        taskId: 7,
        runs: [
          makeRun({ run_id: 2, status: 'completed', message_code: 'run.two' }),
          makeRun({ run_id: 1, status: 'failed', message_code: 'run.one' }),
        ],
      },
      onToggleTask: vi.fn(),
    });

    expect(screen.getByText('logs.taskCenter.runHistory')).toBeTruthy();
    expect(screen.getByText('run.two')).toBeTruthy();
    expect(screen.getByText('run.one')).toBeTruthy();
  });

  it('历史为空时明说没有留下记录，而不是画一片空白', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 7 })],
      history: { taskId: 7, runs: [] },
      onToggleTask: vi.fn(),
    });
    expect(screen.getByText('logs.taskCenter.noRunHistory')).toBeTruthy();
  });

  it('还在取的时候写加载中，不先说一句「没有记录」', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 7 })],
      history: { taskId: 7, loading: true },
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

  // 没合并过就整格不显示，而不是写一个「已合并 0 次」。
  it('没合并过的运行不画合并计数', () => {
    renderLiveRuns([makeRun({ status: 'queued', coalesced_count: 0 })]);
    expect(screen.queryByText('logs.task.coalesced')).toBeNull();
  });

  it('合并过的排队项写出它代表了几次发起', () => {
    renderLiveRuns([makeRun({ status: 'queued', coalesced_count: 2 })]);
    expect(screen.getByText('logs.task.coalesced')).toBeTruthy();
  });

  it('连败为零、没被停发时那两个徽章都不出现', () => {
    renderCenter({ tasks: [makeSummary({ last_run: makeRun({ status: 'completed' }) })] });
    expect(screen.queryByText('logs.taskCenter.failStreak')).toBeNull();
    expect(screen.queryByText('logs.taskCenter.stalled')).toBeNull();
  });

  it('连败与停发一旦有值就标出来，并写明为什么', () => {
    renderCenter({
      tasks: [makeSummary({
        fail_streak: 6,
        stall_reason: 'fail_limit',
        last_run: makeRun({ status: 'failed', error: 'drive is unplugged' }),
      })],
    });
    expect(screen.getByText('logs.taskCenter.failStreak')).toBeTruthy();
    expect(screen.getByText('logs.taskCenter.stalled')).toBeTruthy();
    // 「写明原因」那半句：光标红答不出「我该去修什么」。
    expect(screen.getByText(/logs\.taskCenter\.stallReason\.fail_limit/)).toBeTruthy();
    expect(screen.getByText(/logs\.taskCenter\.lastError/)).toBeTruthy();
  });

  // 退避会自己走完，因此它不标红——每失败一次就红一次只是噪音，而红色要留给「不会自己恢复」。
  it('退避中的任务不标停发，只写还要等到什么时候', () => {
    renderCenter({
      tasks: [makeSummary({ fail_streak: 1, stall_reason: 'backoff', backoff_until: '2026-09-01T02:00:00Z', last_run: makeRun({ status: 'failed' }) })],
    });
    expect(screen.queryByText('logs.taskCenter.stalled')).toBeNull();
    expect(screen.getByText('logs.taskCenter.backingOff')).toBeTruthy();
    expect(screen.getByText(/logs\.taskCenter\.stallReason\.backoff/)).toBeTruthy();
  });

  // 停发的判据只有后端一处：前端拿 disabled / backoff_until 自己再推一遍的话，
  // 改完设置里那三个阈值，界面上的红点与真正被挡下的那次发起会各说各话。
  it('后端没判停发时，光有 disabled 也不标红', () => {
    renderCenter({ tasks: [makeSummary({ disabled: true, last_run: makeRun({ status: 'completed' }) })] });
    expect(screen.queryByText('logs.taskCenter.stalled')).toBeNull();
  });

  // 标红对应的是票据里那一条「连败 ≥6 次停发并标红」。用户自己关掉的自动发起不是故障，
  // 画成红色只会让红色贬值成「这一行有点什么」。
  it('人工禁用画的是中性徽章而不是停发标红', () => {
    renderCenter({
      tasks: [makeSummary({ disabled: true, stall_reason: 'disabled', last_run: makeRun({ status: 'completed' }) })],
    });
    expect(screen.queryByText('logs.taskCenter.stalled')).toBeNull();
    expect(screen.getByText('logs.taskCenter.autoDisabled')).toBeTruthy();
    expect(screen.getByText(/logs\.taskCenter\.stallReason\.disabled/)).toBeTruthy();
  });

  it('禁用开关按任务交出去，而不是按运行', () => {
    const onToggleTaskAuto = vi.fn();
    const task = makeSummary({ last_run: makeRun({ status: 'completed' }) });
    renderCenter({ tasks: [task], onToggleTaskAuto });
    fireEvent.click(screen.getByText('logs.taskCenter.disableAuto'));
    expect(onToggleTaskAuto).toHaveBeenCalledWith(task);
  });

  it('已禁用的任务上那个按钮写的是恢复', () => {
    renderCenter({
      tasks: [makeSummary({ disabled: true, stall_reason: 'disabled', last_run: makeRun({ status: 'completed' }) })],
      onToggleTaskAuto: vi.fn(),
    });
    expect(screen.getByText('logs.taskCenter.enableAuto')).toBeTruthy();
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
      history: { taskId: 1, runs: [makeRun({ status: 'interrupted', rate_per_minute: 60 })] },
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
        history: { taskId: 1, runs: [makeRun({ status, total: 0, percent: undefined, rate_per_minute: undefined })] },
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
      history: { taskId: 1, runs: [makeRun({ status: 'interrupted', ...timestamps })] },
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

  // 被暂停的那几条被取消或跑完之后 paused 回到 false，而全部暂停的闸门仍关着拦住队列。
  // 只看 paused 的话，这个按钮会灰在唯一能重新放开队列的位置上。
  it('一条都没暂停但闸门还关着时，全部恢复仍然按得下去', () => {
    const onResumeAll = vi.fn();
    renderCenter({ live: makeLive({ paused: false, paused_all: true, queued: 1 }), onPauseAll: vi.fn(), onResumeAll });

    fireEvent.click(screen.getByText('settings.maintenance.resumeAllRuns'));
    expect(onResumeAll).toHaveBeenCalledTimes(1);
  });

  it('闸门关着时实况区说出来，开着时那一格不出现', () => {
    renderCenter({ live: makeLive({ paused_all: true }), onPauseAll: vi.fn(), onResumeAll: vi.fn() });
    expect(screen.getByText('logs.taskCenter.pausedAll')).toBeTruthy();
  });

  it('没人按过全部暂停时不画那一格', () => {
    renderCenter({ live: makeLive({ paused_all: false }), onPauseAll: vi.fn(), onResumeAll: vi.fn() });
    expect(screen.queryByText('logs.taskCenter.pausedAll')).toBeNull();
  });
});

describe('排队中的运行', () => {
  // 它从未开跑、也从未占过槽位，按下去当场进已取消——用户不必等它开跑再停它。
  it('排队中的运行也画得出取消键', () => {
    const onTaskAction = vi.fn();
    renderCenter({
      live: makeLive({ queued: 1, runs: [makeRun({ status: 'queued', can_pause: false, can_cancel: true })] }),
      onTaskAction,
    });

    fireEvent.click(screen.getByText('common.cancel'));
    expect(onTaskAction).toHaveBeenCalledTimes(1);
    expect(onTaskAction.mock.calls[0][1]).toBe('cancel');
  });

  it('排队中不画暂停键，也不写「不可暂停」——它还没开跑，没有闸门可按', () => {
    renderCenter({ live: makeLive({ queued: 1, runs: [makeRun({ status: 'queued', can_pause: false })] }) });
    expect(screen.queryByText('settings.maintenance.pauseTask')).toBeNull();
    expect(screen.queryByText('settings.maintenance.taskNotPausable')).toBeNull();
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

describe('日志入口按运行给', () => {
  // 两条运行两个入口，各自打开自己那一次——历次运行里点第二条，拿到的必须是第二条。
  const twoRuns = {
    tasks: [makeSummary({ task_id: 7, last_run: makeRun({ run_id: 3 }) })],
    history: {
      taskId: 7,
      runs: [makeRun({ run_id: 3, key: 'scan_library_1' }), makeRun({ run_id: 2, key: 'scan_library_1' })],
    },
    onToggleTask: vi.fn(),
  };

  it('「查看日志」打开的是这一次运行自己的详情面板', () => {
    const onViewRunDetail = vi.fn();
    renderCenter({ ...twoRuns, onViewRunDetail });

    const entries = screen.getAllByText('logs.task.viewLogs');
    expect(entries).toHaveLength(2);
    fireEvent.click(entries[1]);
    expect(onViewRunDetail).toHaveBeenCalledWith(expect.objectContaining({ run_id: 2 }), runCardId('history', 2));
  });

  it('旁边留着「原始日志」，按的也是这一次运行', () => {
    const onViewRawLogs = vi.fn();
    renderCenter({ ...twoRuns, onViewRawLogs });

    const entries = screen.getAllByText('logs.task.rawLogs');
    expect(entries).toHaveLength(2);
    fireEvent.click(entries[1]);
    expect(onViewRawLogs).toHaveBeenCalledWith(expect.objectContaining({ run_id: 2 }));
  });
});

describe('事件流', () => {
  const runWithEvents = (events: RunEventsResponse) => ({
    live: makeLive({ active: 1, runs: [makeRun({ run_id: 3 })] }),
    detail: { cardId: runCardId('live', 3), events },
  });

  it('阶段时间线一段一行，写着每段跑了多久', () => {
    renderCenter(runWithEvents({
      run_id: 3,
      events: [{ at: '2026-08-31T00:00:00Z', kind: 'phase', phase: 'discovering' }],
      phases: [
        { phase: 'discovering', started_at: '2026-08-31T00:00:00Z', duration_ms: 90_000 },
        { phase: 'reading_metadata', started_at: '2026-08-31T00:01:30Z', duration_ms: 1_800_000, current: true },
      ],
    }));

    expect(screen.getByText('logs.task.phaseTimeline')).toBeTruthy();
    expect(screen.getByText('1m 30s')).toBeTruthy();
    expect(screen.getByText('30m 0s')).toBeTruthy();
    expect(screen.getByText('logs.task.phaseCurrent')).toBeTruthy();
  });

  // 「还有多少条」读后端那一格，不在事件流里自己找：运行还在跑时那条告警根本还没落。
  it('失败明细写出是哪个文件、为什么，并说出还有多少条没列出', () => {
    renderCenter(runWithEvents({
      run_id: 3,
      events: [{ at: '2026-08-31T00:00:01Z', kind: 'item', item: '/lib/vol01.cbz', reason: 'zip: not a valid archive' }],
      phases: [],
      omitted_failures: 37,
    }));

    expect(screen.getByText('/lib/vol01.cbz')).toBeTruthy();
    expect(screen.getByText('zip: not a valid archive')).toBeTruthy();
    expect(screen.getByText('logs.task.failedItemsOmitted')).toBeTruthy();
  });

  it('一条事件都没有时说出来，而不是画一片空白', () => {
    renderCenter(runWithEvents({ run_id: 3, events: [], phases: [] }));
    expect(screen.getByText('logs.task.noEvents')).toBeTruthy();
  });

  it('事件只画在被点开的那一条运行上', () => {
    renderCenter({
      live: makeLive({ active: 2, runs: [makeRun({ run_id: 3 }), makeRun({ run_id: 4 })] }),
      detail: {
        cardId: runCardId('live', 4),
        events: { run_id: 4, events: [{ at: '2026-08-31T00:00:01Z', kind: 'item', item: '/lib/only-mine.cbz' }], phases: [] },
      },
    });

    // 只有一块事件流面板：两张卡片都画一份的话，用户会以为两条运行失败在同一个文件上。
    expect(screen.getAllByText('logs.task.eventStream')).toHaveLength(1);
    expect(screen.getAllByText('/lib/only-mine.cbz').length).toBeGreaterThan(0);
  });

  // 同一条运行会同时出现在实况区与展开着的历次运行里：认运行的话两张卡片下面各画一份，
  // 用户会以为那是两条运行各自的失败。
  it('同一条运行在两区都出现时，事件只画在被点的那张卡片下面', () => {
    renderCenter({
      live: makeLive({ active: 1, runs: [makeRun({ run_id: 3 })] }),
      tasks: [makeSummary({ task_id: 7, last_run: makeRun({ run_id: 3 }) })],
      history: { taskId: 7, runs: [makeRun({ run_id: 3 })] },
      onToggleTask: vi.fn(),
      detail: {
        cardId: runCardId('history', 3),
        events: { run_id: 3, events: [{ at: '2026-08-31T00:00:01Z', kind: 'item', item: '/lib/only-mine.cbz' }], phases: [] },
      },
    });

    expect(screen.getAllByText('logs.task.eventStream')).toHaveLength(1);
  });
});

describe('吞吐曲线', () => {
  // 每 10 秒一个点，第 n 个点比前一个多处理 step 条；step 为 0 就是一段停滞。
  function makeSamples(rates: number[]): RunSample[] {
    const base = Date.parse('2026-08-31T00:00:00Z');
    return rates.map((rate, index) => ({
      at: new Date(base + (index + 1) * 10_000).toISOString(),
      current: 100 * (index + 1),
      throughput_per_minute: rate,
    }));
  }

  const runWithSamples = (samples: RunSamplesResponse) => ({
    live: makeLive({ active: 1, runs: [makeRun({ run_id: 3 })] }),
    detail: { cardId: runCardId('live', 3), samples },
  });

  function polylines() {
    return Array.from(document.querySelectorAll('[data-testid="run-throughput-curve"] polyline'));
  }

  it('有点就画一条折线，而不是一个点一个元素', () => {
    renderCenter(runWithSamples({
      run_id: 3, retention_days: 7, samples: makeSamples([600, 0, 0, 300]),
    }));

    expect(screen.getByText('logs.task.throughput')).toBeTruthy();
    const lines = polylines();
    expect(lines).toHaveLength(1);
    // 四个点连成一条线，DOM 上只有这一个元素——几千个点的长跑运行靠的就是这一条。
    expect(lines[0].getAttribute('points')?.trim().split(/\s+/)).toHaveLength(4);
  });

  // 暂停与停滞都是「没在产出」，我们观测得清清楚楚，因此曲线在那里是**平的**；
  // 断口只留给「这一段一个观测都没有」。两件事画成一个样子，用户就分不出来了。
  it('停滞段照样连着画，只有漏了点的地方才断开', () => {
    const samples = makeSamples([600, 0, 0, 300, 300, 300]);
    // 第三个点之后隔了五分钟才有下一个：中间那段没有任何观测，连过去就是编数据。
    const shift = 300_000 - 10_000;
    for (let i = 3; i < samples.length; i += 1) {
      samples[i] = { ...samples[i], at: new Date(Date.parse(samples[i].at) + shift).toISOString() };
    }
    renderCenter(runWithSamples({
      run_id: 3, retention_days: 7, samples,
    }));

    const lines = polylines();
    expect(lines).toHaveLength(2);
    expect(lines[0].getAttribute('points')?.trim().split(/\s+/)).toHaveLength(3);
    expect(lines[1].getAttribute('points')?.trim().split(/\s+/)).toHaveLength(3);
  });

  // 取点间隔可配：拿「此刻的间隔」去量过去的点，把 60 秒改成 10 秒之后，
  // 每一条老曲线都会被判成处处漏点、碎成一串圆点。判据因此取这条曲线自己的间距。
  it('间隔改过之后，老曲线不会碎成一串点', () => {
    const base = Date.parse('2026-08-31T00:00:00Z');
    // 这条运行当时是每 60 秒一个点，而设置此刻已经改成了 10 秒。
    const samples: RunSample[] = [0, 1, 2, 3].map((index) => ({
      at: new Date(base + index * 60_000).toISOString(),
      current: 100 * (index + 1),
      throughput_per_minute: 300,
    }));
    renderCenter(runWithSamples({ run_id: 3, retention_days: 7, samples }));

    const lines = polylines();
    expect(lines).toHaveLength(1);
    expect(lines[0].getAttribute('points')?.trim().split(/\s+/)).toHaveLength(4);
  });

  it('过了保留期就明说曲线没了，不画一条假的', () => {
    renderCenter(runWithSamples({
      run_id: 3, retention_days: 7, samples: [], expired: true,
    }));

    expect(screen.getByText('logs.task.samplesGone')).toBeTruthy();
    expect(polylines()).toHaveLength(0);
  });

  it('还没攒够第一个点与过了保留期说的不是同一句话', () => {
    renderCenter(runWithSamples({ run_id: 3, retention_days: 7, samples: [] }));

    expect(screen.getByText('logs.task.noSamples')).toBeTruthy();
    expect(screen.queryByText('logs.task.samplesGone')).toBeNull();
  });

  it('曲线不完整与只画了最近一段都要说出来', () => {
    renderCenter(runWithSamples({
      run_id: 3, retention_days: 7, samples: makeSamples([600, 300]),
      expired: true, truncated: true,
    }));

    expect(screen.getByText('logs.task.samplesExpired')).toBeTruthy();
    expect(screen.getByText('logs.task.samplesTruncated')).toBeTruthy();
    expect(polylines()).toHaveLength(1);
  });

  it('曲线只画在被点开的那一张卡片下面', () => {
    renderCenter({
      live: makeLive({ active: 2, runs: [makeRun({ run_id: 3 }), makeRun({ run_id: 4 })] }),
      detail: {
        cardId: runCardId('live', 4),
        samples: { run_id: 4, retention_days: 7, samples: makeSamples([600]) },
      },
    });

    expect(screen.getAllByTestId('run-throughput-curve')).toHaveLength(1);
  });
});

describe('重试作用在任务上', () => {
  it('重试键画在任务行上，一个任务只有一个', () => {
    const onTaskAction = vi.fn();
    renderCenter({
      tasks: [makeSummary({ task_id: 7, last_run: makeRun({ run_id: 3, status: 'failed', retryable: true }) })],
      history: {
        taskId: 7,
        runs: [
          makeRun({ run_id: 3, status: 'failed', retryable: true }),
          makeRun({ run_id: 2, status: 'failed', retryable: true }),
        ],
      },
      onToggleTask: vi.fn(),
      onTaskAction,
    });

    const retries = screen.queryAllByText('common.retry');
    expect(retries).toHaveLength(1);
    fireEvent.click(retries[0]);
    expect(onTaskAction).toHaveBeenCalledWith(expect.objectContaining({ run_id: 3 }), 'retry');
  });

  // 忙碌态的键按**任务 id** 拼，与端点寻址的是同一个东西。按**任务键**拼的话，同一个键此刻
  // 可以有两条仍会变化的运行，而那个键在后端已经不再寻址任何东西——按下去之后按钮不会变灰，
  // 用户会以为没点上，于是连点几下，后台排出一串同样的运行。
  it('重试按下之后按任务 id 认出自己那颗按钮', () => {
    renderCenter({
      tasks: [makeSummary({ task_id: 7, last_run: makeRun({ run_id: 3, task_id: 7, status: 'failed', retryable: true }) })],
      taskActionKey: '7:retry',
    });

    const retry = screen.getByText('common.retry').closest('button');
    expect(retry?.disabled).toBe(true);
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

describe('存储 IO 那一条', () => {
  // 那五个数已从**重启入参**搬进指标（见 taskScanObserver.Metrics）。这一条只读参数的话，
  // 搬完之后它会少五格——而用户没做任何事，只是升了个级。
  it('数搬进指标之后照样显示那五个', () => {
    renderLiveRuns([makeRun({
      params: { storage_profile: 'hdd_external', volume_key: 'e:' },
      metrics: { opened_archives: 5, hashed_files: 3, io_wait_ms: 123, paused_ms: 45, duration_ms: 6789 },
    })]);
    fireEvent.click(screen.getByText('common.viewDetails'));

    expect(screen.getByText(/logs\.task\.io\.opened_archives: 5/)).toBeTruthy();
    expect(screen.getByText(/logs\.task\.io\.hashed_files: 3/)).toBeTruthy();
    expect(screen.getByText(/logs\.task\.io\.io_wait_ms: 123/)).toBeTruthy();
    expect(screen.getByText(/logs\.task\.io\.paused_ms: 45/)).toBeTruthy();
    expect(screen.getByText(/logs\.task\.io\.duration_ms: 6789/)).toBeTruthy();
    // 描述性的那两格仍读入参：它们是发起时就声明下来的，不随报文走。
    expect(screen.getByText(/logs\.task\.io\.storage_profile: hdd_external/)).toBeTruthy();
  });

  // 升级前落下的运行把这五个数留在入参里。取数带参数回落，因此这次搬家没有可见的断层。
  it('升级前落在入参里的老运行照样显示', () => {
    renderLiveRuns([makeRun({
      params: { opened_archives: '5', hashed_files: '3', io_wait_ms: '123', paused_ms: '45', duration_ms: '6789' },
    })]);
    fireEvent.click(screen.getByText('common.viewDetails'));

    expect(screen.getByText(/logs\.task\.io\.opened_archives: 5/)).toBeTruthy();
    expect(screen.getByText(/logs\.task\.io\.duration_ms: 6789/)).toBeTruthy();
  });
});

describe('指标面板认得扫描报的每一个数', () => {
  // 发现数与跳过数从**重启入参**搬进指标之后，若指标面板不认这两个键，它们就一个面板也进不去：
  // 参数面板原先靠 params 全量铺开才显示它们，而那份已经没了。
  it('发现数与跳过数各占一格', () => {
    renderLiveRuns([makeRun({ metrics: { discovered_archives: 12, skipped_archives: 7 } })]);

    expect(screen.getByText(/taskMetric\.discovered_archives/)).toBeTruthy();
    expect(screen.getByText(/taskMetric\.skipped_archives/)).toBeTruthy();
  });
});

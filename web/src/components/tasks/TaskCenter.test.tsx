/**
 * @vitest-environment jsdom
 *
 * 守界面不说谎：速率与不定进度条只在后端真算得出时出现（`0/min` 是另一种谎），
 * 而顶部那对全部暂停 / 全部恢复必须明说不可暂停的运行不受它影响——否则用户按下之后
 * 看到还有运行在跑，会以为暂停失灵了。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';

import { TaskCenter, type RunStatus } from './TaskCenter';

// 词条只要能渲染出来即可：本文件断言的是某一格在不在、按钮调了谁，与译文无关。
vi.mock('../../i18n/LocaleProvider', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    locale: 'zh-CN',
    formatDateTime: (value: string) => value,
    formatRelativeTime: (value: string) => value,
  }),
}));

function makeTask(overrides: Partial<RunStatus>): RunStatus {
  return {
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

function renderTasks(tasks: RunStatus[]) {
  return render(
    <TaskCenter tasks={tasks} loading={false} taskActionKey={null} onRefresh={vi.fn()} onTaskAction={vi.fn()} />,
  );
}

afterEach(cleanup);

describe('任务中心的处理速率', () => {
  it('后端发了速率就显示出来', () => {
    renderTasks([makeTask({ rate_per_minute: 60 })]);
    expect(screen.getByText('60/min')).toBeTruthy();
  });

  it('中断任务没有速率这一项，也不回落成 0/min', () => {
    renderTasks([makeTask({ key: 'scan_library_2', status: 'interrupted', rate_per_minute: undefined })]);
    expect(screen.queryByText('0/min')).toBeNull();
    expect(screen.queryAllByText(/\/min$/)).toHaveLength(0);
  });

  it('同一屏里一个有速率一个没有，只显示有的那个', () => {
    renderTasks([
      makeTask({ rate_per_minute: 60 }),
      makeTask({ key: 'scan_library_2', status: 'interrupted', rate_per_minute: undefined }),
    ]);
    expect(screen.queryAllByText(/\/min$/)).toHaveLength(1);
    expect(screen.getByText('60/min')).toBeTruthy();
  });
});

describe('总数未知的任务的不定进度条', () => {
  // 总数未知时进度条给不出任何数字，它唯一说的话就是「还在动」。
  const indeterminate = () => document.querySelectorAll('.animate-pulse');

  it('活动态才画那条来回跑的进度条', () => {
    renderTasks([makeTask({ status: 'running', total: 0, percent: undefined })]);
    expect(indeterminate()).toHaveLength(1);
  });

  it('收尾之后不再跑动画', () => {
    for (const status of ['completed', 'cancelled', 'failed', 'interrupted']) {
      cleanup();
      renderTasks([makeTask({ status, total: 0, percent: undefined, rate_per_minute: undefined })]);
      expect(indeterminate(), `${status} 的任务还在跑不定进度条动画`).toHaveLength(0);
    }
  });
});

describe('顶部的全部暂停 / 全部恢复', () => {
  function renderCenter(props: Partial<Parameters<typeof TaskCenter>[0]>) {
    return render(
      <TaskCenter
        tasks={[]}
        loading={false}
        taskActionKey={null}
        onRefresh={vi.fn()}
        onTaskAction={vi.fn()}
        {...props}
      />,
    );
  }

  it('没给这对回调时不画那个按钮，也不写那句说明', () => {
    renderCenter({});
    expect(screen.queryByText('settings.maintenance.pauseAllRuns')).toBeNull();
    expect(screen.queryByText('settings.maintenance.pauseAllHint')).toBeNull();
  });

  it('两个按钮并排，各调各的；不可暂停的运行不受影响这句话写在旁边', () => {
    const onPauseAll = vi.fn();
    const onResumeAll = vi.fn();
    renderCenter({ anyRunPaused: true, onPauseAll, onResumeAll });

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
    renderCenter({ anyRunPaused: true, onPauseAll, onResumeAll: vi.fn() });

    const pauseAll = screen.getByText('settings.maintenance.pauseAllRuns').closest('button');
    expect(pauseAll?.hasAttribute('disabled')).toBe(false);
    fireEvent.click(screen.getByText('settings.maintenance.pauseAllRuns'));
    expect(onPauseAll).toHaveBeenCalledTimes(1);
  });

  it('一条都没暂停时全部恢复按不下去——按下去什么也不会发生', () => {
    const onResumeAll = vi.fn();
    renderCenter({ anyRunPaused: false, onPauseAll: vi.fn(), onResumeAll });

    fireEvent.click(screen.getByText('settings.maintenance.resumeAllRuns'));
    expect(onResumeAll).not.toHaveBeenCalled();
  });
});

describe('不可暂停的运行', () => {
  it('运行中却不可暂停的那条明说它不可暂停，而不是画一个暂停键', () => {
    renderTasks([makeTask({ can_pause: false })]);
    expect(screen.getByText('settings.maintenance.taskNotPausable')).toBeTruthy();
    expect(screen.queryByText('settings.maintenance.pauseTask')).toBeNull();
  });

  it('可暂停的那条画暂停键，不写那句说明', () => {
    renderTasks([makeTask({ can_pause: true })]);
    expect(screen.getByText('settings.maintenance.pauseTask')).toBeTruthy();
    expect(screen.queryByText('settings.maintenance.taskNotPausable')).toBeNull();
  });
});

describe('暂停的来由', () => {
  it('展开后写着这条运行是被谁按下的', () => {
    renderTasks([makeTask({ status: 'paused', can_pause: false, can_resume: true, pause_reason: 'pause_all' })]);
    fireEvent.click(screen.getByText('common.viewDetails'));
    expect(screen.getByText(/logs\.task\.pauseReason\.pause_all/)).toBeTruthy();
  });

  it('没暂停的运行不写这一行', () => {
    renderTasks([makeTask({ status: 'running' })]);
    fireEvent.click(screen.getByText('common.viewDetails'));
    expect(screen.queryByText(/logs\.task\.pauseReason/)).toBeNull();
  });
});

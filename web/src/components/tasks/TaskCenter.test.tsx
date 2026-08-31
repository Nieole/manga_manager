/**
 * @vitest-environment jsdom
 *
 * 守卫进度条那行只显示后端真算出来的速率：**中断**任务的速率后端不发（它停在哪一秒没有
 * 任何地方记下过），界面就整个不显示这一项，而不是回落成 `0/min`——那看着像「一分钟一条
 * 都没跑」，是另一种谎。活动态与其余终态照常显示。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';

import { TaskCenter, type TaskStatus } from './TaskCenter';

// 词条只要能渲染出来即可：本文件断言的是速率那一格在不在，与译文无关。
vi.mock('../../i18n/LocaleProvider', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    locale: 'zh-CN',
    formatDateTime: (value: string) => value,
    formatRelativeTime: (value: string) => value,
  }),
}));

function makeTask(overrides: Partial<TaskStatus>): TaskStatus {
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

function renderTasks(tasks: TaskStatus[]) {
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

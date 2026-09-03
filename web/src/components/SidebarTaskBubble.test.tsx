/**
 * @vitest-environment jsdom
 *
 * 守卫任务气泡怎么表述状态：终态条目不转圈、关得掉、状态文案是译文；折叠条只数活动态，
 * 只剩终态时按严重度收口——失败、已取消、中断都不许被画成绿对勾并说「所有任务已完成」。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { messages as enUS } from '../i18n/locales/en-US';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import { SidebarTaskBubble, type TaskBubbleEntry } from './SidebarTaskBubble';

// 复刻 fillTemplate 的占位符替换与 translateInLocale 的缺键行为：查不到就把 key 原样吐出来，
// 裸 key 因此能被断言抓住。中文没有复数形态，这里不必复刻 selectPluralForm。
function fill(template: string, params: Record<string, unknown>) {
  return template.replace(/\{\{\s*([^}]+?)\s*\}\}/g, (_, name: string) => String(params[name.trim()] ?? ''));
}

vi.mock('../i18n/LocaleProvider', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => fill(zhCN[key] ?? key, params ?? {}),
  }),
}));

const TERMINAL_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'];

function makeTask(status: string, key = 'scan_library:1'): TaskBubbleEntry {
  return {
    key,
    type: 'scan_library',
    status,
    message: '扫描资料库',
    current: 0,
    total: 0,
    updatedAt: Date.now(),
  };
}

// 列表折叠在按钮后面：先点开，才看得到气泡条目。
function renderOpened(status: string) {
  const onDismiss = vi.fn();
  const view = render(
    <MemoryRouter>
      <SidebarTaskBubble tasks={[makeTask(status)]} onDismiss={onDismiss} onClearFinished={vi.fn()} />
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByRole('button'));
  return { ...view, onDismiss };
}

afterEach(cleanup);

describe('SidebarTaskBubble 的终态条目', () => {
  it.each(TERMINAL_STATUSES)('%s 的图标不再转圈', (status) => {
    const { container } = renderOpened(status);
    expect(container.querySelector('li .animate-spin')).toBeNull();
  });

  it.each(TERMINAL_STATUSES)('%s 能被手动关掉', (status) => {
    const { onDismiss } = renderOpened(status);
    fireEvent.click(screen.getByLabelText(zhCN['common.close']));
    expect(onDismiss).toHaveBeenCalledWith('scan_library:1');
  });

  it.each(TERMINAL_STATUSES)('%s 的状态文案是译文，不是裸词条 key', (status) => {
    renderOpened(status);
    expect(screen.queryByText(`taskBubble.status.${status}`)).toBeNull();
    expect(screen.getByText(zhCN[`taskBubble.status.${status}`])).toBeTruthy();
  });

  it('取消中仍在转圈，也不给关闭按钮', () => {
    const { container } = renderOpened('cancelling');
    expect(container.querySelector('li .animate-spin')).not.toBeNull();
    expect(screen.queryByLabelText(zhCN['common.close'])).toBeNull();
  });
});

describe('taskBubble.status 的词条覆盖', () => {
  // 后端会推给气泡的全部状态取值：活动态三种加终态四种。
  const ALL_STATUSES = ['running', 'paused', 'cancelling', ...TERMINAL_STATUSES];

  it.each(ALL_STATUSES)('两份语言表都有 %s 的词条', (status) => {
    const key = `taskBubble.status.${status}`;
    expect(zhCN[key], `zh-CN 缺 ${key}`).toBeTruthy();
    expect(enUS[key], `en-US 缺 ${key}`).toBeTruthy();
  });

  it('没有后端到不了的死词条', () => {
    const reachable = new Set(ALL_STATUSES.map((status) => `taskBubble.status.${status}`));
    const declared = Object.keys(zhCN).filter((key) => key.startsWith('taskBubble.status.'));
    expect(declared.filter((key) => !reachable.has(key))).toEqual([]);
  });
});

describe('折叠条的概括', () => {
  // 折叠状态下页面上只有一个按钮，就是气泡本身；它的图标与那行文案就是不展开时的全部信息。
  function renderCollapsed(statuses: string[]) {
    return render(
      <MemoryRouter>
        <SidebarTaskBubble
          tasks={statuses.map((status, index) => makeTask(status, `scan_library:${index}`))}
          onDismiss={vi.fn()}
          onClearFinished={vi.fn()}
        />
      </MemoryRouter>,
    );
  }

  it.each(['failed', 'cancelled', 'interrupted'])('只剩 %s 时不画绿对勾，也不说「所有任务已完成」', (status) => {
    const { container } = renderCollapsed([status]);
    expect(screen.queryByText(zhCN['taskBubble.summary.completed'])).toBeNull();
    expect(container.querySelector('.text-emerald-400')).toBeNull();
    expect(screen.getByText(fill(zhCN[`taskBubble.summary.${status}`], { count: 1 }))).toBeTruthy();
  });

  it('只剩已完成时才画绿对勾并说「所有任务已完成」', () => {
    const { container } = renderCollapsed(['completed', 'completed']);
    expect(screen.getByText(zhCN['taskBubble.summary.completed'])).toBeTruthy();
    expect(container.querySelector('.text-emerald-400')).not.toBeNull();
  });

  it('完成与失败混在一起时按失败收口，不被一句「已完成」盖过去', () => {
    const { container } = renderCollapsed(['completed', 'failed']);
    expect(screen.getByText(fill(zhCN['taskBubble.summary.failed'], { count: 1 }))).toBeTruthy();
    expect(screen.queryByText(zhCN['taskBubble.summary.completed'])).toBeNull();
    expect(container.querySelector('.text-emerald-400')).toBeNull();
  });

  it('终态混合按严重度取一种：失败 > 中断 > 已取消 > 完成', () => {
    renderCollapsed(['completed', 'cancelled', 'interrupted']);
    expect(screen.getByText(fill(zhCN['taskBubble.summary.interrupted'], { count: 1 }))).toBeTruthy();
    cleanup();
    renderCollapsed(['completed', 'cancelled']);
    expect(screen.getByText(fill(zhCN['taskBubble.summary.cancelled'], { count: 1 }))).toBeTruthy();
  });

  it.each(['running', 'paused', 'cancelling'])('活动态 %s 都按「活动任务」计数，不说「进行中」', (status) => {
    renderCollapsed([status]);
    expect(screen.getByText(fill(zhCN['taskBubble.summary.active'], { count: 1 }))).toBeTruthy();
  });

  it('三种活动态一起数，终态不计入这个数', () => {
    renderCollapsed(['running', 'paused', 'cancelling', 'failed']);
    expect(screen.getByText(fill(zhCN['taskBubble.summary.active'], { count: 3 }))).toBeTruthy();
  });

  it('只剩已暂停时折叠条不转圈——没有任何东西在推进', () => {
    const { container } = renderCollapsed(['paused']);
    expect(container.querySelector('.animate-spin')).toBeNull();
  });

  it('还有运行中或取消中时折叠条继续转圈', () => {
    const { container } = renderCollapsed(['paused', 'running']);
    expect(container.querySelector('.animate-spin')).not.toBeNull();
    cleanup();
    const cancelling = renderCollapsed(['cancelling']);
    expect(cancelling.container.querySelector('.animate-spin')).not.toBeNull();
  });
});

describe('已暂停条目', () => {
  it('展开后不转圈，也不给关闭按钮——它还占着运行槽位', () => {
    const { container } = renderOpened('paused');
    expect(container.querySelector('li .animate-spin')).toBeNull();
    expect(screen.queryByLabelText(zhCN['common.close'])).toBeNull();
  });
});

describe('taskBubble.summary 的词条覆盖', () => {
  const SUMMARY_KEYS = ['active', 'completed', 'failed', 'cancelled', 'interrupted'];

  it.each(SUMMARY_KEYS)('两份语言表都有 %s 的词条', (name) => {
    const key = `taskBubble.summary.${name}`;
    expect(zhCN[key], `zh-CN 缺 ${key}`).toBeTruthy();
    expect(enUS[key], `en-US 缺 ${key}`).toBeTruthy();
  });
});

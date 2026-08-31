/**
 * @vitest-environment jsdom
 *
 * 守卫终态气泡在侧边栏里是「已停下且关得掉」的：图标不许再转圈（转圈等于告诉用户还在跑），
 * 必须渲染关闭按钮（否则手动也关不掉），`total == 0` 时那行状态文案必须是译文而不是裸的词条 key。
 * 四种终态各跑一遍，取消中则相反——它属于活动态，仍该转圈且不给关闭按钮。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { messages as enUS } from '../i18n/locales/en-US';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import { SidebarTaskBubble, type TaskBubbleEntry } from './SidebarTaskBubble';

// 复刻 translateInLocale 的缺键行为：查不到就把 key 原样吐出来，裸 key 因此能被断言抓住。
vi.mock('../i18n/LocaleProvider', () => ({
  useI18n: () => ({ t: (key: string) => zhCN[key] ?? key }),
}));

const TERMINAL_STATUSES = ['completed', 'cancelled', 'failed', 'interrupted'];

function makeTask(status: string): TaskBubbleEntry {
  return {
    key: 'scan_library:1',
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

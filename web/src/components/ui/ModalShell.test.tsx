/**
 * @vitest-environment jsdom
 *
 * 业务说明：本文件守卫共享弹窗壳的键盘可达性。全站弹窗都套在 ModalShell 上，
 * 所以这三条性质一旦回归，影响的是每一个弹窗。
 *
 * 此前 ModalShell 有 role="dialog" 与 aria-modal，但没有任何焦点管理：
 *   - 打开时焦点仍留在背景里，读屏软件读的还是背后那一页，弹窗形同不存在；
 *   - aria-modal 只告诉读屏软件「背景不可用」，浏览器的 Tab 顺序并不受它约束，
 *     于是 Tab 会走出弹窗、落到背后视觉上被遮住却仍可聚焦的链接上；
 *   - 关闭后焦点回到 <body>，键盘用户下一次 Tab 要从整页开头重新走一遍。
 */

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ModalShell } from './ModalShell';

// ModalShell 依赖 useI18n 取关闭按钮的 aria-label；这里只需要一个能返回 key 的桩。
vi.mock('../../i18n/LocaleProvider', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

afterEach(cleanup);

function renderModal(onClose = () => {}) {
  return render(
    <div>
      <button type="button">background</button>
      <ModalShell open onClose={onClose} title="标题">
        <button type="button">first</button>
        <button type="button">last</button>
      </ModalShell>
    </div>,
  );
}

describe('ModalShell 的焦点管理', () => {
  it('打开时把焦点移进弹窗', async () => {
    renderModal();
    // 内容是异步渲染的，聚焦排在下一帧。
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));

    const active = document.activeElement as HTMLElement;
    expect(active).not.toBeNull();
    expect(active.closest('[role="dialog"]')).not.toBeNull();
  });

  // 注意：jsdom 不会真的按 Tab 移动焦点，所以「焦点仍在弹窗内」这种断言在没有陷阱时
  // 也恒为真（什么都没发生，焦点自然还在原处），毫无判别力。这里断言的是**环绕**本身：
  // 从最后一个控件按 Tab 必须落到第一个。没有陷阱时焦点纹丝不动，断言必红。
  // 按 DOM 顺序取弹窗内的可聚焦控件。不硬编码「第一个是哪个按钮」：
  // 关闭按钮在头部、内容在其后，改一次布局就会让硬编码的断言变成假绿。
  function focusablesInDialog(): HTMLElement[] {
    const dialog = screen.getByRole('dialog');
    return Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    );
  }

  it('Tab 从最后一个控件环绕回第一个，而不是溜到背景里', async () => {
    renderModal();
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));

    const items = focusablesInDialog();
    items[items.length - 1].focus();
    fireEvent.keyDown(document, { key: 'Tab' });

    expect(document.activeElement).toBe(items[0]);
  });

  it('Shift+Tab 从第一个控件环绕回最后一个', async () => {
    renderModal();
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));

    const items = focusablesInDialog();
    items[0].focus();
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });

    expect(document.activeElement).toBe(items[items.length - 1]);
  });

  it('关闭后把焦点还给打开它的那个元素', async () => {
    const trigger = document.createElement('button');
    trigger.textContent = 'trigger';
    document.body.appendChild(trigger);
    trigger.focus();

    const { rerender } = render(
      <ModalShell open onClose={() => {}} title="标题">
        <button type="button">inside</button>
      </ModalShell>,
    );
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));
    expect(document.activeElement).not.toBe(trigger);

    rerender(
      <ModalShell open={false} onClose={() => {}} title="标题">
        <button type="button">inside</button>
      </ModalShell>,
    );

    expect(document.activeElement).toBe(trigger);
    trigger.remove();
  });

  it('Esc 关闭仍然可用', async () => {
    const onClose = vi.fn();
    renderModal(onClose);
    await new Promise((resolve) => requestAnimationFrame(() => resolve(null)));

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });
});

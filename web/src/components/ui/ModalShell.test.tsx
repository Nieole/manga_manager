/**
 * @vitest-environment jsdom
 *
 * 守共享弹窗壳 ModalShell 的键盘可达性，全站弹窗都套在它上面：打开时必须把焦点移进弹窗
 * （否则读屏软件读的还是背景那一页）；Tab 必须被困在弹窗内环绕（否则会走到背景里视觉被遮住
 * 却仍可聚焦的控件上）；关闭后焦点必须还给触发它的元素（否则下一次 Tab 要从整页开头重来）。
 * 外壳重渲染不得重设焦点；叠了多层时只有栈顶那层吃键盘、滚动锁在栈空时才解。
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

// 搜索弹窗与资料库表单都是这个形态：头部有关闭按钮，正文第一个控件是输入框。
function renderInputModal(onClose = () => {}) {
  return render(
    <ModalShell open onClose={onClose} title="标题">
      <input type="text" aria-label="关键词" />
      <button type="button">提交</button>
    </ModalShell>,
  );
}

const nextFrame = () => new Promise((resolve) => requestAnimationFrame(() => resolve(null)));

describe('ModalShell 在外壳重渲染时的焦点', () => {
  it('打开时焦点落在第一个可输入控件，而不是头部的关闭按钮', async () => {
    renderInputModal();
    await nextFrame();

    expect(document.activeElement).toBe(screen.getByLabelText('关键词'));
  });

  it('外层每次渲染都传一个新的 onClose，焦点也不会被从输入框抢走', async () => {
    const { rerender } = renderInputModal(() => {});
    await nextFrame();

    const input = screen.getByLabelText('关键词') as HTMLInputElement;
    input.focus();
    fireEvent.change(input, { target: { value: 'xy' } });

    // 搜索词是外壳的 state，打字必然让外壳重渲染，内联箭头每次都是新引用。
    rerender(
      <ModalShell open onClose={() => {}} title="标题">
        <input type="text" aria-label="关键词" defaultValue="xy" />
        <button type="button">提交</button>
      </ModalShell>,
    );
    await nextFrame();

    expect(document.activeElement).toBe(screen.getByLabelText('关键词'));
  });
});

describe('ModalShell 的多层叠加', () => {
  function renderTwoLayers(closeOuter: () => void, closeInner: () => void) {
    return render(
      <div>
        <ModalShell open onClose={closeOuter} title="外层">
          <button type="button">outer</button>
        </ModalShell>
        <ModalShell open onClose={closeInner} title="内层">
          <button type="button">inner</button>
        </ModalShell>
      </div>,
    );
  }

  it('一次 Esc 只关掉最上面那层', async () => {
    const closeOuter = vi.fn();
    const closeInner = vi.fn();
    renderTwoLayers(closeOuter, closeInner);
    await nextFrame();

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(closeInner).toHaveBeenCalledTimes(1);
    expect(closeOuter).not.toHaveBeenCalled();
  });

  it('叠加期间经历一次重渲染，全部关闭后页面滚动恢复', async () => {
    document.body.style.overflow = '';
    const { rerender } = renderTwoLayers(
      () => {},
      () => {},
    );
    await nextFrame();
    expect(document.body.style.overflow).toBe('hidden');

    // 外壳重渲染一次：React 先跑完所有 cleanup 再跑所有 setup，两层的还原值会互相污染。
    rerender(
      <div>
        <ModalShell open onClose={() => {}} title="外层">
          <button type="button">outer</button>
        </ModalShell>
        <ModalShell open onClose={() => {}} title="内层">
          <button type="button">inner</button>
        </ModalShell>
      </div>,
    );
    await nextFrame();

    rerender(
      <div>
        <ModalShell open onClose={() => {}} title="外层">
          <button type="button">outer</button>
        </ModalShell>
        <ModalShell open={false} onClose={() => {}} title="内层">
          <button type="button">inner</button>
        </ModalShell>
      </div>,
    );
    rerender(
      <div>
        <ModalShell open={false} onClose={() => {}} title="外层">
          <button type="button">outer</button>
        </ModalShell>
        <ModalShell open={false} onClose={() => {}} title="内层">
          <button type="button">inner</button>
        </ModalShell>
      </div>,
    );

    expect(document.body.style.overflow).toBe('');
  });
});

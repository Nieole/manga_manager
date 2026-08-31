/**
 * @vitest-environment jsdom
 *
 * 守全局快捷键的两条边界：弹窗打开时整体让位（弹窗里的焦点常停在按钮而非输入框上，只判「目标是否
 * 可编辑」挡不住），以及判定只认 KeyboardEvent.key 给出的字符、不掺 shiftKey，从而与键盘布局无关。
 */

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useLayoutShortcuts } from './useLayoutShortcuts';
import { ModalShell } from '../ui/ModalShell';

// ModalShell 依赖 useI18n 取关闭按钮的 aria-label；这里只需要一个能返回 key 的桩。
vi.mock('../../i18n/LocaleProvider', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

afterEach(cleanup);

type HarnessProps = { modalOpen?: boolean } & Partial<Parameters<typeof useLayoutShortcuts>[0]>;

// 应用外壳的最小形态：快捷键 hook、一个输入框与弹窗挂在同一棵树上。未传的回调用空函数兜底。
function Harness({ modalOpen = false, ...handlers }: HarnessProps) {
  useLayoutShortcuts({
    onOpenSearch: () => {},
    onToggleShortcuts: () => {},
    onCloseShortcuts: () => {},
    onToggleSidebar: () => {},
    onNavigate: () => {},
    ...handlers,
  });
  return (
    <>
      <input aria-label="输入框" />
      <ModalShell open={modalOpen} onClose={() => {}} title="弹窗">
        <button type="button">里面</button>
      </ModalShell>
    </>
  );
}

describe('useLayoutShortcuts 与弹窗的关系', () => {
  it('没有弹窗时，/ 打开搜索、g s 跳转设置', () => {
    const onOpenSearch = vi.fn();
    const onNavigate = vi.fn();
    render(<Harness modalOpen={false} onOpenSearch={onOpenSearch} onNavigate={onNavigate} />);

    fireEvent.keyDown(document.body, { key: '/' });
    fireEvent.keyDown(document.body, { key: 'g' });
    fireEvent.keyDown(document.body, { key: 's' });

    expect(onOpenSearch).toHaveBeenCalledTimes(1);
    expect(onNavigate).toHaveBeenCalledWith('/settings');
  });

  it('弹窗打开时，/ 不叠第二个搜索框、g s 不把页面跳走', () => {
    const onOpenSearch = vi.fn();
    const onNavigate = vi.fn();
    render(<Harness modalOpen onOpenSearch={onOpenSearch} onNavigate={onNavigate} />);

    fireEvent.keyDown(document.body, { key: '/' });
    fireEvent.keyDown(document.body, { key: 'g' });
    fireEvent.keyDown(document.body, { key: 's' });

    expect(onOpenSearch).not.toHaveBeenCalled();
    expect(onNavigate).not.toHaveBeenCalled();
  });
});

describe('useLayoutShortcuts 与键盘布局的关系', () => {
  it('/ 要按 Shift 才打得出的布局上（德语 QWERTZ 的 Shift+7），/ 打开搜索而不是快捷键面板', () => {
    const onOpenSearch = vi.fn();
    const onToggleShortcuts = vi.fn();
    render(<Harness onOpenSearch={onOpenSearch} onToggleShortcuts={onToggleShortcuts} />);

    fireEvent.keyDown(document.body, { key: '/', code: 'Digit7', shiftKey: true });

    expect(onOpenSearch).toHaveBeenCalledTimes(1);
    expect(onToggleShortcuts).not.toHaveBeenCalled();
  });

  it('真打出问号时开关快捷键面板，无论该布局拿哪个物理键打出它', () => {
    const onOpenSearch = vi.fn();
    const onToggleShortcuts = vi.fn();
    render(<Harness onOpenSearch={onOpenSearch} onToggleShortcuts={onToggleShortcuts} />);

    // 美式 QWERTY 的 Shift+/ 与德语 QWERTZ 的 Shift+ß，key 都是 ?，差别只在物理键 code 上。
    fireEvent.keyDown(document.body, { key: '?', code: 'Slash', shiftKey: true });
    fireEvent.keyDown(document.body, { key: '?', code: 'Minus', shiftKey: true });

    expect(onToggleShortcuts).toHaveBeenCalledTimes(2);
    expect(onOpenSearch).not.toHaveBeenCalled();
  });
});

describe('useLayoutShortcuts 的让位与前缀边界', () => {
  it('焦点在输入框里时，/ ? [ 与 g s 都不触发快捷键', () => {
    const onOpenSearch = vi.fn();
    const onToggleShortcuts = vi.fn();
    const onToggleSidebar = vi.fn();
    const onNavigate = vi.fn();
    render(
      <Harness
        onOpenSearch={onOpenSearch}
        onToggleShortcuts={onToggleShortcuts}
        onToggleSidebar={onToggleSidebar}
        onNavigate={onNavigate}
      />,
    );
    const input = screen.getByLabelText('输入框');

    fireEvent.keyDown(input, { key: '/' });
    fireEvent.keyDown(input, { key: '/', shiftKey: true });
    fireEvent.keyDown(input, { key: '?', shiftKey: true });
    fireEvent.keyDown(input, { key: '[' });
    fireEvent.keyDown(input, { key: 'g' });
    fireEvent.keyDown(input, { key: 's' });

    expect(onOpenSearch).not.toHaveBeenCalled();
    expect(onToggleShortcuts).not.toHaveBeenCalled();
    expect(onToggleSidebar).not.toHaveBeenCalled();
    expect(onNavigate).not.toHaveBeenCalled();
  });

  it('⌘K 与 Ctrl+K 在输入框里照常开搜索', () => {
    const onOpenSearch = vi.fn();
    render(<Harness onOpenSearch={onOpenSearch} />);
    const input = screen.getByLabelText('输入框');

    fireEvent.keyDown(input, { key: 'k', metaKey: true });
    fireEvent.keyDown(input, { key: 'k', ctrlKey: true });

    expect(onOpenSearch).toHaveBeenCalledTimes(2);
  });

  it('g 前缀超时后作废，隔太久再按 s 不跳转', () => {
    vi.useFakeTimers();
    try {
      const onNavigate = vi.fn();
      render(<Harness onNavigate={onNavigate} />);

      fireEvent.keyDown(document.body, { key: 'g' });
      vi.advanceTimersByTime(1500);
      fireEvent.keyDown(document.body, { key: 's' });

      expect(onNavigate).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});

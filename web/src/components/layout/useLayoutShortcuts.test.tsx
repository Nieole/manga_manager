/**
 * @vitest-environment jsdom
 *
 * 守全局快捷键在弹窗打开时整体让位：弹窗里的焦点常停在按钮而非输入框上，只判「目标是否可编辑」
 * 挡不住：按 / 会叠出第二个搜索框，按 g s 会直接路由跳走，把弹窗留在新页面上。
 */

import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useLayoutShortcuts } from './useLayoutShortcuts';
import { ModalShell } from '../ui/ModalShell';

// ModalShell 依赖 useI18n 取关闭按钮的 aria-label；这里只需要一个能返回 key 的桩。
vi.mock('../../i18n/LocaleProvider', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

afterEach(cleanup);

interface HarnessProps {
  modalOpen: boolean;
  onOpenSearch: () => void;
  onNavigate: (path: string) => void;
}

// 应用外壳的最小形态：快捷键 hook 与弹窗挂在同一棵树上。
function Harness({ modalOpen, onOpenSearch, onNavigate }: HarnessProps) {
  useLayoutShortcuts({
    onOpenSearch,
    onToggleShortcuts: () => {},
    onCloseShortcuts: () => {},
    onToggleSidebar: () => {},
    onNavigate,
  });
  return (
    <ModalShell open={modalOpen} onClose={() => {}} title="弹窗">
      <button type="button">里面</button>
    </ModalShell>
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

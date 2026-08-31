/**
 * @vitest-environment jsdom
 *
 * 守资源库页快捷键在弹窗打开时让位：本页的批量编辑、加入合集、转移确认都是弹窗，弹窗里的焦点
 * 常停在按钮上，只判「目标是否可编辑」挡不住——按 e 会在弹窗背后切换选择模式。
 */

import { cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { useLibraryKeyboard } from './useLibraryKeyboard';
import { ModalShell } from '../../../components/ui/ModalShell';

// ModalShell 依赖 useI18n 取关闭按钮的 aria-label；这里只需要一个能返回 key 的桩。
vi.mock('../../../i18n/LocaleProvider', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}));

afterEach(cleanup);

function Harness({ modalOpen, onToggleSelection }: { modalOpen: boolean; onToggleSelection: () => void }) {
  useLibraryKeyboard({
    enabled: true,
    onFocusSearch: () => {},
    onJumpFirst: () => {},
    onJumpLast: () => {},
    onToggleSelection,
    onEscape: () => {},
  });
  return (
    <ModalShell open={modalOpen} onClose={() => {}} title="弹窗">
      <button type="button">里面</button>
    </ModalShell>
  );
}

describe('useLibraryKeyboard 与弹窗的关系', () => {
  it('没有弹窗时，e 切换批量选择模式', () => {
    const onToggleSelection = vi.fn();
    render(<Harness modalOpen={false} onToggleSelection={onToggleSelection} />);

    fireEvent.keyDown(document.body, { key: 'e' });

    expect(onToggleSelection).toHaveBeenCalledTimes(1);
  });

  it('弹窗打开时，e 不去背后切换批量选择模式', () => {
    const onToggleSelection = vi.fn();
    render(<Harness modalOpen onToggleSelection={onToggleSelection} />);

    fireEvent.keyDown(document.body, { key: 'e' });

    expect(onToggleSelection).not.toHaveBeenCalled();
  });
});

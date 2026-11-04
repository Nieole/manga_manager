// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { SeriesBookCard } from './SeriesBookCard';
import type { Book } from './types';
import { downloadBookFile } from '../../utils/download';
import { uploadBookCover } from '../../utils/cover';

const auth = vi.hoisted(() => ({ isAdmin: true }));
const showToast = vi.hoisted(() => vi.fn());
vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => auth }));
vi.mock('../../i18n/LocaleProvider', () => ({ useI18n: () => ({ t: (key: string) => key }) }));
vi.mock('../../components/ToastProvider', () => ({ useToast: () => ({ showToast }) }));
vi.mock('../../utils/download', () => ({ downloadBookFile: vi.fn() }));
vi.mock('../../utils/cover', () => ({ uploadBookCover: vi.fn() }));

const book: Book = { id: 11, name: 'Book eleven', library_id: 1, volume: '', page_count: 10 };
function rect(left: number, top: number, width: number, height: number): DOMRect {
  return { x: left, y: top, left, top, width, height, right: left + width, bottom: top + height, toJSON: () => ({}) };
}

beforeEach(() => {
  auth.isAdmin = true;
  // jsdom has no layout: these measurements exercise wiring, not browser geometry.
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    if (this.tagName === 'BUTTON') return rect(120, 20, 28, 28);
    return rect(0, 0, 320, 300);
  });
  vi.stubGlobal('ResizeObserver', class {
    observe() {}
    disconnect() {}
    unobserve() {}
  });
  vi.mocked(uploadBookCover).mockResolvedValue('/covers/11.jpg');
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

function Location() {
  return <output data-testid="location">{useLocation().pathname}</output>;
}
function renderCard(isSelectionMode = false) {
  const callbacks = {
    onCardClick: vi.fn(), onQuickToggleRead: vi.fn(), onExportComicInfo: vi.fn(),
    onWriteComicInfo: vi.fn(), onCopyPath: vi.fn(),
  };
  const view = render(
    <MemoryRouter initialEntries={['/series/1']}>
      <div data-testid="clipping-container" style={{ overflow: 'hidden', width: 160, height: 200 }}>
        <SeriesBookCard book={book} isSelectionMode={isSelectionMode} isSelected={false} {...callbacks} />
      </div>
      <Location />
    </MemoryRouter>
  );
  return { ...view, ...callbacks };
}
function openMenu() {
  const trigger = screen.getByRole('button', { name: 'series.book.moreActions' });
  fireEvent.click(trigger);
  return trigger;
}
function popup() {
  return screen.getByRole('group', { name: 'series.book.moreActions' });
}
function expectNoNavigation() {
  expect(screen.getByTestId('location').textContent).toBe('/series/1');
}

describe('book card anchored actions', () => {
  it('portals beyond a clipping ancestor while preserving both cover clipping layers', () => {
    renderCard();
    const trigger = openMenu();
    const menu = popup();
    expect(menu.parentElement).toBe(document.body);
    expect(screen.getByTestId('clipping-container').contains(menu)).toBe(false);
    expect(screen.getByRole('link').classList.contains('overflow-hidden')).toBe(true);
    expect(trigger.closest('.aspect-3\\/4')?.classList.contains('overflow-hidden')).toBe(true);
    expect(menu.classList.contains('overflow-y-auto')).toBe(true);
    expect(menu.classList.contains('overscroll-contain')).toBe(true);
    expect(menu.style.maxWidth).toBe('calc(100vw - 16px)');
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'series.book.exportComicInfo' }));
    expectNoNavigation();
  });

  it.each(['exportComicInfo', 'writeComicInfo', 'copyPath'] as const)('preserves %s after pointer down inside the portal', (action) => {
    const callbacks = renderCard();
    openMenu();
    const button = screen.getByRole('button', { name: `series.book.${action}` });
    fireEvent.mouseDown(button);
    fireEvent.click(button);
    const callback = action === 'exportComicInfo' ? callbacks.onExportComicInfo
      : action === 'writeComicInfo' ? callbacks.onWriteComicInfo : callbacks.onCopyPath;
    expect(callback).toHaveBeenCalledExactlyOnceWith(book);
    expect(screen.queryByRole('group')).toBeNull();
    expectNoNavigation();
  });

  it('keeps read-only actions for non-admins and hides write/upload', () => {
    auth.isAdmin = false;
    const { container } = renderCard();
    openMenu();
    expect(screen.queryByRole('button', { name: 'series.book.writeComicInfo' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'series.book.uploadCover' })).toBeNull();
    expect(container.querySelector('input[type="file"]')).toBeNull();
    expect(screen.getByRole('button', { name: 'series.book.exportComicInfo' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'series.book.copyPath' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'series.book.download' }));
    expect(downloadBookFile).toHaveBeenCalledExactlyOnceWith(book.id);
    expectNoNavigation();
  });

  it('opens the admin file picker and preserves upload feedback and cover refresh', async () => {
    const { container } = renderCard();
    const input = container.querySelector<HTMLInputElement>('input[type="file"]')!;
    const picker = vi.spyOn(input, 'click');
    openMenu();
    fireEvent.click(screen.getByRole('button', { name: 'series.book.uploadCover' }));
    expect(picker).toHaveBeenCalledOnce();
    expectNoNavigation();
    const file = new File(['cover'], 'cover.png', { type: 'image/png' });
    fireEvent.change(input, { target: { files: [file] } });
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('series.book.coverUploaded', 'success'));
    expect(uploadBookCover).toHaveBeenCalledExactlyOnceWith(book.id, file);
    expect(screen.getByRole('img').getAttribute('src')).toMatch(/^\/api\/covers\/11\?v=\d+$/);
  });

  it('dismisses on Escape and returns focus to an accessible expanded-state trigger', () => {
    renderCard();
    const trigger = openMenu();
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    expect(screen.queryByRole('group')).toBeNull();
    expect(document.activeElement).toBe(trigger);
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    expect(trigger.getAttribute('aria-label')).toBe('series.book.moreActions');
    expectNoNavigation();
  });

  it.each(['backdrop', 'outside'])('dismisses on %s without navigating', (target) => {
    renderCard();
    openMenu();
    fireEvent.click(target === 'backdrop' ? popup().previousElementSibling! : document.body);
    expect(screen.queryByRole('group')).toBeNull();
    expectNoNavigation();
  });

  it('does not bubble non-action popup clicks into card selection or navigation', () => {
    renderCard();
    openMenu();
    fireEvent.click(popup());
    expect(screen.getByRole('group')).toBeTruthy();
    expectNoNavigation();
  });

  it('preserves quick read, selection mode, and ordinary card navigation', () => {
    const first = renderCard();
    fireEvent.click(screen.getByRole('button', { name: 'series.book.quickMarkRead' }));
    expect(first.onQuickToggleRead).toHaveBeenCalledExactlyOnceWith(book, true);
    expectNoNavigation();
    fireEvent.click(screen.getByRole('link'));
    expect(screen.getByTestId('location').textContent).toBe('/reader/11');
    first.unmount();
    const selected = renderCard(true);
    expect(screen.queryByRole('button', { name: 'series.book.moreActions' })).toBeNull();
    fireEvent.click(screen.getByRole('link'));
    expect(selected.onCardClick).toHaveBeenCalledExactlyOnceWith(book);
    expectNoNavigation();
  });
});

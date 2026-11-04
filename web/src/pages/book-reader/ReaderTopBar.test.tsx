// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ComponentProps } from 'react';
import { ReaderTopBar } from './ReaderTopBar';

const mocks = vi.hoisted(() => ({
  isAdmin: true,
  download: vi.fn(),
  setCover: vi.fn().mockResolvedValue(undefined),
  toast: vi.fn(),
}));
vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: mocks.isAdmin }) }));
vi.mock('../../components/ToastProvider', () => ({ useToast: () => ({ showToast: mocks.toast }) }));
vi.mock('../../utils/download', () => ({ downloadBookFile: mocks.download }));
vi.mock('../../utils/cover', () => ({ setBookCoverFromPage: mocks.setCover }));

function setup(overrides: Partial<ComponentProps<typeof ReaderTopBar>> = {}) {
  const props = {
    t: (key: string) => key,
    bookTitle: 'A long manga title', bookVolume: 'Volume 1',
    isBookmarked: false, savingBookmark: false, loading: false,
    showHelp: false, showSettings: false,
    onBack: vi.fn(), onSaveBookmark: vi.fn(), onToggleHelp: vi.fn(), onToggleSettings: vi.fn(),
    allInVolume: [1, 2].map(id => ({ id, name: `${id}`, title: `Chapter ${id}`, volume: '1' })),
    currentBookId: 1, currentPageNumber: 3, onOpenBook: vi.fn(),
    ...overrides,
  };
  render(<ReaderTopBar {...props} />);
  return props;
}
function openMore() {
  fireEvent.click(screen.getByRole('button', { name: 'reader.moreActions' }));
  return within(screen.getByRole('group', { name: 'reader.moreActions' }));
}
afterEach(cleanup);
beforeEach(() => { vi.clearAllMocks(); mocks.isAdmin = true; });

describe('ReaderTopBar compact actions', () => {
  it('opens secondary actions and closes after adding a bookmark', () => {
    const props = setup();
    const actions = openMore();
    fireEvent.click(actions.getByRole('button', { name: 'reader.bookmark.add' }));
    expect(props.onSaveBookmark).toHaveBeenCalledOnce();
    expect(screen.queryByRole('group', { name: 'reader.moreActions' })).toBeNull();
  });

  it('preserves download and cover actions for the current page', () => {
    setup();
    fireEvent.click(openMore().getByRole('button', { name: 'reader.download' }));
    expect(mocks.download).toHaveBeenCalledWith(1);
    fireEvent.click(openMore().getByRole('button', { name: 'reader.setCover' }));
    expect(mocks.setCover).toHaveBeenCalledWith(1, 3);
  });

  it('keeps admin-only actions hidden for non-admins', () => {
    mocks.isAdmin = false;
    setup();
    const actions = openMore();
    expect(actions.queryByRole('button', { name: 'reader.setCover' })).toBeNull();
    expect(actions.getByRole('button', { name: 'reader.download' })).toBeTruthy();
  });

  it('keeps loading bookmarks disabled and hides book actions without a book', () => {
    setup({ currentBookId: null, loading: true });
    const actions = openMore();
    expect(actions.queryByRole('button', { name: 'reader.download' })).toBeNull();
    expect(actions.queryByRole('button', { name: 'reader.setCover' })).toBeNull();
    expect((actions.getByRole('button', { name: 'reader.bookmark.add' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('closes the popover on outside pointer events or Escape and restores focus', () => {
    setup();
    openMore();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole('group', { name: 'reader.moreActions' })).toBeNull();
    openMore();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('group', { name: 'reader.moreActions' })).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'reader.moreActions' }));
  });

  it('keeps chapters and more actions mutually exclusive', () => {
    const props = setup();
    openMore();
    fireEvent.click(screen.getByTitle('reader.siblings.volumeChapters'));
    expect(screen.queryByRole('group', { name: 'reader.moreActions' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Chapter 2' })).toBeTruthy();
    openMore();
    expect(screen.queryByRole('button', { name: 'Chapter 2' })).toBeNull();
    fireEvent.click(screen.getByTitle('reader.siblings.volumeChapters'));
    fireEvent.click(screen.getByRole('button', { name: 'Chapter 2' }));
    expect(props.onOpenBook).toHaveBeenCalledWith(2);
    expect(screen.queryByRole('button', { name: 'Chapter 2' })).toBeNull();
  });

  it('opens help and settings without leaving an overlapping action popover', () => {
    const props = setup();
    fireEvent.click(openMore().getByRole('button', { name: 'reader.help' }));
    expect(props.onToggleHelp).toHaveBeenCalledOnce();
    openMore();
    fireEvent.click(screen.getByTitle('reader.settings'));
    expect(props.onToggleSettings).toHaveBeenCalledOnce();
    expect(screen.queryByRole('group', { name: 'reader.moreActions' })).toBeNull();
  });
});

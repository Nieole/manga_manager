// @vitest-environment jsdom
import { useRef } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { apiClient } from '../../api/client';
import { BookmarkPanel } from './BookmarkPanel';
import { useReaderBookmarks } from './useReaderBookmarks';

vi.mock('../../api/client', () => ({ apiClient: { get: vi.fn(), post: vi.fn(), delete: vi.fn() } }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });

const saved = { id: 41, book_id: 1, page: 5, note: 'saved note', created_at: '2026-10-20', updated_at: '2026-10-20' };
const t = (key: string) => key;

function ReaderBookmarks({ bookId = '1' }: { bookId?: string }) {
  const currentBookIdRef = useRef<string | null>(bookId);
  const state = useReaderBookmarks({ bookId, currentBookIdRef, activePageCount: 12, currentPageNumber: 5 });
  return <BookmarkPanel t={t} {...state} loading={false} currentPageNumber={5}
    onBookmarkNoteChange={state.setBookmarkNote} onSaveBookmark={state.saveBookmark}
    onDeleteBookmark={state.deleteBookmark} onJumpToPage={() => {}} />;
}

it('loads notes, updates the existing bookmark and deletes it through the reader panel', async () => {
  vi.mocked(apiClient.get).mockResolvedValue({ data: [saved] });
  vi.mocked(apiClient.post).mockResolvedValue({ data: { ...saved, note: 'edited note' } });
  vi.mocked(apiClient.delete).mockResolvedValue({ data: { status: 'Bookmark deleted' } });
  render(<ReaderBookmarks />);
  await screen.findByText('saved note');
  await waitFor(() => expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('saved note'));
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'edited note' } });
  fireEvent.click(screen.getByRole('button', { name: 'reader.bookmark.update' }));
  await screen.findByText('edited note');
  expect(screen.queryByText('saved note')).toBeNull();
  expect(screen.getAllByTitle('reader.bookmark.delete')).toHaveLength(1);
  expect(apiClient.post).toHaveBeenCalledWith('/api/books/1/bookmarks', { page: 5, note: 'edited note' });
  fireEvent.click(screen.getByTitle('reader.bookmark.delete'));
  await screen.findByText('reader.bookmark.empty');
  expect(apiClient.delete).toHaveBeenCalledWith('/api/books/1/bookmarks/41');
});

it('shows an empty list and creates a bookmark with the returned identity', async () => {
  vi.mocked(apiClient.get).mockResolvedValue({ data: [] });
  vi.mocked(apiClient.post).mockResolvedValue({ data: saved });
  vi.mocked(apiClient.delete).mockResolvedValue({ data: {} });
  render(<ReaderBookmarks />);
  await screen.findByText('reader.bookmark.empty');
  fireEvent.change(screen.getByRole('textbox'), { target: { value: 'saved note' } });
  fireEvent.click(screen.getByRole('button', { name: 'reader.bookmark.addCurrent' }));
  await screen.findByText('saved note');
  fireEvent.click(screen.getByTitle('reader.bookmark.delete'));
  await waitFor(() => expect(apiClient.delete).toHaveBeenCalledWith('/api/books/1/bookmarks/41'));
});

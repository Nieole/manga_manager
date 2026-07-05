import { describe, expect, it } from 'vitest'
import { buildContinueCta, isFullyRead } from './useSeriesContinue'
import type { Book, SeriesContinue } from '../types'

function makeBook(fields: Partial<Book> & { id: number }): Book {
  return {
    name: `Book ${fields.id}`,
    library_id: 1,
    volume: '',
    page_count: 0,
    ...fields,
  }
}

function makeContinue(fields: Partial<SeriesContinue>): SeriesContinue {
  return {
    total_books: 0,
    read_books: 0,
    total_pages: 0,
    read_pages: 0,
    ...fields,
  }
}

describe('buildContinueCta', () => {
  it('returns null without continue info', () => {
    expect(buildContinueCta(null, [])).toBeNull()
  })

  it('returns null when no book id is resolvable', () => {
    expect(buildContinueCta(makeContinue({ total_books: 3 }), [])).toBeNull()
  })

  it('prefers the next unread book and drops last_read_page when it belongs to another book', () => {
    const info = makeContinue({
      next_unread_book_id: 2,
      last_read_book_id: 1,
      last_read_page: 15,
      total_books: 3,
      read_books: 1,
    })
    const cta = buildContinueCta(info, [makeBook({ id: 2, volume: 'v2', page_count: 40 })])
    expect(cta).not.toBeNull()
    expect(cta?.bookId).toBe(2)
    // last_read_page belongs to book 1, but the CTA resolved to book 2, so page resets to 0.
    expect(cta?.page).toBe(0)
    expect(cta?.totalPages).toBe(40)
    expect(cta?.volumeLabel).toBe('v2')
  })

  it('keeps last_read_page when the resolved book equals last_read_book_id and uses the title label', () => {
    const info = makeContinue({ last_read_book_id: 5, last_read_page: 12 })
    const cta = buildContinueCta(info, [
      makeBook({ id: 5, page_count: 30, title: { String: 'Titled Volume', Valid: true } }),
    ])
    expect(cta?.page).toBe(12)
    expect(cta?.bookLabel).toBe('Titled Volume')
  })

  it('falls back to the book name when the title is not valid', () => {
    const info = makeContinue({ last_read_book_id: 7 })
    const cta = buildContinueCta(info, [
      makeBook({ id: 7, name: 'Fallback Name', title: { String: 'ignored', Valid: false } }),
    ])
    expect(cta?.bookLabel).toBe('Fallback Name')
  })
})

describe('buildContinueCta edge cases', () => {
  it('resolves a bookId even when the book is not in the list, with safe defaults', () => {
    // 书列表尚未加载但已知 next_unread_book_id：仍返回可跳转的 CTA，页码/总页数/标签取安全默认。
    const cta = buildContinueCta(makeContinue({ next_unread_book_id: 9, last_read_page: 4 }), []);
    expect(cta).not.toBeNull();
    expect(cta?.bookId).toBe(9);
    expect(cta?.page).toBe(0); // last_read_page 属于未知的 last_read_book，不套用
    expect(cta?.totalPages).toBe(0);
    expect(cta?.volumeLabel).toBeUndefined();
    expect(cta?.bookLabel).toBe('');
  });

  it('drops a whitespace-only volume label to undefined and trims a real one', () => {
    const blank = buildContinueCta(makeContinue({ last_read_book_id: 3 }), [makeBook({ id: 3, volume: '   ' })]);
    expect(blank?.volumeLabel).toBeUndefined();

    const trimmed = buildContinueCta(makeContinue({ last_read_book_id: 4 }), [makeBook({ id: 4, volume: '  v3  ' })]);
    expect(trimmed?.volumeLabel).toBe('v3');
  });

  it('prefers next_unread over last_read when both are present', () => {
    const info = makeContinue({ next_unread_book_id: 8, last_read_book_id: 2 });
    const cta = buildContinueCta(info, [makeBook({ id: 8, volume: 'v8' }), makeBook({ id: 2, volume: 'v2' })]);
    expect(cta?.bookId).toBe(8);
  });
});

describe('isFullyRead', () => {
  it('is false without continue info', () => {
    expect(isFullyRead(null)).toBe(false)
  })

  it('is true only when read_books reaches total_books and total is positive', () => {
    expect(isFullyRead(makeContinue({ total_books: 0, read_books: 0 }))).toBe(false)
    expect(isFullyRead(makeContinue({ total_books: 3, read_books: 2 }))).toBe(false)
    expect(isFullyRead(makeContinue({ total_books: 3, read_books: 3 }))).toBe(true)
    expect(isFullyRead(makeContinue({ total_books: 3, read_books: 4 }))).toBe(true)
  })
})

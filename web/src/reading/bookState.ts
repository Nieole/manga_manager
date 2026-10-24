interface ReadingBook {
 page_count: number;
 is_read?: boolean;
 last_read_page?: { Valid: boolean; Int64: number };
}

/** A completion mark carries no page evidence when the page count was unknown. */
export function bookReadingState(book: ReadingBook) {
 const reported = book.last_read_page?.Valid ? Math.max(0, book.last_read_page.Int64) : 0;
 const readPages = book.page_count > 0 ? Math.min(reported, book.page_count) : reported;
 return {
  completed: book.is_read === true || (book.page_count > 0 && readPages >= book.page_count),
  readPages,
 };
}

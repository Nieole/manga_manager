import { afterEach, describe, expect, it, vi } from 'vitest';
import { apiClient } from '../api/client';
import { setBookCoverFromPage, uploadBookCover } from './cover';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('setBookCoverFromPage', () => {
  it('POSTs the page number to the cover endpoint and returns the new path', async () => {
    const post = vi
      .spyOn(apiClient, 'post')
      .mockResolvedValue({ data: { cover_path: '/covers/42.jpg' } });

    const result = await setBookCoverFromPage(42, 3);

    expect(post).toHaveBeenCalledWith('/api/books/42/cover', { page: 3 });
    expect(result).toBe('/covers/42.jpg');
  });

  it('returns an empty string when the response omits cover_path', async () => {
    vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} });
    expect(await setBookCoverFromPage(7, 1)).toBe('');
  });

  it('returns an empty string when the response body is missing entirely', async () => {
    vi.spyOn(apiClient, 'post').mockResolvedValue({ data: undefined });
    expect(await setBookCoverFromPage(7, 1)).toBe('');
  });
});

describe('uploadBookCover', () => {
  it('POSTs multipart form data with the file and returns the new path', async () => {
    const post = vi
      .spyOn(apiClient, 'post')
      .mockResolvedValue({ data: { cover_path: '/covers/up.jpg' } });
    const file = new File(['binarydata'], 'my cover.png', { type: 'image/png' });

    const result = await uploadBookCover(9, file);

    expect(result).toBe('/covers/up.jpg');
    expect(post).toHaveBeenCalledTimes(1);
    const [url, body, config] = post.mock.calls[0] as [string, FormData, { headers: Record<string, string> }];
    expect(url).toBe('/api/books/9/cover/upload');
    expect(body).toBeInstanceOf(FormData);
    const uploaded = body.get('file');
    expect(uploaded).toBeInstanceOf(File);
    expect((uploaded as File).name).toBe('my cover.png');
    expect(config.headers['Content-Type']).toBe('multipart/form-data');
  });

  it('returns an empty string when the upload response omits cover_path', async () => {
    vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} });
    const file = new File(['x'], 'x.png', { type: 'image/png' });
    expect(await uploadBookCover(9, file)).toBe('');
  });
});

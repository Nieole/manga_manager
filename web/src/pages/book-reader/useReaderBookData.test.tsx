/**
 * @vitest-environment jsdom
 *
 * 本文件守「取页失败时阅读器必须说话」：失败要终结加载态，提示要指名是哪个资料库读不到。
 * readerLoading 除 loading 外还叠了「页清单是否属于当前这本书」，失败路径上后者永远补不上，
 * 于是加载态恒真、两套主题都先判它再判 loadError，错误界面一次也渲染不到——用户看到的就是黑屏。
 * 现场是一块外置盘被拔掉，落在那个资料库上的书取页全部 500。
 */

import { renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../../api/client';
import type { StorageFailureResponse } from '../../api/generated';
import { loadLocaleMessages, translateInLocale } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { useReaderBookData } from './useReaderBookData';

const BOOK_ID = '3108127';

// 后端在盘掉了时下发的响应体（storage_diagnosis.go 的 StorageFailureResponse）。
const OFFLINE_BODY: StorageFailureResponse = {
  error: 'Library "dmzj" (G:\\dmzj) is unreachable; check that the storage is connected',
  reason: 'storage_offline',
  library_id: 4,
  library_name: 'dmzj',
  library_path: 'G:\\dmzj',
  path: 'G:\\dmzj\\Series\\vol01.zip',
};

// 手搓一个 axios 形状的错误：isAxiosError 只认 isAxiosError === true 这一个标记，
// 而 lint 禁止测试直接 import axios 的值（鉴权与 locale 拦截器只挂在 apiClient 上）。
function axiosFailure(status: number, data: unknown) {
  return Object.assign(new Error(`Request failed with status code ${status}`), {
    isAxiosError: true,
    response: { status, data },
  });
}

// 直接用真实词条翻译，避免测试自己抄一份文案而与 locale 漂移。
const t = (key: string, params?: Record<string, string | number | boolean | null | undefined>) =>
  translateInLocale('zh-CN', key, params);

function renderReaderData() {
  const currentBookIdRef = { current: BOOK_ID as string | null };
  const tRef = { current: t };
  const cache: Record<string, Record<string, unknown>> = {};
  return renderHook(() => useReaderBookData({
    bookId: BOOK_ID,
    currentBookIdRef,
    tRef,
    getBookCache: (id: string) => (cache[id] ??= {}) as never,
    setCachedPageImageUrls: () => {},
    cachedImageUrlsForBook: () => ({}),
    retainBookCaches: () => {},
    setCurrentPageIndex: () => {},
    setSliderValue: () => {},
  }));
}

// translateInLocale 只读已进缓存的词条表，没预热就会原样回吐 key。
beforeAll(async () => {
  await loadLocaleMessages('zh-CN');
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('useReaderBookData 的失败路径', () => {
  it('存储离线时结束加载态，而不是把用户留在黑屏上', async () => {
    vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
      if (url.startsWith('/api/pages/')) {
        throw axiosFailure(503, OFFLINE_BODY);
      }
      return { data: { id: 3108127, name: 'vol01', series_id: 1, volume: '' } };
    }) as never);

    const { result } = renderReaderData();

    await waitFor(() => expect(result.current.loadError).not.toBeNull());
    // 这一条才是黑屏本身：loading 结束了不算数，readerLoading 还挂着主题就只画转圈。
    expect(result.current.readerLoading).toBe(false);
    expect(result.current.loading).toBe(false);
  });

  it('提示要说出是哪个资料库读不到，而不是一句「加载失败」', async () => {
    vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
      if (url.startsWith('/api/pages/')) {
        throw axiosFailure(503, OFFLINE_BODY);
      }
      return { data: { id: 3108127, name: 'vol01', series_id: 1, volume: '' } };
    }) as never);

    const { result } = renderReaderData();

    await waitFor(() => expect(result.current.loadError).not.toBeNull());
    expect(result.current.loadError).toBe(t('storage.error.offline', { name: 'dmzj', path: 'G:\\dmzj' }));
    expect(result.current.loadError).toContain('dmzj');
    expect(result.current.loadError).not.toBe(zhCN['reader.error.loadFailed']);
  });

  it('单本文件缺失与存储离线给出的是不同的话', async () => {
    const missing: StorageFailureResponse = {
      error: 'Book file is missing on disk: E:\\bilicomic\\S\\v01.zip',
      reason: 'file_missing',
      library_id: 1,
      library_name: 'bilicomic',
      library_path: 'E:\\bilicomic',
      path: 'E:\\bilicomic\\S\\v01.zip',
    };
    vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
      if (url.startsWith('/api/pages/')) {
        throw axiosFailure(404, missing);
      }
      return { data: { id: 1, name: 'v01', series_id: 1, volume: '' } };
    }) as never);

    const { result } = renderReaderData();

    await waitFor(() => expect(result.current.loadError).not.toBeNull());
    expect(result.current.readerLoading).toBe(false);
    expect(result.current.loadError).toBe(t('storage.error.fileMissing', { path: 'E:\\bilicomic\\S\\v01.zip' }));
    expect(result.current.loadError).not.toBe(t('storage.error.offline', { name: 'bilicomic', path: 'E:\\bilicomic' }));
  });

  it('归档损坏是第三种说法', async () => {
    const broken: StorageFailureResponse = {
      error: 'Book archive cannot be read (corrupt, encrypted or empty): E:\\m\\S\\v01.cbz',
      reason: 'archive_unreadable',
      library_id: 2,
      library_name: 'manga',
      library_path: 'E:\\m',
      path: 'E:\\m\\S\\v01.cbz',
    };
    vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
      if (url.startsWith('/api/pages/')) {
        throw axiosFailure(422, broken);
      }
      return { data: { id: 1, name: 'v01', series_id: 1, volume: '' } };
    }) as never);

    const { result } = renderReaderData();

    await waitFor(() => expect(result.current.loadError).not.toBeNull());
    expect(result.current.loadError).toBe(t('storage.error.archiveUnreadable', { path: 'E:\\m\\S\\v01.cbz' }));
  });

  it('书信息那条腿失败也一样要结束加载态', async () => {
    // /api/pages 与 /api/book-info 在同一个 Promise.all 里，任一条 reject 都会走 catch。
    vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
      if (url.startsWith('/api/book-info/')) {
        throw axiosFailure(503, OFFLINE_BODY);
      }
      return { data: [{ number: 1, url: '/api/books/page/1/1' }] };
    }) as never);

    const { result } = renderReaderData();

    await waitFor(() => expect(result.current.loadError).not.toBeNull());
    expect(result.current.readerLoading).toBe(false);
  });

  it('取页成功时不退化：加载态正常结束、没有错误', async () => {
    vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
      if (url.startsWith('/api/pages/')) {
        return { data: [{ number: 2, url: '/api/books/page/1/2' }, { number: 1, url: '/api/books/page/1/1' }] };
      }
      if (url.startsWith('/api/book-info/')) {
        return { data: { id: 1, name: 'vol01', series_id: 1, volume: '01' } };
      }
      return { data: { id: null } };
    }) as never);

    const { result } = renderReaderData();

    await waitFor(() => expect(result.current.readerLoading).toBe(false));
    expect(result.current.loadError).toBeNull();
    expect(result.current.activePages.map((page) => page.number)).toEqual([1, 2]);
  });
});

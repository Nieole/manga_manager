/**
 * @vitest-environment jsdom
 *
 * 守离线书架上的两个回收入口：索引空了但盘上还有字节时，「清空离线缓存」必须还能点——否则
 * 那些字节要么等用户下次显式登出、要么永远占着配额，而书架上再没有第二个出口；以及一次只准
 * 有一本书在删，删除按钮不能只按自己那本判。
 */

import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import OfflineShelf from './OfflineShelf';
import { cacheBookForOffline } from './book-reader/offlineReader';

class FakeCache {
  entries = new Map<string, unknown>();

  async put(request: Request, response: unknown) {
    this.entries.set(request.url, response);
  }

  async keys() {
    return Array.from(this.entries.keys()).map((url) => new Request(url));
  }

  async match(request: Request | string) {
    const url = typeof request === 'string' ? request : request.url;
    return this.entries.get(url);
  }

  async delete(request: Request | string) {
    const url = typeof request === 'string' ? request : request.url;
    return this.entries.delete(url);
  }
}

// fakeResponse 的 clone 指回自己，好让「落盘的就是响应本身」与「统计体积要 clone().blob()」
// 两条路径都走得通。
function fakeResponse(size: number) {
  const response: Record<string, unknown> = {
    ok: true,
    status: 200,
    blob: async () => ({ size }),
  };
  response.clone = () => response;
  return response;
}

let cache: FakeCache;

beforeEach(() => {
  localStorage.clear();
  cache = new FakeCache();
  vi.stubGlobal('caches', {
    open: async () => cache,
    delete: async () => {
      cache.entries.clear();
      return true;
    },
    has: async () => true,
    keys: async () => [],
    match: async () => undefined,
  });
  Object.defineProperty(window.navigator, 'serviceWorker', {
    configurable: true,
    value: { register: async () => undefined },
  });
  vi.stubGlobal('fetch', async () => fakeResponse(1024) as unknown as Response);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  Reflect.deleteProperty(window.navigator, 'serviceWorker');
  localStorage.clear();
});

async function seedBook(bookId: string) {
  await cacheBookForOffline({
    bookId,
    title: `书 ${bookId}`,
    pages: [1, 2],
    imageProfile: 'original',
    imageUrlForPage: (page: number) => `/api/pages/${bookId}/${page}`,
  } as never);
}

function renderShelf() {
  return render(
    <MemoryRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <OfflineShelf />
      </LocaleProvider>
    </MemoryRouter>,
  );
}

function button(name: string) {
  return screen.getByRole('button', { name }) as HTMLButtonElement;
}

describe('离线书架的孤儿字节回收', () => {
  // 复现 A-2 的后半段：删除某本书时另一本刚下载完，索引被旧快照盖掉，字节却留在盘上。
  // 界面于是自相矛盾——「缓存大小」照样有占用，书目却一本都没有，而唯一的回收按钮是灰的。
  it('索引空了但盘上还有字节时，清空全部仍可点，并说清有多少可回收', async () => {
    await cache.put(new Request('https://x.test/api/pages/7'), fakeResponse(1024));
    await cache.put(new Request('https://x.test/api/pages/7/1'), fakeResponse(2048));

    renderShelf();
    await screen.findByText(zhCN['offlineShelf.emptyTitle']);

    expect(button(zhCN['offlineShelf.clearAll']).disabled).toBe(false);
    expect(screen.getByText(/还留着/)).toBeTruthy();

    await act(async () => {
      fireEvent.click(button(zhCN['offlineShelf.clearAll']));
    });

    expect(cache.entries.size).toBe(0);
    await waitFor(() => expect(button(zhCN['offlineShelf.clearAll']).disabled).toBe(true));
  });

  // 反向判据：真的什么都没有时按钮照旧是灰的，也不提回收。
  it('索引与缓存都空时清空全部仍然禁用', async () => {
    renderShelf();
    await screen.findByText(zhCN['offlineShelf.emptyTitle']);

    expect(button(zhCN['offlineShelf.clearAll']).disabled).toBe(true);
    expect(screen.queryByText(/还留着/)).toBeNull();
  });
});

describe('离线书架的删除按钮', () => {
  // 复现 A-1 的触发条件：删除按钮只按自己那本判禁用，第二本的按钮全程可点，
  // 于是「点完第一本紧接着点第二本」这条最自然的操作正好落在第一次删除跑完之前。
  it('一本书正在删时，另一本书的删除按钮同样禁用', async () => {
    await seedBook('1');
    await seedBook('2');

    let releaseDelete: () => void = () => {};
    const gate = new Promise<void>((resolve) => {
      releaseDelete = resolve;
    });
    const originalDelete = cache.delete.bind(cache);
    cache.delete = async (request: Request | string) => {
      await gate;
      return originalDelete(request);
    };

    renderShelf();
    await waitFor(() => expect(screen.getAllByRole('button', { name: zhCN['offlineShelf.remove'] })).toHaveLength(2));

    const [first] = screen.getAllByRole('button', { name: zhCN['offlineShelf.remove'] });
    await act(async () => {
      fireEvent.click(first);
    });

    const removeButtons = screen.getAllByRole('button', { name: zhCN['offlineShelf.remove'] }) as HTMLButtonElement[];
    expect(removeButtons.map((node) => node.disabled)).toEqual([true, true]);

    await act(async () => {
      releaseDelete();
      await gate;
    });
    await waitFor(() => expect(screen.getAllByRole('button', { name: zhCN['offlineShelf.remove'] })).toHaveLength(1));
  });
});

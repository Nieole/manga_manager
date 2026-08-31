/**
 * 本文件守卫「刮削源列表不会被一次瞬时错误永久毒化」。
 *
 * 这个列表是全局缓存一次的（避免每个卡片都发一次请求），但失败分支不得写缓存：一次网络
 * 抖动或后端刚重启，空数组一旦被固化就是整个会话的答案——刮削菜单从此永远是空的，
 * 用户不刷新页面就再也刮不了元数据。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';

import { apiClient } from '../api/client';
import { __resetScrapeProvidersCacheForTest, loadScrapeProviders } from './useScrapeProviders';

afterEach(() => {
  vi.restoreAllMocks();
  __resetScrapeProvidersCacheForTest();
});

describe('loadScrapeProviders', () => {
  it('一次失败之后仍会重试，而不是把空结果固化下来', async () => {
    const get = vi.spyOn(apiClient, 'get');
    get.mockRejectedValueOnce(new Error('network down'));

    await expect(loadScrapeProviders()).resolves.toEqual([]);

    // 第二次调用必须真的再发一次请求。若失败分支写了缓存，这里会直接命中缓存、
    // 请求次数停在 1，刮削菜单在整个会话里永远是空的。
    get.mockResolvedValueOnce({ data: [{ id: 'bangumi', name: 'Bangumi', description: '' }] } as never);
    await expect(loadScrapeProviders()).resolves.toHaveLength(1);
    expect(get).toHaveBeenCalledTimes(2);
  });

  it('成功结果仍然只取一次', async () => {
    const get = vi.spyOn(apiClient, 'get');
    get.mockResolvedValue({ data: [{ id: 'anilist', name: 'AniList', description: '' }] } as never);

    await loadScrapeProviders();
    await loadScrapeProviders();
    // 缓存的意义就在这里：成功之后不该每个卡片都再问一遍。
    expect(get).toHaveBeenCalledTimes(1);
  });

  it('后端返回非数组时按空列表处理并缓存', async () => {
    const get = vi.spyOn(apiClient, 'get');
    get.mockResolvedValue({ data: { unexpected: true } } as never);

    await expect(loadScrapeProviders()).resolves.toEqual([]);
    await loadScrapeProviders();
    // 这是一个**明确的答案**（后端说没有源），与「请求失败」不同，可以缓存。
    expect(get).toHaveBeenCalledTimes(1);
  });
});

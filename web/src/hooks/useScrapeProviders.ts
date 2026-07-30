/**
 * 业务说明：本文件提供“可用刮削源列表”的前端读取，供系列页/资料库的刮削菜单动态渲染。
 * 源列表由后端 /api/metadata/providers 返回（AniList/MangaDex 恒有，MyAnimeList/Comic Vine 仅在配置密钥时出现），
 * 因此菜单不再硬编码 bangumi/llm。列表全局缓存一次，避免每个卡片重复请求。
 */

import { useEffect, useState } from 'react';
import { apiClient } from '../api/client';

export interface ScrapeProvider {
  id: string;
  name: string;
  description: string;
}

let cached: ScrapeProvider[] | null = null;
let inflight: Promise<ScrapeProvider[]> | null = null;

// loadScrapeProviders 导出供测试直接驱动缓存语义（组件层仍用下面的 hook）。
export function loadScrapeProviders(): Promise<ScrapeProvider[]> {
  if (cached) return Promise.resolve(cached);
  if (!inflight) {
    inflight = apiClient
      .get<ScrapeProvider[]>('/api/metadata/providers')
      .then((res) => {
        cached = Array.isArray(res.data) ? res.data : [];
        return cached;
      })
      .catch(() => {
        // 失败时**不写缓存**。此前这里 `cached = []`，于是一次瞬时错误（网络抖动、
        // 后端刚重启）就把空数组固化成整个会话的答案：刮削菜单从此永远是空的，
        // 用户不刷新页面就再也刮不了元数据，而界面上没有任何提示说明为什么。
        // 不缓存意味着下一个挂载的组件会重试一次——正是这里想要的行为。
        return [];
      })
      .finally(() => {
        inflight = null;
      });
  }
  return inflight;
}

// useScrapeProviders 返回后端声明的可用刮削源；首次挂载时拉取一次（结果全局缓存）。
export function useScrapeProviders(): ScrapeProvider[] {
  const [providers, setProviders] = useState<ScrapeProvider[]>(cached ?? []);
  useEffect(() => {
    let active = true;
    loadScrapeProviders().then((list) => {
      if (active) setProviders(list);
    });
    return () => {
      active = false;
    };
  }, []);
  return providers;
}

// __resetScrapeProvidersCacheForTest 清空模块级缓存。仅供测试使用：
// 缓存是模块级的，用例之间不隔离的话前一条的结果会渗进后一条。
export function __resetScrapeProvidersCacheForTest(): void {
  cached = null;
  inflight = null;
}

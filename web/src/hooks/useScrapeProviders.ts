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

function loadProviders(): Promise<ScrapeProvider[]> {
  if (cached) return Promise.resolve(cached);
  if (!inflight) {
    inflight = apiClient
      .get<ScrapeProvider[]>('/api/metadata/providers')
      .then((res) => {
        cached = Array.isArray(res.data) ? res.data : [];
        return cached;
      })
      .catch(() => {
        cached = [];
        return cached;
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
    loadProviders().then((list) => {
      if (active) setProviders(list);
    });
    return () => {
      active = false;
    };
  }, []);
  return providers;
}

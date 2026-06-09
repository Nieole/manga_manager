/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

const STORAGE_PREFIX = 'series_open_volumes_';

function loadStored(seriesId: string | undefined): Set<string> {
  if (!seriesId || typeof window === 'undefined') return new Set();
  try {
    const raw = window.localStorage.getItem(`${STORAGE_PREFIX}${seriesId}`);
    if (!raw) return new Set();
    const arr = JSON.parse(raw);
    return Array.isArray(arr) ? new Set(arr.filter((s) => typeof s === 'string')) : new Set();
  } catch {
    return new Set();
  }
}

interface UseSeriesOpenVolumesParams {
  seriesId: string | undefined;
  knownVolumes: string[];
}

export function useSeriesOpenVolumes({ seriesId, knownVolumes }: UseSeriesOpenVolumesParams) {
  const [params, setParams] = useSearchParams();
  const [open, setOpen] = useState<Set<string>>(() => loadStored(seriesId));
  const [hydrated, setHydrated] = useState(false);

  useEffect(() => {
     
    setOpen(loadStored(seriesId));
    setHydrated(false);
  }, [seriesId]);

  // 兼容 ?volume=... 旧链接：进入时自动展开匹配卷
  useEffect(() => {
    if (hydrated || knownVolumes.length === 0) return;
    const queryVol = params.get('volume');
    if (queryVol && knownVolumes.includes(queryVol)) {
       
      setOpen((prev) => {
        if (prev.has(queryVol)) return prev;
        const next = new Set(prev);
        next.add(queryVol);
        return next;
      });
    }
    setHydrated(true);
  }, [hydrated, knownVolumes, params]);

  useEffect(() => {
    if (!seriesId || typeof window === 'undefined') return;
    try {
      window.localStorage.setItem(`${STORAGE_PREFIX}${seriesId}`, JSON.stringify(Array.from(open)));
    } catch {
      /* ignore quota / privacy errors */
    }
  }, [seriesId, open]);

  const toggle = useCallback(
    (name: string) => {
      setOpen((prev) => {
        const next = new Set(prev);
        if (next.has(name)) next.delete(name);
        else next.add(name);
        return next;
      });
      // 用户主动操作后清除旧 volume= 链接，避免下次刷新被强制展开
      if (params.has('volume')) {
        const next = new URLSearchParams(params);
        next.delete('volume');
        setParams(next, { replace: true });
      }
    },
    [params, setParams],
  );

  const expandAll = useCallback(() => {
    setOpen(new Set(knownVolumes));
  }, [knownVolumes]);

  const collapseAll = useCallback(() => {
    setOpen(new Set());
  }, []);

  const isOpen = useCallback((name: string) => open.has(name), [open]);

  const allOpen = useMemo(
    () => knownVolumes.length > 0 && knownVolumes.every((name) => open.has(name)),
    [knownVolumes, open],
  );

  return { isOpen, toggle, expandAll, collapseAll, allOpen };
}

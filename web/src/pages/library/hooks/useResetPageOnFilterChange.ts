import { useEffect } from 'react';

import type { UseLibraryFiltersResult } from './useLibraryFilters';

/**
 * 用户改筛选、排序或每页数量后回到第 1 页并清空游标缓存。
 *
 * 判据只认 userFilterRevision：它由 useLibraryFilters 在公开 setter 上记账，
 * 水合与深链恢复不会动它。于是水合分几轮到达都不影响本效果——它根本不看那些值，
 * 深链 ?page=3 停得住，而首访(本地无存档)那种一轮都不改的水合也不会白吃掉一次重置。
 */
export function useResetPageOnFilterChange(filters: UseLibraryFiltersResult, resetPagination: () => void) {
  const { settingsReady, userFilterRevision, setPage } = filters;
  useEffect(() => {
    if (userFilterRevision === 0) return;
    if (!settingsReady) return;
    setPage(1);
    resetPagination();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userFilterRevision]);
}

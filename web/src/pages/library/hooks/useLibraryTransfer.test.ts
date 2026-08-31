/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「转移到外部库不会因为只看得见当前页而谎报成功」。批量选择是跨页保留的，
 * 而外部库覆盖表只装当前页；缺的那部分必须补查后再判定。任何一个选中系列的覆盖状态未知时
 * 都不得下「已全部存在」的结论——否则用户的转移被静默吞掉，界面还弹一条绿色成功提示。
 */

import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { ExternalSeriesStatus, ExternalSession, Series } from '../types';
import { useLibraryTransfer } from './useLibraryTransfer';

// 库内该系列有几本书 / 其中几本已经在外部库里，两者共同决定覆盖状态与「需复制」的数量。
const BOOK_COUNT: Record<number, number> = { 1: 3, 2: 2, 3: 3, 4: 4, 5: 2 };
const EXTERNAL_MATCHED: Record<number, number> = { 1: 0, 2: 0, 3: 3, 4: 1, 5: 2 };

// 第 2 页当前显示 3、4 两个系列：外部库覆盖表与列表数据都只有它们。
const CURRENT_PAGE = [3, 4];

const READY_SESSION: ExternalSession = {
  session_id: 'sess-1',
  library_id: 7,
  external_path: '/mnt/external',
  ignore_extension: false,
  status: 'ready',
  scanned_files: 9,
  matched_books: 6,
  unmatched_files: 3,
  total_books: 14,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:01:00Z',
};

/** 复刻后端 GetSeriesCoverage：问到的每个 id 都回一条，会话里没有的记 0/0。 */
function statusOf(seriesId: number): ExternalSeriesStatus {
  const total = BOOK_COUNT[seriesId] ?? 0;
  const matched = EXTERNAL_MATCHED[seriesId] ?? 0;
  return {
    series_id: seriesId,
    external_match_count: matched,
    external_total_count: total,
    external_sync_status: total > 0 && matched >= total ? 'complete' : matched > 0 ? 'partial' : 'missing',
  };
}

function cardOf(seriesId: number): Series {
  return {
    id: seriesId,
    name: `系列${seriesId}`,
    volume_count: BOOK_COUNT[seriesId],
    actual_book_count: BOOK_COUNT[seriesId],
    read_count: 0,
    total_pages: { Float64: 0, Valid: false },
    is_favorite: false,
  };
}

function setup(selectedSeries: number[], options: { queryFails?: boolean; queryReturnsNothing?: boolean } = {}) {
  const showToast = vi.fn();
  const showError = vi.fn();
  const startExternalTransfer = vi.fn(async (_ids: number[], onSuccess: () => void) => {
    onSuccess();
  });
  const queryExternalSeriesStatus = vi.fn(async (ids: number[]) => {
    if (options.queryFails) throw new Error('external session gone');
    if (options.queryReturnsNothing) return [] as ExternalSeriesStatus[];
    return ids.map(statusOf);
  });
  const externalSeriesMap: Record<number, ExternalSeriesStatus> = {};
  CURRENT_PAGE.forEach((id) => {
    externalSeriesMap[id] = statusOf(id);
  });

  const view = renderHook(() =>
    useLibraryTransfer({
      externalSession: READY_SESSION,
      externalSeriesMap,
      allSeries: CURRENT_PAGE.map(cardOf),
      selectedSeries,
      startExternalTransfer,
      queryExternalSeriesStatus,
      clearSelection: vi.fn(),
      showError,
      showToast,
      t: (key: string) => key,
    }),
  );

  const requestTransfer = async () => {
    await act(async () => {
      await view.result.current.requestTransfer();
    });
  };

  return { view, requestTransfer, showToast, showError, queryExternalSeriesStatus, startExternalTransfer };
}

describe('useLibraryTransfer 的 requestTransfer', () => {
  it('在第 1 页勾选后翻到第 2 页发起转移：补查跨页选中项，算出缺失并打开确认框', async () => {
    const { view, requestTransfer, showToast, showError, queryExternalSeriesStatus } = setup([1, 2]);

    await requestTransfer();

    expect(showToast).not.toHaveBeenCalled();
    expect(showError).not.toHaveBeenCalled();
    expect(view.result.current.showTransferConfirmModal).toBe(true);
    expect(view.result.current.pendingTransferSummary).toEqual({ total: 5, matched: 0, missing: 5 });
    expect(queryExternalSeriesStatus).toHaveBeenCalledWith([1, 2]);
  });

  it('选中项全在当前页时不补查，摘要与确认框照旧', async () => {
    const { view, requestTransfer, showToast, queryExternalSeriesStatus } = setup([4]);

    await requestTransfer();

    expect(queryExternalSeriesStatus).not.toHaveBeenCalled();
    expect(showToast).not.toHaveBeenCalled();
    expect(view.result.current.showTransferConfirmModal).toBe(true);
    expect(view.result.current.pendingTransferSummary).toEqual({ total: 4, matched: 1, missing: 3 });
  });

  it('所选系列确实全部存在于外部库时仍提示已全部存在，不开确认框', async () => {
    const { view, requestTransfer, showToast } = setup([3, 5]);

    await requestTransfer();

    expect(showToast).toHaveBeenCalledWith('home.external.alreadyComplete', 'success');
    expect(view.result.current.showTransferConfirmModal).toBe(false);
    expect(view.result.current.pendingTransferSummary).toBeNull();
  });

  it('补查没能覆盖全部选中项时不下「已全部存在」的结论，仍打开确认框', async () => {
    const { view, requestTransfer, showToast } = setup([3, 5], { queryReturnsNothing: true });

    await requestTransfer();

    expect(showToast).not.toHaveBeenCalled();
    expect(view.result.current.showTransferConfirmModal).toBe(true);
  });

  it('补查失败时报错而不是报成功，确认框不开', async () => {
    const { view, requestTransfer, showToast, showError } = setup([1, 2], { queryFails: true });

    await requestTransfer();

    expect(showError).toHaveBeenCalledWith('home.external.statusUnavailable');
    expect(showToast).not.toHaveBeenCalled();
    expect(view.result.current.showTransferConfirmModal).toBe(false);
  });
});

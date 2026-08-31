import { useCallback, useState } from 'react';

import type { ExternalSeriesStatus, ExternalSession, Series } from '../types';

export interface TransferSummary {
  total: number;
  matched: number;
  missing: number;
}

interface UseLibraryTransferParams {
  externalSession: ExternalSession | null;
  externalSeriesMap: Record<number, ExternalSeriesStatus>;
  allSeries: Series[];
  selectedSeries: number[];
  /** 按 id 补查外部库覆盖；问不到时必须抛错，不能拿空结果冒充「外部库里没有」。 */
  queryExternalSeriesStatus: (ids: number[]) => Promise<ExternalSeriesStatus[]>;
  startExternalTransfer: (seriesIds: number[], onSuccess: () => void) => Promise<void>;
  clearSelection: () => void;
  showError: (key: string) => void;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useLibraryTransfer({
  externalSession,
  externalSeriesMap,
  allSeries,
  selectedSeries,
  queryExternalSeriesStatus,
  startExternalTransfer,
  clearSelection,
  showError,
  showToast,
  t,
}: UseLibraryTransferParams) {
  const [showTransferConfirmModal, setShowTransferConfirmModal] = useState(false);
  const [pendingTransferSummary, setPendingTransferSummary] = useState<TransferSummary | null>(null);

  const closeTransferModal = useCallback(() => {
    setShowTransferConfirmModal(false);
    setPendingTransferSummary(null);
  }, []);

  const requestTransfer = useCallback(async () => {
    if (!externalSession?.session_id) {
      showError('home.external.scanFirst');
      return;
    }
    if (externalSession.status !== 'ready') {
      showError('home.external.stillScanning');
      return;
    }
    // 选择是跨页保留的，externalSeriesMap 与 allSeries 却都只有当前页：不在覆盖表里的选中项
    // 单独补查，已在表里的不重问——翻一页不会变成整份选择集的全量重查。
    let coverage = externalSeriesMap;
    const unresolvedIds = selectedSeries.filter((seriesId) => !coverage[seriesId]);
    if (unresolvedIds.length > 0) {
      try {
        const fetched = await queryExternalSeriesStatus(unresolvedIds);
        coverage = { ...externalSeriesMap };
        fetched.forEach((item) => {
          coverage[item.series_id] = item;
        });
      } catch (err) {
        console.error('Failed to resolve external coverage for the selection', err);
        showError('home.external.statusUnavailable');
        return;
      }
    }
    const summary = selectedSeries.reduce<TransferSummary>(
      (acc, seriesId) => {
        const status = coverage[seriesId];
        const total = status?.external_total_count ?? allSeries.find((item) => item.id === seriesId)?.actual_book_count ?? 0;
        const matched = status?.external_match_count ?? 0;
        acc.total += total;
        acc.matched += matched;
        acc.missing += Math.max(0, total - matched);
        return acc;
      },
      { total: 0, matched: 0, missing: 0 },
    );
    // missing === 0 只有在每个选中系列的覆盖都问到了的前提下才等于「已全部存在」。
    // 状态未知时一律走确认框：宁可让用户确认一次，也不能因为信息不足就不执行操作还报成功。
    const allResolved = selectedSeries.every((seriesId) => Boolean(coverage[seriesId]));
    if (allResolved && summary.missing === 0) {
      showToast(t('home.external.alreadyComplete'), 'success');
      return;
    }
    setPendingTransferSummary(summary);
    setShowTransferConfirmModal(true);
  }, [
    allSeries,
    externalSession,
    externalSeriesMap,
    queryExternalSeriesStatus,
    selectedSeries,
    showError,
    showToast,
    t,
  ]);

  const confirmTransfer = useCallback(async () => {
    await startExternalTransfer(selectedSeries, () => {
      showToast(t('home.external.transferQueued'), 'success');
      closeTransferModal();
      clearSelection();
    });
  }, [closeTransferModal, clearSelection, selectedSeries, showToast, startExternalTransfer, t]);

  return {
    showTransferConfirmModal,
    pendingTransferSummary,
    requestTransfer,
    confirmTransfer,
    closeTransferModal,
  };
}

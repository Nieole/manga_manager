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

  const requestTransfer = useCallback(() => {
    if (!externalSession?.session_id) {
      showError('home.external.scanFirst');
      return;
    }
    if (externalSession.status !== 'ready') {
      showError('home.external.stillScanning');
      return;
    }
    const summary = selectedSeries.reduce<TransferSummary>(
      (acc, seriesId) => {
        const status = externalSeriesMap[seriesId];
        const total = status?.external_total_count ?? allSeries.find((item) => item.id === seriesId)?.actual_book_count ?? 0;
        const matched = status?.external_match_count ?? 0;
        acc.total += total;
        acc.matched += matched;
        acc.missing += Math.max(0, total - matched);
        return acc;
      },
      { total: 0, matched: 0, missing: 0 },
    );
    if (summary.missing === 0) {
      showToast(t('home.external.alreadyComplete'), 'success');
      return;
    }
    setPendingTransferSummary(summary);
    setShowTransferConfirmModal(true);
  }, [allSeries, externalSession, externalSeriesMap, selectedSeries, showError, showToast, t]);

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

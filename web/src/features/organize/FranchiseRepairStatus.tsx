import { useCallback, useEffect, useState } from 'react';
import { apiClient } from '../../api/client';
import { useLatestRequest } from '../../hooks/useLatestRequest';
import { useI18n } from '../../i18n/LocaleProvider';

interface FranchiseState {
  available: boolean;
  stale: boolean;
  status: 'current' | 'pending' | 'failed';
}

export function FranchiseRepairStatus({ isAdmin, refreshKey, onRepaired }: { isAdmin: boolean; refreshKey: number; onRepaired?: () => void }) {
  const { t } = useI18n();
  const request = useLatestRequest();
  const [state, setState] = useState<FranchiseState | null>(null);
  const [loadFailed, setLoadFailed] = useState(false);
  const [retryFailed, setRetryFailed] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const load = useCallback(() => request.run(() => apiClient.get<FranchiseState>('/api/franchise-state'), {
    onSuccess: response => {
      if (typeof response.data.available !== 'boolean') { setLoadFailed(true); return; }
      setState(response.data);
      setLoadFailed(false);
    },
    onError: () => setLoadFailed(true),
  }), [request]);
  useEffect(() => {
    void load();
    return () => request.invalidate();
  }, [load, request, refreshKey]);
  useEffect(() => {
    if (!state?.stale) return;
    const timer = window.setInterval(() => { void load(); }, 5000);
    return () => window.clearInterval(timer);
  }, [load, state?.stale]);

  const retry = async () => {
    setRetrying(true);
    setRetryFailed(false);
    try { await apiClient.post('/api/system/rebuild-franchises'); onRepaired?.(); }
    catch { setRetryFailed(true); }
    finally { await load(); setRetrying(false); }
  };
  if (!loadFailed && (!state || !state.stale)) return null;
  const text = loadFailed ? t('franchise.stateFailed')
    : !state?.available ? t(state?.status === 'failed' ? 'franchise.unavailableFailed' : 'franchise.unavailable')
      : state.status === 'failed' ? t('franchise.failedStale') : t('franchise.updatingStale');
  return <div role="status" className="mb-4 rounded-lg border border-amber-700 bg-amber-950/40 p-4 text-sm text-amber-100">
    <p>{text}</p>
    {retryFailed && <p>{t('franchise.retryFailed')}</p>}
    {loadFailed && <button type="button" onClick={() => { void load(); }} className="mt-2 underline">{t('franchise.refresh')}</button>}
    {isAdmin && state?.stale && <button type="button" disabled={retrying} onClick={() => { void retry(); }} className="mt-2 underline disabled:opacity-50">{t('franchise.retry')}</button>}
  </div>;
}

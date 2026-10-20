import { useEffect, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import type { RelocationDetail, RelocationDetails, RelocationPlan, RunLive, RunSnapshot } from '../../api/generated';
import { useI18n } from '../../i18n/LocaleProvider';
import { ModalShell } from '../ui/ModalShell';
import { modalGhostButtonClass, modalPrimaryButtonClass, modalSectionClass } from '../ui/modalStyles';
import { DirectoryPicker } from './DirectoryPicker';
import { useDirectoryBrowser } from './useDirectoryBrowser';

interface Props {
  library: { id: number; name: string; path: string };
  planID?: number;
  onClose: () => void;
}

export function RelocationModal({ library, planID, onClose }: Props) {
  const { t } = useI18n();
  const browser = useDirectoryBrowser();
  const [target, setTarget] = useState('');
  const [plan, setPlan] = useState<RelocationPlan | null>(null);
  const [run, setRun] = useState<RunSnapshot | null>(null);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [kind, setKind] = useState('blocker');
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<RelocationDetails | null>(null);
  const [pageError, setPageError] = useState(false);
  const [pageLoading, setPageLoading] = useState(false);
  const busy = plan?.status === 'pending' || plan?.status === 'checking';

  useEffect(() => {
    let active = true;
    const url = planID ? `/api/relocation-plans/${planID}` : `/api/libraries/${library.id}/relocation-plans/latest`;
    apiClient.get<RelocationPlan>(url).then(({ data }) => {
      if (active && data.library_id === library.id) { setPlan(data); setTarget(data.target_root); }
    }).catch((err: unknown) => {
      if (active && !(isAxiosError(err) && err.response?.status === 404)) setError('relocation.loadError');
    });
    return () => { active = false; };
  }, [library.id, planID]);

  useEffect(() => {
    if (!busy || !plan) return;
    let active = true;
    const id = plan.id;
    const runID = plan.run_id;
    const refresh = () => {
      apiClient.get<RelocationPlan>(`/api/relocation-plans/${id}`).then(({ data }) => {
        if (active) { setPlan(data); setError(''); }
      }).catch(() => { if (active) setError('relocation.loadError'); });
      apiClient.get<RunLive>('/api/system/tasks/live').then(({ data }) => {
        if (active) setRun(data.runs?.find((item) => item.run_id === runID) || null);
      }).catch(() => { /* Plan remains available when the live summary is temporarily unavailable. */ });
    };
    refresh();
    const timer = window.setInterval(refresh, 1000);
    return () => { active = false; window.clearInterval(timer); };
  }, [busy, plan?.id, plan?.run_id]);

  useEffect(() => {
    if (!plan) return;
    let active = true;
    setPageLoading(true);
    setPageError(false);
    apiClient.get<RelocationDetails>(`/api/relocation-plans/${plan.id}/details?kind=${kind}&offset=${offset}&limit=50`)
      .then(({ data }) => { if (active) setPage(data); })
      .catch(() => { if (active) { setPage(null); setPageError(true); } })
      .finally(() => { if (active) setPageLoading(false); });
    return () => { active = false; };
  }, [plan?.id, plan?.status, kind, offset]);

  const check = async () => {
    setSubmitting(true); setError(''); setCancelling(false);
    try {
      const { data } = await apiClient.post<RelocationPlan>(`/api/libraries/${library.id}/relocation-plans`, { target_root: target });
      setPlan(data); setOffset(0); setRun(null);
    } catch { setError('relocation.checkError'); }
    finally { setSubmitting(false); }
  };
  const cancel = async () => {
    if (!plan) return;
    setCancelling(true);
    try { await apiClient.post(`/api/system/runs/${plan.run_id}/cancel`); }
    catch { setError('relocation.cancelError'); setCancelling(false); }
  };
  const detailRow = (item: RelocationDetail) => (
    <li key={item.ordinal} className="space-y-1 border-b border-gray-800 py-3 text-sm last:border-0">
      {item.reason && <p className={item.kind === 'blocker' ? 'text-red-300' : 'text-amber-200'}>{t(`relocation.reason.${item.reason}`)}</p>}
      {item.source_path && <p className="break-all text-gray-400">{item.source_path}</p>}
      {item.target_path && <p className="break-all text-gray-100">{item.target_path}</p>}
      {item.run_id ? <p>{t('relocation.work', { id: item.run_id, status: t(`logs.task.status.${item.run_status}`) })}</p> : null}
      {item.reason === 'size_mismatch' && <p>{t('relocation.sizes', { expected: item.expected_size, actual: item.actual_size })}</p>}
    </li>
  );

  return <ModalShell open onClose={onClose} title={t('relocation.title')} description={t('relocation.description')} size="standard"
    footer={<><button type="button" className={modalGhostButtonClass} onClick={onClose}>{t('relocation.close')}</button>
      {busy ? <button type="button" className={modalGhostButtonClass} disabled={cancelling || run?.can_cancel === false} onClick={cancel}>{t(cancelling ? 'relocation.cancelling' : 'relocation.cancelCheck')}</button>
        : <button type="button" className={modalPrimaryButtonClass} disabled={!target || submitting} onClick={check}>{t('relocation.check')}</button>}</>}>
    <div className="space-y-5 text-gray-300">
      <div className={modalSectionClass}><p className="font-medium text-white">{library.name}</p>
        <p className="mt-3 text-xs text-gray-400">{t('relocation.source')}</p><p className="break-all text-sm">{plan?.source_root || library.path}</p>
        <p className="mt-3 text-xs text-gray-400">{t('relocation.target')}</p>
        <fieldset disabled={busy || submitting} className="mt-2"><DirectoryPicker value={target} onChange={setTarget} recentPaths={[]}
          browsing={browser.browsing} browseCurrent={browser.browseCurrent} browseParent={browser.browseParent} browseDirs={browser.browseDirs} browseDrives={browser.browseDrives}
          onOpen={browser.openDirectoryBrowser} onClose={() => browser.setBrowsing(false)} onNavigate={browser.navigateDirectoryBrowser}
          onChooseCurrent={() => { setTarget(browser.browseCurrent); browser.setBrowsing(false); }} /></fieldset>
        <p className="mt-3 text-xs text-gray-400">{t('relocation.scopeHint')}</p>
      </div>
      {error && <p role="alert" className="text-red-300">{t(error)}</p>}
      {plan && <>
        <section className={modalSectionClass} aria-live="polite">
          <p className="font-medium text-white">{t(`relocation.status.${plan.status}`)}</p>
          <p className="mt-2 text-sm">{t('relocation.counts', { series: plan.series_count, books: plan.book_count, policies: plan.policy_count })}</p>
          {run && <p className="mt-2 text-sm">{t(`settings.maintenance.taskPhase.${run.phase || 'roots'}`)} · {run.current}/{run.total}</p>}
          {plan.status === 'checked' && <p className="mt-2 text-sm text-amber-200">{t('relocation.checkedHint')}</p>}
          <a className="mt-3 inline-block text-sm text-komgaPrimary hover:underline" href="/settings/tasks">{t('relocation.taskCenter')}</a>
        </section>
        {plan.samples.length > 0 && <section className={modalSectionClass}><h3>{t('relocation.examples')}</h3><ul>{plan.samples.map(detailRow)}</ul></section>}
        <section className={modalSectionClass}>
          <div className="flex flex-wrap gap-2" role="tablist">{[['blocker', 'blockers', plan.blocker_count], ['anomaly', 'anomalies', plan.anomaly_count], ['policy', 'policies', plan.policy_count], ['mapping', 'mappings', plan.series_count + plan.book_count]].map(([value, label, count]) =>
            <button type="button" role="tab" aria-selected={kind === value} key={value} className={`rounded-lg border px-3 py-2 text-sm ${kind === value ? 'border-komgaPrimary text-white' : 'border-gray-700 text-gray-400'}`}
              onClick={() => { setKind(String(value)); setOffset(0); }}><span>{t(`relocation.${label}`)}</span> <span>({count})</span></button>)}</div>
          {kind === 'anomaly' && <p className="mt-3 text-xs text-gray-400">{t('relocation.sizeHint')}</p>}
          {pageLoading ? <p className="py-4">{t('common.loading')}</p> : pageError ? <p role="alert" className="py-4 text-red-300">{t('relocation.detailsError')}</p>
            : <ul className="max-h-72 overflow-y-auto">{page?.items.map(detailRow)}</ul>}
          {!pageLoading && !pageError && page?.total === 0 && <p className="py-4 text-sm text-gray-500">{t('relocation.empty')}</p>}
          {page && page.total > 50 && <div className="mt-3 flex items-center justify-between gap-3 text-sm">
            <button type="button" disabled={offset === 0 || pageLoading} onClick={() => setOffset(Math.max(0, offset - 50))}>{t('relocation.previous')}</button>
            <span>{offset + 1}–{Math.min(offset + 50, page.total)} / {page.total}</span>
            <button type="button" disabled={offset + 50 >= page.total || pageLoading} onClick={() => setOffset(offset + 50)}>{t('relocation.next')}</button>
          </div>}
        </section>
      </>}
    </div>
  </ModalShell>;
}

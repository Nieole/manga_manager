import { useEffect, useRef, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import type { RelocationOperation, RelocationDetail, RelocationDetails, RelocationPlan, RelocationPolicy, RunLive, RunSnapshot } from '../../api/generated';
import { useI18n } from '../../i18n/LocaleProvider';
import { ModalShell } from '../ui/ModalShell';
import { modalGhostButtonClass, modalPrimaryButtonClass, modalSectionClass } from '../ui/modalStyles';
import { DirectoryPicker } from './DirectoryPicker';
import { useDirectoryBrowser } from './useDirectoryBrowser';

interface Props {
  library: { id: number; name: string; path: string };
  planID?: number;
  initialTarget?: string;
  onCompleted?: () => void;
  onClose: () => void;
}

function PolicyPreview({ policy, title, description }: { policy: RelocationPolicy; title: string; description: string }) {
  const { t } = useI18n();
  const io = policy.io_policy;
  const values = [
    ['profile', t(`settings.library.storageProfile.${policy.storage_profile}`, undefined, policy.storage_profile)],
    ['scanConcurrency', io.scan_concurrency],
    ['archiveOpenConcurrency', io.archive_open_concurrency],
    ['coverConcurrency', io.cover_concurrency],
    ['hashConcurrency', io.hash_concurrency],
    ['pauseWhenReading', t(io.pause_background_when_reading ? 'relocation.policy.enabled' : 'relocation.policy.disabled')],
    ['idleOnlyHeavyTasks', t(io.idle_only_heavy_tasks ? 'relocation.policy.enabled' : 'relocation.policy.disabled')],
    ['disableSameDiskPageCache', t(io.disable_same_disk_page_cache ? 'relocation.policy.enabled' : 'relocation.policy.disabled')],
  ];
  return <section aria-label={t(title)} className="rounded-lg border border-gray-700 bg-gray-950/40 p-3">
    <h4 className="font-medium text-white">{t(title)}</h4>
    <p className="mt-1 text-xs text-gray-400">{t(description)}</p>
    <dl className="mt-3 space-y-2 text-xs">
      {values.map(([label, value]) => <div key={label} className="flex justify-between gap-3">
        <dt className="text-gray-400">{t(`relocation.policy.${label}`)}</dt>
        <dd className="shrink-0 text-gray-100">{value}</dd>
      </div>)}
    </dl>
  </section>;
}

export function RelocationModal({ library, planID, initialTarget, onCompleted, onClose }: Props) {
  const { t } = useI18n();
  const browser = useDirectoryBrowser();
  const [target, setTarget] = useState(initialTarget || '');
  const [plan, setPlan] = useState<RelocationPlan | null>(null);
  const [needsRecheck, setNeedsRecheck] = useState(false);
  const completedCallback = useRef(onCompleted);
  useEffect(() => { completedCallback.current = onCompleted; }, [onCompleted]);
  const [operation, setOperation] = useState<RelocationOperation | null>(null);
  const [commitRun, setCommitRun] = useState<RunSnapshot | null>(null);
  const [run, setRun] = useState<RunSnapshot | null>(null);
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [kind, setKind] = useState('blocker');
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<RelocationDetails | null>(null);
  const [pageError, setPageError] = useState(false);
  const [pageLoading, setPageLoading] = useState(false);
  const [detailsRetry, setDetailsRetry] = useState(0);
  const [loading, setLoading] = useState(true);
  const id = plan?.id;
  const runID = plan?.run_id;
  const status = plan?.status;
  const committing = operation?.status === 'pending' || operation?.status === 'paths_committed';
  const canCommit = plan?.status === 'checked' && !plan.blocker_count && !plan.anomaly_count && !plan.policy_count && !operation && !plan?.operation_id && !needsRecheck;
  const busy = plan?.status === 'pending' || plan?.status === 'checking';

  useEffect(() => {
    let active = true;
    const url = planID ? `/api/relocation-plans/${planID}` : `/api/libraries/${library.id}/relocation-plans/latest`;
    apiClient.get<RelocationPlan>(url).then(({ data }) => {
      if (!active) return;
      if (data.library_id !== library.id) { setError('relocation.loadError'); return; }
      setPlan(data); if (!initialTarget) setTarget(data.target_root);
    }).catch((err: unknown) => {
      if (active && (planID || !(isAxiosError(err) && err.response?.status === 404))) setError('relocation.loadError');
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [library.id, planID, initialTarget]);

  useEffect(() => {
    if (!busy || !id) return;
    let active = true;
    let timer: number | undefined;
    const refresh = async () => {
      await Promise.all([
        apiClient.get<RelocationPlan>(`/api/relocation-plans/${id}`).then(({ data }) => {
          if (active) { setPlan(data); setError(''); }
        }).catch(() => { if (active) setError('relocation.loadError'); }),
        apiClient.get<RunLive>('/api/system/tasks/live').then(({ data }) => {
          if (active) setRun(data.runs?.find((item) => item.run_id === runID) || null);
        }).catch(() => { /* A failed live request does not hide the persisted preview. */ }),
      ]);
      if (active) timer = window.setTimeout(refresh, 1000);
    };
    void refresh();
    return () => { active = false; window.clearTimeout(timer); };
  }, [busy, id, runID]);

  useEffect(() => {
    if (!id) return;
    let active = true;
    setPageLoading(true);
    setPageError(false);
    apiClient.get<RelocationDetails>(`/api/relocation-plans/${id}/details?kind=${kind}&offset=${offset}&limit=50`)
      .then(({ data }) => { if (active) setPage(data); })
      .catch(() => { if (active) setPageError(true); })
      .finally(() => { if (active) setPageLoading(false); });
    return () => { active = false; };
  }, [id, status, kind, offset, detailsRetry]);

  const operationID = operation?.id || plan?.operation_id;
  const operationStatus = operation?.status;
  useEffect(() => {
    if (!operationID || (operationStatus && operationStatus !== 'pending' && operationStatus !== 'paths_committed')) return;
    let active = true;
    let timer: number | undefined;
    const refresh = async () => {
      try {
        const { data } = await apiClient.get<RelocationOperation>(`/api/relocation-operations/${operationID}`);
        if (!active) return;
        setOperation(data);
        if (data.status === 'pending' || data.status === 'paths_committed') {
          try {
            const live = await apiClient.get<RunLive>('/api/system/tasks/live');
            if (active) setCommitRun(live.data.runs?.find((item) => item.run_id === data.run_id) || null);
          } catch { /* Persisted results remain available without live progress. */ }
          if (active) timer = window.setTimeout(refresh, 1000);
        }
        else if (data.status === 'completed') completedCallback.current?.();
      } catch { if (active) setError('relocation.operationLoadError'); }
    };
    void refresh();
    return () => { active = false; window.clearTimeout(timer); };
  }, [operationID, operationStatus]);

  const commit = async () => {
    if (!plan || target !== plan.target_root) return;
    setSubmitting(true); setError('');
    try {
      const { data } = await apiClient.post<RelocationOperation>(`/api/relocation-plans/${plan.id}/commit`, {});
      setOperation(data);
      if (data.status === 'completed') onCompleted?.();
    } catch (err: unknown) {
      const code = isAxiosError(err) ? err.response?.data?.code : '';
      setNeedsRecheck(Boolean(code));
      setError(code ? `relocation.commitError.${code}` : 'relocation.commitError.generic');
    } finally { setSubmitting(false); }
  };

  const check = async () => {
    setSubmitting(true); setError(''); setCancelling(false);
    try {
      const { data } = await apiClient.post<RelocationPlan>(`/api/libraries/${library.id}/relocation-plans`, { target_root: target });
      setPlan(data); setOperation(null); setNeedsRecheck(false); setOffset(0); setPage(null); setRun(null);
    } catch { setError('relocation.checkError'); }
    finally { setSubmitting(false); }
  };
  const cancel = async () => {
    if (!plan) return;
    setCancelling(true);
    try { await apiClient.post(`/api/system/runs/${plan.run_id}/cancel`); }
    catch { setError('relocation.cancelError'); setCancelling(false); }
  };
  const cancelCommit = async () => {
    if (!commitRun?.can_cancel) return;
    setCancelling(true);
    try { await apiClient.post(`/api/system/runs/${commitRun.run_id}/cancel`); }
    catch { setError('relocation.cancelError'); setCancelling(false); }
  };
  const detailRow = (item: RelocationDetail) => (
    <li key={item.ordinal} className="space-y-1 border-b border-gray-800 py-3 text-sm last:border-0">
      {item.reason && <p className={item.kind === 'blocker' ? 'text-red-300' : 'text-amber-200'}>{t(item.kind === 'policy' ? `settings.library.storageProfile.${item.reason}` : `relocation.reason.${item.reason}`, undefined, item.reason)}</p>}
      {item.source_path && <p className="break-all text-gray-400">{item.source_path}</p>}
      {item.target_path && <p className="break-all text-gray-100">{item.target_path}</p>}
      {item.run_id ? <p>{t('relocation.work', { id: item.run_id, status: t(`logs.runStatus.${item.run_status}`) })}</p> : null}
      {item.conflict_record_id ? <p>{t('relocation.conflictingRecord', { kind: t(`relocation.record.${item.conflict_record_type}`), id: item.conflict_record_id })}</p> : null}
      {item.reason === 'size_mismatch' && <p>{t('relocation.sizes', { expected: item.expected_size, actual: item.actual_size })}</p>}
      {(item.policy_before || item.policy_after) && <div className="grid gap-3 pt-2 sm:grid-cols-2">
        {item.policy_before && <PolicyPreview policy={item.policy_before} title="relocation.policy.before"
          description={item.reason === 'policy_affects_other_library' ? 'relocation.policy.effectiveBefore' : item.reason === 'policy_conflict' ? 'relocation.policy.destinationExisting' : 'relocation.policy.sourceExplicit'} />}
        {item.policy_after && <PolicyPreview policy={item.policy_after} title="relocation.policy.after"
          description={item.reason === 'policy_affects_other_library' ? 'relocation.policy.effectiveAfter' : 'relocation.policy.destinationProposed'} />}
      </div>}
    </li>
  );

  return <ModalShell open onClose={onClose} title={t('relocation.title')} description={t('relocation.description')} size="standard"
    footer={<><button type="button" className={modalGhostButtonClass} onClick={onClose}>{t('relocation.close')}</button>
      {committing && commitRun?.can_cancel && <button type="button" className={modalGhostButtonClass} disabled={cancelling} onClick={cancelCommit}>{t(cancelling ? 'relocation.cancelling' : 'relocation.cancelCommit')}</button>}
      {busy ? <button type="button" className={modalGhostButtonClass} disabled={cancelling || run?.can_cancel === false} onClick={cancel}>{t(cancelling ? 'relocation.cancelling' : 'relocation.cancelCheck')}</button>
        : <>{!committing && <button type="button" className={modalGhostButtonClass} disabled={!target || submitting || loading} onClick={check}>{t('relocation.check')}</button>}{canCommit && <button type="button" className={modalPrimaryButtonClass} disabled={target !== plan.target_root || submitting || loading} onClick={commit}>{t('relocation.commit')}</button>}</>}</>}>
    <div className="space-y-5 text-gray-300">
      {loading && <p role="status">{t('common.loading')}</p>}
      <div className={modalSectionClass}><p className="font-medium text-white">{library.name}</p>
        <p className="mt-3 text-xs text-gray-400">{t('relocation.source')}</p><p className="break-all text-sm">{plan?.source_root || library.path}</p>
        <p className="mt-3 text-xs text-gray-400">{t('relocation.target')}</p>
        <fieldset disabled={busy || committing || submitting || loading} className="mt-2"><DirectoryPicker value={target} onChange={setTarget} recentPaths={[]}
          browsing={browser.browsing} browseCurrent={browser.browseCurrent} browseParent={browser.browseParent} browseDirs={browser.browseDirs} browseDrives={browser.browseDrives}
          onOpen={browser.openDirectoryBrowser} onClose={() => browser.setBrowsing(false)} onNavigate={browser.navigateDirectoryBrowser}
          onChooseCurrent={() => { setTarget(browser.browseCurrent); browser.setBrowsing(false); }} /></fieldset>
        <p className="mt-3 text-xs text-gray-400">{t('relocation.scopeHint')}</p>
      </div>
      {error && <p role="alert" className="text-red-300">{t(error, undefined, t('relocation.commitError.generic'))}</p>}
      {plan && <>
        <section className={modalSectionClass} aria-live="polite">
          <p className="font-medium text-white">{t(`relocation.status.${plan.status}`)}</p>
          <p className="mt-2 text-sm">{t('relocation.counts', { series: plan.series_count, books: plan.book_count, policies: plan.policy_count })}</p>
          {run && <p className="mt-2 text-sm">{t(`settings.maintenance.taskPhase.${run.phase || 'roots'}`)} · {run.current}/{run.total}</p>}
          {plan.status === 'checked' && !operation && <p className="mt-2 text-sm text-amber-200">{t('relocation.checkedHint')}</p>}
          <a className="mt-3 inline-block text-sm text-komgaPrimary hover:underline" href="/ops?tab=tasks">{t('relocation.taskCenter')}</a>
        </section>
        {operation && <section className={modalSectionClass} aria-live="polite">
          <p className="font-medium text-white">{t(`relocation.operation.${operation.status}`)}</p>
          <p className="mt-2 text-sm">{t('relocation.operationCounts', { series: operation.series_count, books: operation.book_count })}</p>
          {commitRun && committing && <p className="mt-2 text-sm">{t(`settings.maintenance.taskPhase.${commitRun.phase || 'revalidate'}`)} · {commitRun.current}/{commitRun.total}</p>}
          {operation.code && <p className="mt-2 text-amber-200">{t(`relocation.commitError.${operation.code}`, undefined, t('relocation.commitError.generic'))}</p>}
          {committing && !commitRun?.can_cancel && <p className="mt-2 text-sm">{t('relocation.noCancelCommit')}</p>}
          {operation.status === 'completed' && <p className="mt-2 text-sm">{t('relocation.completedHint')}</p>}
        </section>}
        {plan.samples.length > 0 && <section className={modalSectionClass}><h3>{t('relocation.examples')}</h3><ul>{plan.samples.map(detailRow)}</ul></section>}
        <section className={modalSectionClass}>
          <div className="flex flex-wrap gap-2" role="tablist">{[['blocker', 'blockers', plan.blocker_count], ['anomaly', 'anomalies', plan.anomaly_count], ['policy', 'policies', plan.policy_count], ['mapping', 'mappings', plan.series_count + plan.book_count]].map(([value, label, count]) =>
            <button type="button" role="tab" aria-selected={kind === value} key={value} className={`rounded-lg border px-3 py-2 text-sm ${kind === value ? 'border-komgaPrimary text-white' : 'border-gray-700 text-gray-400'}`}
              onClick={() => { if (kind !== value) { setKind(String(value)); setOffset(0); setPage(null); } }}><span>{t(`relocation.${label}`)}</span> <span>({count})</span></button>)}</div>
          {kind === 'anomaly' && <p className="mt-3 text-xs text-gray-400">{t('relocation.sizeHint')}</p>}
          {pageLoading ? <p className="py-4">{t('common.loading')}</p> : pageError ? <div className="py-4"><p role="alert" className="text-red-300">{t('relocation.detailsError')}</p><button type="button" className={modalGhostButtonClass} onClick={() => setDetailsRetry((value) => value + 1)}>{t('relocation.retryDetails')}</button></div>
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

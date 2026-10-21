import { useEffect, useRef, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import type { RelocationDetails, RelocationOperation, RelocationPlan, RelocationRestoration, RunLive, RunSnapshot } from '../../api/generated';
import { useI18n } from '../../i18n/LocaleProvider';
import { ModalShell } from '../ui/ModalShell';
import { modalGhostButtonClass, modalPrimaryButtonClass, modalSectionClass } from '../ui/modalStyles';

interface Props {
  library: { id: number; name: string; path: string };
  planID?: number;
  onCompleted?: () => void;
  onClose: () => void;
}

export function RestorationModal({ library, planID, onClose, onCompleted }: Props) {
  const { t } = useI18n();
  const [facts, setFacts] = useState<RelocationRestoration | null>(null);
  const [plan, setPlan] = useState<RelocationPlan | null>(null);
  const [mode, setMode] = useState('none');
  const [interval, setInterval] = useState(60);
  const [formats, setFormats] = useState('');
  const [consent, setConsent] = useState(false);
  const [lossRisk, setLossRisk] = useState(false);
  const [operation, setOperation] = useState<RelocationOperation | null>(null);
  const [run, setRun] = useState<RunSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);
  const [error, setError] = useState('');
  const [stale, setStale] = useState(false);
  const [history, setHistory] = useState(false);
  const [kind, setKind] = useState('anomaly');
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<RelocationDetails | null>(null);
  const [pageError, setPageError] = useState(false);
  const [pageLoading, setPageLoading] = useState(false);
  const [retry, setRetry] = useState(0);
  const generation = useRef(0);
  const completed = useRef(onCompleted);
  useEffect(() => { completed.current = onCompleted; }, [onCompleted]);
  const resetConsent = () => { setConsent(false); setLossRisk(false); };
  const base = `/api/libraries/${library.id}/relocation-restoration`;
  const busy = plan?.status === 'pending' || plan?.status === 'checking';
  const recovering = operation?.status === 'pending' || operation?.status === 'prepared' || operation?.status === 'paths_committed';
  const available = Boolean(facts?.protection && plan?.purpose === 'restoration' && plan.protection_id === facts.protection.operation_id && plan.library_id === library.id && plan.library_root === library.path && plan.status === 'checked' && !plan.blocker_count && !plan.operation_id && !operation && !stale);
  const settingsValid = ['none', 'interval', 'watch'].includes(mode) && Number.isInteger(interval) && interval > 0;

  useEffect(() => {
    const token = ++generation.current;
    const valid = () => generation.current === token;
    setLoading(true); setFacts(null); setPlan(null); setOperation(null); setError(''); setStale(false); setConsent(false); setLossRisk(false); setRun(null); setPage(null); setOffset(0);
    const load = async () => {
      try {
        const { data } = await apiClient.get<RelocationRestoration>(base);
        if (!valid()) return;
        setFacts(data);
        if (data.protection) {
          setMode(data.protection.scan_mode); setInterval(data.protection.scan_interval); setFormats(data.protection.scan_formats);
        }
        const check = planID ? (await apiClient.get<RelocationPlan>(`/api/relocation-plans/${planID}`)).data : data.check;
        if (!valid()) return;
        if (check && (check.library_id !== library.id || check.purpose !== 'restoration')) { setError('restoration.invalidCheck'); return; }
        setPlan(check || null);
      } catch { if (valid()) setError('restoration.loadError'); }
      finally { if (valid()) setLoading(false); }
    };
    void load();
    return () => { generation.current = token + 1; };
  }, [base, library.id, planID]);

  const checkID = plan?.id;
  useEffect(() => {
    if (!busy || !checkID) return;
    let active = true;
    let timer: number | undefined;
    const refresh = async () => {
      try {
        const { data } = await apiClient.get<RelocationPlan>(`/api/relocation-plans/${checkID}`);
        if (active) {
          if (data.library_id !== library.id || data.purpose !== 'restoration') { setStale(true); setError('restoration.invalidCheck'); return; }
          setPlan(data);
          if (data.status !== 'pending' && data.status !== 'checking') setCancelling(false);
        }
        const live = await apiClient.get<RunLive>('/api/system/tasks/live');
        if (active) setRun(live.data.runs?.find((item) => item.run_id === data.run_id) || null);
      } catch { if (active) setError('restoration.loadError'); }
      if (active) timer = window.setTimeout(refresh, 1000);
    };
    void refresh();
    return () => { active = false; window.clearTimeout(timer); };
  }, [busy, checkID, library.id]);

  const operationID = operation?.id || plan?.operation_id;
  const operationStatus = operation?.status;
  useEffect(() => {
    if (!operationID || (operationStatus && !['pending', 'prepared', 'paths_committed'].includes(operationStatus))) return;
    let active = true;
    let timer: number | undefined;
    const refresh = async () => {
      try {
        const { data } = await apiClient.get<RelocationOperation>(`/api/relocation-operations/${operationID}`);
        if (!active) return;
        if (data.purpose !== 'restoration' || data.library_id !== library.id) { setError('restoration.invalidCheck'); setStale(true); return; }
        setOperation(data);
        if (['pending', 'prepared', 'paths_committed'].includes(data.status)) timer = window.setTimeout(refresh, 1000);
        else if (data.status === 'completed') completed.current?.();
      } catch { if (active) setError('restoration.operationLoadError'); }
    };
    void refresh();
    return () => { active = false; window.clearTimeout(timer); };
  }, [operationID, operationStatus, library.id]);

  const detailPlan = history ? facts?.accepted : plan;
  const detailID = detailPlan?.id;
  const detailStatus = detailPlan?.status;
  useEffect(() => {
    if (!detailID) return;
    let active = true;
    setPage(null); setPageError(false); setPageLoading(true);
    apiClient.get<RelocationDetails>(`/api/relocation-plans/${detailID}/details?kind=${kind}&offset=${offset}&limit=50`)
      .then(({ data }) => { if (active) setPage(data); })
      .catch(() => { if (active) setPageError(true); })
      .finally(() => { if (active) setPageLoading(false); });
    return () => { active = false; };
  }, [detailID, detailStatus, history, kind, offset, retry]);

  const recheck = async () => {
    const token = generation.current;
    resetConsent(); setSubmitting(true); setError(''); setStale(true); setOperation(null); setPlan(null); setHistory(false); setOffset(0); setRun(null); setCancelling(false);
    try {
      const { data } = await apiClient.post<RelocationPlan>(`${base}/checks`, {});
      if (generation.current !== token) return;
      if (data.library_id !== library.id || data.purpose !== 'restoration') { setError('restoration.invalidCheck'); return; }
      const current = (await apiClient.get<RelocationRestoration>(base)).data;
      if (generation.current !== token) return;
      if (current.protection && current.protection.operation_id !== facts?.protection?.operation_id) {
        setMode(current.protection.scan_mode); setInterval(current.protection.scan_interval); setFormats(current.protection.scan_formats);
      }
      setFacts(current); setPlan(data); setStale(false);
    } catch { if (generation.current === token) setError('restoration.checkError'); }
    finally { if (generation.current === token) setSubmitting(false); }
  };
  const release = async () => {
    if (!available || !plan || !consent || !settingsValid || submitting || (plan.anomaly_count > 0 && !lossRisk)) return;
    const token = generation.current;
    setSubmitting(true); setError('');
    try {
      const { data } = await apiClient.post<RelocationOperation>(`${base}/release`, {
        plan_id: plan.id, confirm_release: true, accept_loss_risk: plan.anomaly_count > 0 && lossRisk,
        scan_mode: mode, scan_interval: interval, scan_formats: formats,
      });
      if (generation.current !== token) return;
      resetConsent(); setOperation(data);
      if (data.status === 'completed') completed.current?.();
    } catch (err) {
      if (generation.current !== token) return;
      const code = isAxiosError(err) ? (err.response?.data as { code?: string } | undefined)?.code : undefined;
      resetConsent(); setStale(true); setError(code ? `restoration.error.${code}` : 'restoration.releaseError');
    } finally { if (generation.current === token) setSubmitting(false); }
  };
  const cancel = async () => {
    if (!plan?.run_id || run?.can_cancel === false) return;
    const token = generation.current;
    setCancelling(true);
    try { await apiClient.post(`/api/system/runs/${plan.run_id}/cancel`); }
    catch { if (generation.current === token) { setError('relocation.cancelError'); setCancelling(false); } }
  };
  const control = 'mt-1 w-full rounded-lg border border-gray-700 bg-gray-950 px-3 py-2 text-sm text-white';
  return <ModalShell open onClose={onClose} title={t('restoration.title')} description={t('restoration.description')}
    footer={<><button type="button" className={modalGhostButtonClass} onClick={onClose}>{t('relocation.close')}</button>
      {busy ? <button type="button" className={modalGhostButtonClass} disabled={cancelling || run?.can_cancel === false} onClick={cancel}>{t(cancelling ? 'relocation.cancelling' : 'relocation.cancelCheck')}</button>
        : <button type="button" className={modalGhostButtonClass} disabled={loading || submitting || recovering || !facts?.protection || operation?.status === 'completed'} onClick={recheck}>{t('restoration.recheck')}</button>}
      <button type="button" className={modalPrimaryButtonClass} disabled={!available || !settingsValid || !consent || Boolean(plan?.anomaly_count && !lossRisk) || submitting || loading} onClick={release}>{t('restoration.release')}</button></>}>
    <div className="space-y-5 text-gray-300">
      <p className="font-medium text-white">{library.name}</p>
      {loading && <p role="status">{t('common.loading')}</p>}
      {error && <p role="alert" className="text-red-300">{t(error, undefined, t('restoration.releaseError'))}</p>}
      {!loading && !facts?.protection && !operation && <p>{t('restoration.notProtected')}</p>}
      {facts?.protection && <section className={modalSectionClass}>
        {operation?.status !== 'completed' && <p className="font-medium text-amber-200">{t('relocation.protection.active')}</p>}
        <p className="mt-2 text-sm">{t('restoration.originalSettings', { mode: t(`libraryForm.scanMode${facts.protection.scan_mode === 'watch' ? 'Watch' : facts.protection.scan_mode === 'interval' ? 'Interval' : 'None'}`), interval: facts.protection.scan_interval, formats: facts.protection.scan_formats })}</p>
        {facts.accepted && <p className="mt-2 text-sm">{t('restoration.historicalCounts', { count: facts.accepted.anomaly_count })}</p>}
      </section>}
      {plan && <section className={modalSectionClass} aria-live="polite">
        <h3 className="font-medium text-white">{t('restoration.currentCheck')}</h3>
        <p className="mt-2 text-sm">{t(`relocation.status.${plan.status}`)}</p>
        <p className="mt-2 text-sm">{t('restoration.currentCounts', { anomalies: plan.anomaly_count, blockers: plan.blocker_count })}</p>
        {busy && run && <p className="mt-2 text-sm">{t(`settings.maintenance.taskPhase.${run.phase || 'roots'}`)} · {run.current}/{run.total}</p>}
        <p className="mt-2 text-sm text-amber-200">{t(stale || (facts?.protection && plan.protection_id !== facts.protection.operation_id) ? 'restoration.stale' : 'restoration.checkOnly')}</p>
        <a href="/ops?tab=tasks" className="mt-2 inline-block text-sm text-komgaPrimary">{t('relocation.taskCenter')}</a>
      </section>}
      {operation && <section className={modalSectionClass} role="status"><p className="font-medium text-white">{t(operation.status === 'completed' ? 'restoration.completed' : operation.status === 'paths_committed' ? 'restoration.recovering' : `relocation.operation.${operation.status}`)}</p>{operation.code && <p className="mt-2 text-sm text-amber-200">{t(`restoration.error.${operation.code}`, undefined, t('restoration.releaseError'))}</p>}</section>}
      {(plan || facts?.accepted) && <section className={modalSectionClass}>
        <div className="flex gap-3"><button type="button" aria-pressed={!history} className={modalGhostButtonClass} onClick={() => { setHistory(false); setOffset(0); }}>{t('restoration.currentDetails')}</button>{facts?.accepted && <button type="button" aria-pressed={history} className={modalGhostButtonClass} onClick={() => { setHistory(true); setOffset(0); }}>{t('restoration.historicalDetails')}</button>}</div>
        <label className="mt-3 block text-sm">{t('restoration.detailKind')}<select className={control} value={kind} onChange={(event) => { setKind(event.target.value); setOffset(0); }}><option value="anomaly">{t('relocation.anomalies')}</option><option value="blocker">{t('relocation.blockers')}</option></select></label>
        {pageLoading ? <p className="mt-3">{t('common.loading')}</p> : pageError ? <div><p role="alert">{t('relocation.detailsError')}</p><button type="button" className={modalGhostButtonClass} onClick={() => setRetry((value) => value + 1)}>{t('relocation.retryDetails')}</button></div> : <ul className="max-h-64 overflow-y-auto">{page?.items.map((item) => <li key={item.ordinal} className="border-b border-gray-800 py-3 text-sm"><p className="text-amber-200">{t(`relocation.reason.${item.reason}`, undefined, item.reason)}</p><p className="break-all">{item.target_path || item.source_path}</p>{item.reason === 'size_mismatch' && <p>{t('relocation.sizes', { expected: item.expected_size, actual: item.actual_size })}</p>}</li>)}</ul>}
        {page?.total === 0 && <p className="mt-3 text-sm text-gray-500">{t('relocation.empty')}</p>}
        {page && page.total > 50 && <div className="mt-3 flex justify-between gap-3 text-sm"><button type="button" disabled={!offset || pageLoading} onClick={() => setOffset(Math.max(0, offset - 50))}>{t('relocation.previous')}</button><span>{offset + 1}–{Math.min(offset + 50, page.total)} / {page.total}</span><button type="button" disabled={offset + 50 >= page.total || pageLoading} onClick={() => setOffset(offset + 50)}>{t('relocation.next')}</button></div>}
      </section>}
      <fieldset className={`${modalSectionClass} space-y-3`} disabled={loading || busy || recovering || submitting || !facts?.protection || operation?.status === 'completed'}>
        <legend className="text-sm text-white">{t('restoration.selectedSettings')}</legend>
        <label className="block text-sm">{t('restoration.scanMode')}<select className={control} value={mode} onChange={(event) => { setMode(event.target.value); resetConsent(); }}>{['none', 'interval', 'watch'].map((value) => <option key={value} value={value}>{t(`libraryForm.scanMode${value === 'watch' ? 'Watch' : value === 'interval' ? 'Interval' : 'None'}`)}</option>)}</select></label>
        <label className="block text-sm">{t('restoration.scanInterval')}<input className={control} type="number" min="1" step="1" value={interval} onChange={(event) => { setInterval(Number(event.target.value)); resetConsent(); }} /></label>
        <label className="block text-sm">{t('restoration.scanFormats')}<input className={control} value={formats} onChange={(event) => { setFormats(event.target.value); resetConsent(); }} /></label>
        {Boolean(plan?.anomaly_count) && <section className="space-y-3 rounded-lg border border-amber-700 p-3"><p className="text-sm text-amber-200">{t('restoration.lossConsequences')}</p><label className="flex gap-3 text-sm"><input type="checkbox" checked={lossRisk} onChange={(event) => setLossRisk(event.target.checked)} />{t('restoration.acceptLossRisk')}</label></section>}
        <label className="flex gap-3 text-sm"><input type="checkbox" disabled={!available} checked={consent} onChange={(event) => setConsent(event.target.checked)} />{t('restoration.confirm')}</label>
      </fieldset>
    </div>
  </ModalShell>;
}

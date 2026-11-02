import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useLatestRequest, useRequestLifetime } from '../../session/useRequestLifetime';
import { apiClient } from '../../api/client';
import { getClientLocale, translateInLocale, useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { useKOReaderEditor } from '../settings-editor/SettingsEditorProvider';
import type { KOReaderStatus, KOReaderAccount, KOReaderAccountForm, KOReaderUnmatchedItem, KOReaderDeviceDiagnostics } from './types';

interface KOReaderContextValue {
  koreaderStatus: KOReaderStatus | null;
  koreaderAccounts: KOReaderAccount[];
  koreaderAccountForm: KOReaderAccountForm;
  setKOReaderAccountForm: React.Dispatch<React.SetStateAction<KOReaderAccountForm>>;
  unmatchedItems: KOReaderUnmatchedItem[];
  koreaderDevices: KOReaderDeviceDiagnostics | null;
  creatingAccount: boolean;
  accountActionId: number | null;
  applyingMatching: boolean;
  needsMatchingMaintenance: boolean;
  setNeedsMatchingMaintenance: React.Dispatch<React.SetStateAction<boolean>>;
  fetchKOReaderUnmatched: () => Promise<void>;
  fetchKOReaderDevices: () => Promise<void>;
  handleApplyMatchingChanges: () => Promise<void>;
  handleCreateKOReaderAccount: () => Promise<void>;
  handleCopySyncKey: (account: KOReaderAccount) => Promise<void>;
  handleRotateKOReaderAccount: (account: KOReaderAccount) => Promise<void>;
  handleToggleKOReaderAccount: (account: KOReaderAccount) => Promise<void>;
  handleDeleteKOReaderAccount: (account: KOReaderAccount) => Promise<void>;
  formatKOReaderLatestSync: (value?: { Time: string; Valid: boolean } | null) => string;
  formatKOReaderIndexLabel: (matchMode: string, pathIgnoreExtension: boolean) => string;
}

type EditingValue = Omit<ReturnType<typeof useKOReaderEditor>, 'saveKOReader'> & { saveKOReader: () => Promise<void> };
const EditingContext = createContext<EditingValue | null>(null);
const KOReaderContext = createContext<KOReaderContextValue | null>(null);

export function formatKOReaderLatestSync(value?: { Time: string; Valid: boolean } | null): string {
  const locale = getClientLocale();
  if (!value?.Valid || !value.Time) return translateInLocale(locale, 'settings.koreader.noSyncRecord');
  const date = new Date(value.Time);
  if (Number.isNaN(date.getTime())) return translateInLocale(locale, 'settings.koreader.noSyncRecord');
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date);
}

export function formatKOReaderIndexLabel(matchMode: string, pathIgnoreExtension: boolean): string {
  const locale = getClientLocale();
  if (matchMode === 'file_path') {
    return pathIgnoreExtension
      ? translateInLocale(locale, 'task.koreader.pathIndexIgnoreExtension')
      : translateInLocale(locale, 'task.koreader.pathIndex');
  }
  return translateInLocale(locale, 'task.koreader.binaryHashIndex');
}

export function KOReaderProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const { showToast } = useToast();
  const editor = useKOReaderEditor();
  const beginRequest = useRequestLifetime();
  const beginStatus = useLatestRequest();
  const beginAccounts = useLatestRequest();
  const beginUnmatched = useLatestRequest();
  const beginDevices = useLatestRequest();
  const [koreaderStatus, setKOReaderStatus] = useState<KOReaderStatus | null>(null);
  const [koreaderAccounts, setKOReaderAccounts] = useState<KOReaderAccount[]>([]);
  const [koreaderAccountForm, setKOReaderAccountForm] = useState<KOReaderAccountForm>({ username: '' });
  const [unmatchedItems, setUnmatchedItems] = useState<KOReaderUnmatchedItem[]>([]);
  const [koreaderDevices, setKOReaderDevices] = useState<KOReaderDeviceDiagnostics | null>(null);
  const [applyingMatching, setApplyingMatching] = useState(false);
  const [needsMatchingMaintenance, setNeedsMatchingMaintenance] = useState(false);
  const [creatingAccount, setCreatingAccount] = useState(false);
  const [accountActionId, setAccountActionId] = useState<number | null>(null);
  const fetchKOReader = useCallback(async () => {
    const current = beginStatus();
    try {
    const res = await apiClient.get<KOReaderStatus>('/api/system/koreader');
    if (current()) setKOReaderStatus(res.data);
    } catch (error) { if (current()) throw error; }
  }, [beginStatus]);

  const fetchKOReaderAccounts = useCallback(async () => {
    const current = beginAccounts();
    try {
    const res = await apiClient.get<KOReaderAccount[]>('/api/system/koreader/accounts');
    if (current()) setKOReaderAccounts(Array.isArray(res.data) ? res.data : []);
    } catch (error) { if (current()) throw error; }
  }, [beginAccounts]);

  const fetchKOReaderUnmatched = useCallback(async () => {
    const current = beginUnmatched();
    try {
    const res = await apiClient.get<KOReaderUnmatchedItem[]>('/api/system/koreader/unmatched?limit=12');
    if (current()) setUnmatchedItems(Array.isArray(res.data) ? res.data : []);
    } catch (error) { if (current()) throw error; }
  }, [beginUnmatched]);

  const fetchKOReaderDevices = useCallback(async () => {
    const current = beginDevices();
    try {
    const res = await apiClient.get<KOReaderDeviceDiagnostics>('/api/system/koreader/devices?limit=20');
    if (current()) setKOReaderDevices(res.data);
    } catch (error) { if (current()) throw error; }
  }, [beginDevices]);

  useEffect(() => {
    const alive = beginRequest();
    void Promise.all([fetchKOReader(), fetchKOReaderAccounts()]).catch((error) => { if (alive()) { console.error(error); showToast(t('settings.toast.fetchFailed'), 'error'); } });
  }, [beginRequest, fetchKOReader, fetchKOReaderAccounts, showToast, t]);

  const refreshAfterAction = useCallback(async (refresh: () => Promise<unknown>) => {
    const alive = beginRequest();
    try { await refresh(); }
    catch (error) { if (alive()) { console.error(error); showToast(t('settings.toast.configRefreshFailed'), 'error'); } }
  }, [beginRequest, showToast, t]);

  // Only an explicit successful discard invalidates published status, not draft edits.
  useEffect(() => {
    if (editor.koreaderReloadVersion === 0) return;
    void refreshAfterAction(fetchKOReader);
  }, [editor.koreaderReloadVersion, fetchKOReader, refreshAfterAction]);

  const saveKOReader = useCallback(async () => {
    const alive = beginRequest();
    const result = await editor.saveKOReader();
    if (!alive() || !result.committed) return;
    if (result.status) setKOReaderStatus(result.status);
    setNeedsMatchingMaintenance(result.requiresMaintenance);
    try { await Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderUnmatched(), fetchKOReaderDevices()]); }
    catch (error) { if (alive()) { console.error(error); showToast(t('settings.toast.configRefreshFailed'), 'error'); } }
  }, [beginRequest, editor, fetchKOReader, fetchKOReaderAccounts, fetchKOReaderUnmatched, fetchKOReaderDevices, showToast, t]);

  const handleApplyMatchingChanges = useCallback(async () => {
    const alive = beginRequest();
    setApplyingMatching(true);
    try {
      const res = await apiClient.post('/api/system/koreader/apply-matching');
      if (!alive()) return;
      showToast(res.data?.message || t('settings.toast.koreaderApplyMatchingStarted'), 'success');
      setNeedsMatchingMaintenance(false);
      await refreshAfterAction(fetchKOReader);
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('settings.toast.koreaderApplyMatchingFailed'), 'error');
    } finally {
      if (alive()) setApplyingMatching(false);
    }
  }, [beginRequest, refreshAfterAction, fetchKOReader, showToast, t]);

  const handleCreateKOReaderAccount = useCallback(async () => {
    const alive = beginRequest();
    if (!koreaderAccountForm.username.trim()) return;
    setCreatingAccount(true);
    try {
      const res = await apiClient.post<KOReaderAccount>('/api/system/koreader/accounts', {
        username: koreaderAccountForm.username.trim(),
      });
      if (!alive()) return;
      showToast(t('settings.toast.koreaderAccountCreated', { username: res.data.username }), 'success');
      setKOReaderAccountForm((current) => current.username === koreaderAccountForm.username ? { username: '' } : current);
      await refreshAfterAction(() => Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderDevices()]));
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('settings.toast.koreaderAccountCreateFailed'), 'error');
    } finally {
      if (alive()) setCreatingAccount(false);
    }
  }, [beginRequest, refreshAfterAction, fetchKOReader, fetchKOReaderAccounts, fetchKOReaderDevices, koreaderAccountForm.username, showToast, t]);

  const handleCopySyncKey = useCallback(async (account: KOReaderAccount) => {
    const alive = beginRequest();
    try {
      await navigator.clipboard.writeText(account.sync_key);
      if (!alive()) return;
      showToast(t('settings.toast.koreaderSyncKeyCopied', { username: account.username }), 'success');
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('settings.toast.koreaderSyncKeyCopyFailed'), 'error');
    }
  }, [beginRequest, showToast, t]);

  const handleRotateKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    const alive = beginRequest();
    setAccountActionId(account.id);
    try {
      await apiClient.post(`/api/system/koreader/accounts/${account.id}/rotate-key`);
      if (!alive()) return;
      showToast(t('settings.toast.koreaderSyncKeyRotated', { username: account.username }), 'success');
      await refreshAfterAction(() => Promise.all([fetchKOReaderAccounts(), fetchKOReaderDevices()]));
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('settings.toast.koreaderSyncKeyRotateFailed'), 'error');
    } finally {
      if (alive()) setAccountActionId((current) => current === account.id ? null : current);
    }
  }, [beginRequest, refreshAfterAction, fetchKOReaderAccounts, fetchKOReaderDevices, showToast, t]);

  const handleToggleKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    const alive = beginRequest();
    setAccountActionId(account.id);
    try {
      await apiClient.post(`/api/system/koreader/accounts/${account.id}/toggle`, {
        enabled: !account.enabled,
      });
      if (!alive()) return;
      showToast(
        t('settings.toast.koreaderAccountToggled', {
          username: account.username,
          action: account.enabled ? t('settings.toast.koreaderAccountDisabled') : t('settings.toast.koreaderAccountEnabled'),
        }),
        'success',
      );
      await refreshAfterAction(() => Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderDevices()]));
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('settings.toast.koreaderAccountToggleFailed'), 'error');
    } finally {
      if (alive()) setAccountActionId((current) => current === account.id ? null : current);
    }
  }, [beginRequest, refreshAfterAction, fetchKOReader, fetchKOReaderAccounts, fetchKOReaderDevices, showToast, t]);

  const handleDeleteKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    const alive = beginRequest();
    setAccountActionId(account.id);
    try {
      await apiClient.delete(`/api/system/koreader/accounts/${account.id}`);
      if (!alive()) return;
      showToast(t('settings.toast.koreaderAccountDeleted', { username: account.username }), 'success');
      await refreshAfterAction(() => Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderDevices()]));
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(t('settings.toast.koreaderAccountDeleteFailed'), 'error');
    } finally {
      if (alive()) setAccountActionId((current) => current === account.id ? null : current);
    }
  }, [beginRequest, refreshAfterAction, fetchKOReader, fetchKOReaderAccounts, fetchKOReaderDevices, showToast, t]);

  const koreaderValue = useMemo<KOReaderContextValue>(
    () => ({
      koreaderStatus,
      koreaderAccounts,
      koreaderAccountForm,
      setKOReaderAccountForm,
      unmatchedItems,
      koreaderDevices,
      creatingAccount,
      accountActionId,
      applyingMatching,
      needsMatchingMaintenance,
      setNeedsMatchingMaintenance,
      fetchKOReaderUnmatched,
      fetchKOReaderDevices,
      handleApplyMatchingChanges,
      handleCreateKOReaderAccount,
      handleCopySyncKey,
      handleRotateKOReaderAccount,
      handleToggleKOReaderAccount,
      handleDeleteKOReaderAccount,
      formatKOReaderLatestSync,
      formatKOReaderIndexLabel,
    }),
    [
      accountActionId,
      applyingMatching,
      creatingAccount,
      fetchKOReaderDevices,
      fetchKOReaderUnmatched,
      handleApplyMatchingChanges,
      handleCopySyncKey,
      handleCreateKOReaderAccount,
      handleDeleteKOReaderAccount,
      handleRotateKOReaderAccount,
      handleToggleKOReaderAccount,
      koreaderAccountForm,
      koreaderAccounts,
      koreaderDevices,
      koreaderStatus,
      needsMatchingMaintenance,
      unmatchedItems,
    ],
  );

  const editingValue = useMemo(() => ({ ...editor, saveKOReader }), [editor, saveKOReader]);
  return <KOReaderContext.Provider value={koreaderValue}><EditingContext.Provider value={editingValue}>{children}</EditingContext.Provider></KOReaderContext.Provider>;
}

export function useKOReaderDiagnostics() {
  const context = useContext(KOReaderContext);
  if (!context) throw new Error('useKOReader must be used within KOReaderProvider');
  return context;
}

export function useKOReaderEditing() {
  const value = useContext(EditingContext);
  if (!value) throw new Error('useKOReaderEditing must be used within KOReaderProvider');
  return value;
}

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { useRequestLifetime } from '../../session/useRequestLifetime';
import type { FieldEffect, Publication, ValidationResult } from '../../api/generated';
import type { Capabilities, Config, ConfigEnvelope, ConfigSaveResponse } from './types';
import { applySectionSnapshot, type ConfigSectionKey, type EditorSectionKey } from './sections';
import type { KOReaderForm, KOReaderStatus } from '../koreader-settings/types';
import { EditSession } from './EditSession';

export type SettingsSectionKey = EditorSectionKey | 'overview' | 'appearance' | 'tags' | 'users' | 'maintenance';

interface EditorValue {
  config: Config | null;
  setConfig: React.Dispatch<React.SetStateAction<Config | null>>;
  validation: ValidationResult;
  capabilities: Capabilities | null;
  fieldEffects: FieldEffect[];
  configPublication: Publication | null;
  lastPublication: Publication | null;
  loading: boolean;
  saving: boolean;
  isSectionSaving: (section: EditorSectionKey) => boolean;
  fieldErrors: (field: string) => string[];
  saveConfig: (section: ConfigSectionKey, successMessage?: string) => Promise<void>;
  saveProtocols: (protocol: 'opds' | 'mihon', enabled: boolean) => Promise<{ committed: boolean }>;
  hasSectionChanges: (section: SettingsSectionKey) => boolean;
  hasSectionConflict: (section: EditorSectionKey) => boolean;
  discardSection: (section: EditorSectionKey) => Promise<void>;
  showToast: (text: string, type?: 'success' | 'error') => void;
  koreaderReloadVersion: number;
  koreaderForm: KOReaderForm | null;
  setKOReaderForm: React.Dispatch<React.SetStateAction<KOReaderForm | null>>;
  koreaderValidation: ValidationResult;
  koreaderFieldErrors: (field: string) => string[];
  saveKOReader: () => Promise<{ committed: boolean; requiresMaintenance: boolean; status?: KOReaderStatus }>;
}

const EditorContext = createContext<EditorValue | null>(null);
const validResult: ValidationResult = { valid: true, issues: [] };

export function SettingsEditorProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const { showToast } = useToast();
  const beginRequest = useRequestLifetime();
  const reads = useRef(0);
  const [session] = useState(() => new EditSession());
  const [config, publishConfig] = useState<Config | null>(null);
  const [validation, setValidation] = useState<ValidationResult>(validResult);
  const [koreaderValidation, setKOReaderValidation] = useState<ValidationResult>(validResult);
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null);
  const [fieldEffects, setFieldEffects] = useState<FieldEffect[]>([]);
  const [configPublication, setConfigPublication] = useState<Publication | null>(null);
  const [lastPublication, setLastPublication] = useState<Publication | null>(null);
  const [koreaderReloadVersion, setKOReaderReloadVersion] = useState(0);
  const [loading, setLoading] = useState(true);
  const pending = useRef(new Set<EditorSectionKey>());
  const [savingSections, setSavingSections] = useState<ReadonlySet<EditorSectionKey>>(new Set());
  const markSaving = useCallback((section: EditorSectionKey, saving: boolean) => {
    if (saving) pending.current.add(section); else pending.current.delete(section);
    setSavingSections(new Set(pending.current));
  }, []);

  const setConfig = useCallback<EditorValue['setConfig']>((update) => {
    session.edit(typeof update === 'function' ? update(session.draft) : update);
    publishConfig(session.draft);
  }, [session]);

  const fetchConfig = useCallback(async () => {
    const requestIsCurrent = beginRequest();
    const request = ++reads.current;
    const res = await apiClient.get<ConfigEnvelope>('/api/system/config');
    if (!requestIsCurrent() || request !== reads.current) return false;
    session.receive(res.data.config, res.headers.etag || res.data.publication?.version || '');
    publishConfig(session.draft);
    setValidation(res.data.validation);
    setCapabilities(res.data.capabilities);
    setFieldEffects(res.data.field_effects ?? []);
    setConfigPublication(res.data.publication ?? null);
    return true;
  }, [beginRequest, session]);

  useEffect(() => {
    const requestIsCurrent = beginRequest();
    void fetchConfig().catch((error) => {
      if (!requestIsCurrent()) return;
      console.error(error);
      showToast(t('settings.toast.fetchFailed'), 'error');
    }).finally(() => { if (requestIsCurrent()) setLoading(false); });
  }, [fetchConfig, beginRequest, showToast, t]);

  const refreshAfterSave = useCallback(async () => {
    const requestIsCurrent = beginRequest();
    try { await fetchConfig(); }
    catch (error) { if (requestIsCurrent()) { console.error(error); showToast(t('settings.toast.configRefreshFailed'), 'error'); } }
  }, [fetchConfig, beginRequest, showToast, t]);

  const saveConfig = useCallback<EditorValue['saveConfig']>(async (section, successMessage = t('settings.toast.configSaved')) => {
    if (section === 'connections' || pending.current.has(section)) return;
    const request = session.prepare(section);
    if (!request) return;
    const requestIsCurrent = beginRequest();
    markSaving(section, true);
    try {
      const res = await apiClient.post<ConfigSaveResponse>('/api/system/config', request.payload, { headers: { 'If-Match': request.version } });
      if (!requestIsCurrent()) return;
      session.acknowledge(section, request.sent, res.data.config || request.payload, res.headers.etag || res.data.publication?.version || request.version);
      publishConfig(session.draft);
      setValidation(res.data.validation);
      if (res.data.publication) { setLastPublication(res.data.publication); setConfigPublication(res.data.publication); }
      const message = res.data.publication?.restart_required.length ? t('settings.toast.configSavedRestart')
        : res.data.publication?.next_operation.length ? t('settings.toast.configSavedNext') : res.data.message || successMessage;
      showToast(message, 'success');
      await refreshAfterSave();
    } catch (error) {
      if (!requestIsCurrent()) return;
      console.error(error);
      if (isAxiosError(error) && error.response?.status === 409) {
        session.reject(section);
        showToast(t('settings.toast.configChanged'), 'error');
      } else if (isAxiosError(error) && error.response?.status === 422) {
        if (error.response.data?.validation) setValidation(error.response.data.validation);
        showToast(t('settings.toast.configInvalid'), 'error');
      } else showToast(t('settings.toast.configSaveFailed'), 'error');
    } finally { if (requestIsCurrent()) markSaving(section, false); }
  }, [beginRequest, markSaving, refreshAfterSave, session, showToast, t]);

  const saveProtocols = useCallback<EditorValue['saveProtocols']>(async (protocol, enabled) => {
    const request = session.prepare('connections');
    if (!request || pending.current.has('connections')) return { committed: false };
    const requestIsCurrent = beginRequest();
    const server = session.server!;
    const draft = { ...server, protocols: { ...server.protocols, [protocol]: { ...server.protocols?.[protocol], enabled } } };
    const payload = applySectionSnapshot(server, draft, 'connections');
    markSaving('connections', true);
    try {
      const res = await apiClient.post<ConfigSaveResponse>('/api/system/config', payload, { headers: { 'If-Match': request.version } });
      if (!requestIsCurrent()) return { committed: false };
      session.acknowledge('connections', request.sent, res.data.config || payload, res.headers.etag || res.data.publication?.version || request.version);
      publishConfig(session.draft);
      setValidation(res.data.validation);
      if (res.data.publication) { setLastPublication(res.data.publication); setConfigPublication(res.data.publication); }
      await refreshAfterSave();
      return { committed: true };
    } finally { if (requestIsCurrent()) markSaving('connections', false); }
  }, [beginRequest, markSaving, refreshAfterSave, session]);

  const setKOReaderForm = useCallback<EditorValue['setKOReaderForm']>((update) => {
    if (!session.draft) return;
    const form = typeof update === 'function' ? update(session.draft.koreader) : update;
    if (form) setConfig({ ...session.draft, koreader: form });
  }, [session, setConfig]);

  const saveKOReader = useCallback<EditorValue['saveKOReader']>(async () => {
    const request = session.prepare('koreader');
    if (!request || pending.current.has('koreader')) return { committed: false, requiresMaintenance: false };
    const requestIsCurrent = beginRequest();
    const previous = session.server?.koreader;
    markSaving('koreader', true);
    try {
      const res = await apiClient.post<KOReaderStatus>('/api/system/koreader', request.sent.koreader, { headers: { 'If-Match': request.version } });
      if (!requestIsCurrent()) return { committed: false, requiresMaintenance: false };
      const saved: Config = { ...request.payload, koreader: {
        enabled: res.data.enabled, base_path: res.data.base_path, allow_registration: res.data.allow_registration,
        match_mode: res.data.match_mode, path_ignore_extension: res.data.path_ignore_extension,
      } };
      session.acknowledge('koreader', request.sent, saved, res.headers.etag || request.version);
      publishConfig(session.draft);
      setKOReaderValidation(validResult);
      showToast(t('settings.toast.koreaderSaved'), 'success');
      await refreshAfterSave();
      return { committed: true, requiresMaintenance: previous?.match_mode !== saved.koreader.match_mode || previous?.path_ignore_extension !== saved.koreader.path_ignore_extension, status: res.data };
    } catch (error) {
      if (requestIsCurrent()) {
        console.error(error);
        if (isAxiosError(error) && error.response?.status === 409) {
          session.reject('koreader');
          showToast(t('settings.toast.configChanged'), 'error');
        } else if (isAxiosError(error) && error.response?.status === 422) {
          if (error.response.data?.validation) setKOReaderValidation(error.response.data.validation);
          showToast(t('settings.toast.koreaderInvalid'), 'error');
        } else showToast(t('settings.toast.koreaderSaveFailed'), 'error');
      }
      return { committed: false, requiresMaintenance: false };
    } finally { if (requestIsCurrent()) markSaving('koreader', false); }
  }, [beginRequest, markSaving, refreshAfterSave, session, showToast, t]);

  const discardSection = useCallback<EditorValue['discardSection']>(async (section) => {
    const requestIsCurrent = beginRequest();
    try {
      if (!await fetchConfig()) return;
      session.discard(section);
      publishConfig(session.draft);
      if (section === 'koreader') setKOReaderReloadVersion((version) => version + 1);
    } catch (error) { if (requestIsCurrent()) { console.error(error); showToast(t('settings.toast.configRefreshFailed'), 'error'); } }
  }, [beginRequest, fetchConfig, session, showToast, t]);

  const value = useMemo<EditorValue>(() => ({
    config, setConfig, validation, capabilities, fieldEffects, configPublication, lastPublication, loading,
    saving: savingSections.size > 0, isSectionSaving: (section) => savingSections.has(section),
    fieldErrors: (field) => validation.issues.filter((issue) => issue.field === field).map((issue) => issue.message),
    saveConfig, saveProtocols,
    hasSectionChanges: (section) => ['library', 'media', 'ai', 'connections', 'koreader'].includes(section) && session.dirty(section as EditorSectionKey),
    hasSectionConflict: (section) => session.conflicted(section), discardSection, showToast,
    koreaderReloadVersion, koreaderForm: config?.koreader ?? null, setKOReaderForm, koreaderValidation,
    koreaderFieldErrors: (field) => koreaderValidation.issues.filter((issue) => issue.field === field).map((issue) => issue.message),
    saveKOReader,
  }), [koreaderReloadVersion, config, setConfig, validation, capabilities, fieldEffects, configPublication, lastPublication, loading, savingSections, saveConfig, saveProtocols, session, discardSection, showToast, setKOReaderForm, koreaderValidation, saveKOReader]);
  return <EditorContext.Provider value={value}>{children}</EditorContext.Provider>;
}

export function useSettings() {
  const value = useContext(EditorContext);
  if (!value) throw new Error('useSettings must be used within SettingsEditorProvider');
  return value;
}

export function useKOReaderEditor() {
  const { koreaderReloadVersion, koreaderForm, setKOReaderForm, koreaderValidation, koreaderFieldErrors, isSectionSaving, saveKOReader } = useSettings();
  return useMemo(() => ({ koreaderReloadVersion, koreaderForm, setKOReaderForm, koreaderValidation, koreaderFieldErrors, savingKOReader: isSectionSaving('koreader'), saveKOReader }), [koreaderReloadVersion, koreaderForm, setKOReaderForm, koreaderValidation, koreaderFieldErrors, isSectionSaving, saveKOReader]);
}

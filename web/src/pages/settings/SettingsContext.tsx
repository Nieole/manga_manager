import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import axios from 'axios';
import { getClientLocale, translateInLocale, useI18n } from '../../i18n/LocaleProvider';

export interface Config {
  server: { port: number };
  database: { path: string };
  library: { paths: string[] };
  cache: { dir: string };
  logging: { level: string };
  scanner: {
    workers: number;
    thumbnail_format: string;
    waifu2x_path: string;
    realcugan_path: string;
    archive_pool_size: number;
    max_ai_concurrency: number;
  };
  llm: {
    provider: string;
    api_mode: string;
    base_url: string;
    request_path: string;
    endpoint: string;
    model: string;
    api_key: string;
    timeout: number;
  };
  koreader: {
    enabled: boolean;
    base_path: string;
    allow_registration: boolean;
    match_mode: string;
    path_ignore_extension: boolean;
  };
}

interface ValidationIssue {
  field: string;
  message: string;
  severity: string;
}

export interface ValidationResult {
  valid: boolean;
  issues: ValidationIssue[];
}

export interface Capabilities {
  supported_scan_formats: string[];
  supported_log_levels: string[];
  default_scan_formats: string;
  default_scan_interval: number;
  supported_llm_providers: string[];
  supported_llm_api_modes: string[];
}

interface ConfigEnvelope {
  config: Config;
  validation: ValidationResult;
  capabilities: Capabilities;
}

export interface KOReaderStatus {
  enabled: boolean;
  base_path: string;
  allow_registration: boolean;
  match_mode: string;
  path_ignore_extension: boolean;
  path_match_depth: number;
  account_count: number;
  enabled_account_count: number;
  latest_error?: string;
  stats: {
    configured: boolean;
    has_password: boolean;
    has_valid_sync_key: boolean;
    username: string;
    total_books: number;
    hashed_books: number;
    unmatched_progress_count: number;
    matched_progress_count: number;
    latest_sync_at?: { Time: string; Valid: boolean } | null;
  };
}

export interface KOReaderForm {
  enabled: boolean;
  base_path: string;
  allow_registration: boolean;
  match_mode: string;
  path_ignore_extension: boolean;
}

export interface KOReaderAccount {
  id: number;
  username: string;
  sync_key: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  last_used_at?: string | null;
  latest_error?: string;
}

interface KOReaderAccountForm {
  username: string;
}

export interface KOReaderUnmatchedItem {
  id: number;
  document: string;
  normalized_key: string;
  device: string;
  device_id: string;
  percentage: number;
  updated_at: string;
  suggestion: string;
}

type SettingsSectionKey = 'overview' | 'appearance' | 'library' | 'media' | 'ai' | 'koreader' | 'maintenance';

interface SettingsContextValue {
  config: Config | null;
  setConfig: React.Dispatch<React.SetStateAction<Config | null>>;
  validation: ValidationResult;
  capabilities: Capabilities | null;
  loading: boolean;
  saving: boolean;
  testingLLM: boolean;
  llmTestPrompt: string;
  setLlmTestPrompt: React.Dispatch<React.SetStateAction<string>>;
  llmTestResult: string | null;
  toastMsg: { text: string; type: 'success' | 'error' } | null;
  setToastMsg: React.Dispatch<React.SetStateAction<{ text: string; type: 'success' | 'error' } | null>>;
  showToast: (text: string, type?: 'success' | 'error') => void;
  fieldErrors: (field: string) => string[];
  saveConfig: (successMessage?: string) => Promise<void>;
  handleTestLLM: () => Promise<void>;
  koreaderStatus: KOReaderStatus | null;
  koreaderForm: KOReaderForm | null;
  setKOReaderForm: React.Dispatch<React.SetStateAction<KOReaderForm | null>>;
  koreaderValidation: ValidationResult;
  koreaderFieldErrors: (field: string) => string[];
  savingKOReader: boolean;
  saveKOReader: () => Promise<void>;
  koreaderAccounts: KOReaderAccount[];
  koreaderAccountForm: KOReaderAccountForm;
  setKOReaderAccountForm: React.Dispatch<React.SetStateAction<KOReaderAccountForm>>;
  unmatchedItems: KOReaderUnmatchedItem[];
  creatingAccount: boolean;
  accountActionId: number | null;
  applyingMatching: boolean;
  needsMatchingMaintenance: boolean;
  setNeedsMatchingMaintenance: React.Dispatch<React.SetStateAction<boolean>>;
  fetchKOReaderUnmatched: () => Promise<void>;
  handleApplyMatchingChanges: () => Promise<void>;
  handleCreateKOReaderAccount: () => Promise<void>;
  handleCopySyncKey: (account: KOReaderAccount) => Promise<void>;
  handleRotateKOReaderAccount: (account: KOReaderAccount) => Promise<void>;
  handleToggleKOReaderAccount: (account: KOReaderAccount) => Promise<void>;
  handleDeleteKOReaderAccount: (account: KOReaderAccount) => Promise<void>;
  handleAction: (path: string, successMessage: string, errorMessage?: string) => Promise<void>;
  hasSectionChanges: (section: SettingsSectionKey) => boolean;
  formatKOReaderLatestSync: (value?: { Time: string; Valid: boolean } | null) => string;
  formatKOReaderIndexLabel: (matchMode: string, pathIgnoreExtension: boolean) => string;
}

const SettingsContext = createContext<SettingsContextValue | null>(null);

function buildKOReaderForm(
  configState?: Config['koreader'] | null,
  status?: KOReaderStatus | null,
  current?: KOReaderForm | null,
): KOReaderForm {
  return {
    enabled: status?.enabled ?? configState?.enabled ?? current?.enabled ?? false,
    base_path: status?.base_path ?? configState?.base_path ?? current?.base_path ?? '/koreader',
    allow_registration: status?.allow_registration ?? configState?.allow_registration ?? current?.allow_registration ?? false,
    match_mode: status?.match_mode ?? configState?.match_mode ?? current?.match_mode ?? 'binary_hash',
    path_ignore_extension:
      status?.path_ignore_extension ?? configState?.path_ignore_extension ?? current?.path_ignore_extension ?? false,
  };
}

function pickSectionSnapshot(config: Config, section: Exclude<SettingsSectionKey, 'overview' | 'appearance' | 'koreader' | 'maintenance'>) {
  switch (section) {
    case 'library':
      return {
        server: config.server,
        database: config.database,
        library: config.library,
        logging: config.logging,
        scanner: {
          workers: config.scanner.workers,
          archive_pool_size: config.scanner.archive_pool_size,
        },
      };
    case 'media':
      return {
        cache: config.cache,
        scanner: {
          thumbnail_format: config.scanner.thumbnail_format,
          waifu2x_path: config.scanner.waifu2x_path,
          realcugan_path: config.scanner.realcugan_path,
          max_ai_concurrency: config.scanner.max_ai_concurrency,
        },
      };
    case 'ai':
      return {
        llm: config.llm,
      };
  }
}

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

export function SettingsProvider({ children }: { children: ReactNode }) {
  const { t } = useI18n();
  const [config, setConfig] = useState<Config | null>(null);
  const [initialConfig, setInitialConfig] = useState<Config | null>(null);
  const [validation, setValidation] = useState<ValidationResult>({ valid: true, issues: [] });
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testingLLM, setTestingLLM] = useState(false);
  const [savingKOReader, setSavingKOReader] = useState(false);
  const [llmTestPrompt, setLlmTestPrompt] = useState(() =>
    translateInLocale(getClientLocale(), 'settings.ai.defaultTestPrompt'),
  );
  const [llmTestResult, setLlmTestResult] = useState<string | null>(null);
  const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);
  const [koreaderStatus, setKOReaderStatus] = useState<KOReaderStatus | null>(null);
  const [koreaderForm, setKOReaderForm] = useState<KOReaderForm | null>(null);
  const [initialKOReaderForm, setInitialKOReaderForm] = useState<KOReaderForm | null>(null);
  const [koreaderValidation, setKOReaderValidation] = useState<ValidationResult>({ valid: true, issues: [] });
  const [koreaderAccounts, setKOReaderAccounts] = useState<KOReaderAccount[]>([]);
  const [koreaderAccountForm, setKOReaderAccountForm] = useState<KOReaderAccountForm>({ username: '' });
  const [unmatchedItems, setUnmatchedItems] = useState<KOReaderUnmatchedItem[]>([]);
  const [applyingMatching, setApplyingMatching] = useState(false);
  const [needsMatchingMaintenance, setNeedsMatchingMaintenance] = useState(false);
  const [creatingAccount, setCreatingAccount] = useState(false);
  const [accountActionId, setAccountActionId] = useState<number | null>(null);
  const configRef = useRef<Config | null>(null);
  const koreaderStatusRef = useRef<KOReaderStatus | null>(null);

  const showToast = useCallback((text: string, type: 'success' | 'error' = 'success') => {
    setToastMsg({ text, type });
    window.setTimeout(() => setToastMsg(null), 3200);
  }, []);

  useEffect(() => {
    configRef.current = config;
  }, [config]);

  useEffect(() => {
    koreaderStatusRef.current = koreaderStatus;
  }, [koreaderStatus]);

  const fetchConfig = useCallback(async () => {
    const res = await axios.get<ConfigEnvelope>('/api/system/config');
    setConfig(res.data.config);
    setInitialConfig(res.data.config);
    setValidation(res.data.validation);
    setCapabilities(res.data.capabilities);
    setKOReaderForm((current) => buildKOReaderForm(res.data.config.koreader, koreaderStatusRef.current, current));
  }, []);

  const fetchKOReader = useCallback(async () => {
    const res = await axios.get<KOReaderStatus>('/api/system/koreader');
    setKOReaderStatus(res.data);
    setKOReaderForm((current) => {
      const nextForm = buildKOReaderForm(configRef.current?.koreader, res.data, current);
      setInitialKOReaderForm(nextForm);
      return nextForm;
    });
    setKOReaderValidation({ valid: true, issues: [] });
  }, []);

  const fetchKOReaderAccounts = useCallback(async () => {
    const res = await axios.get<KOReaderAccount[]>('/api/system/koreader/accounts');
    setKOReaderAccounts(Array.isArray(res.data) ? res.data : []);
  }, []);

  const fetchKOReaderUnmatched = useCallback(async () => {
    const res = await axios.get<KOReaderUnmatchedItem[]>('/api/system/koreader/unmatched?limit=12');
    setUnmatchedItems(Array.isArray(res.data) ? res.data : []);
  }, []);

  useEffect(() => {
    Promise.all([fetchConfig(), fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderUnmatched()])
      .catch((error) => {
        console.error('Failed to fetch settings data', error);
        showToast(t('settings.toast.fetchFailed'), 'error');
      })
      .finally(() => setLoading(false));
  }, [fetchConfig, fetchKOReader, fetchKOReaderAccounts, fetchKOReaderUnmatched, showToast, t]);

  const validationByField = useMemo(() => {
    const map = new Map<string, string[]>();
    validation.issues.forEach((issue) => {
      const current = map.get(issue.field) || [];
      current.push(issue.message);
      map.set(issue.field, current);
    });
    return map;
  }, [validation]);

  const fieldErrors = useCallback((field: string) => validationByField.get(field) || [], [validationByField]);

  const koreaderValidationByField = useMemo(() => {
    const map = new Map<string, string[]>();
    koreaderValidation.issues.forEach((issue) => {
      const current = map.get(issue.field) || [];
      current.push(issue.message);
      map.set(issue.field, current);
    });
    return map;
  }, [koreaderValidation]);

  const koreaderFieldErrors = useCallback((field: string) => koreaderValidationByField.get(field) || [], [koreaderValidationByField]);

  const saveConfig = useCallback(
    async (successMessage = t('settings.toast.configSaved')) => {
      if (!config) return;
      setSaving(true);
      try {
        const res = await axios.post('/api/system/config', config);
        setValidation(res.data.validation);
        showToast(res.data.message || successMessage, 'success');
        await fetchConfig();
      } catch (error) {
        console.error(error);
        if (axios.isAxiosError(error) && error.response?.status === 422) {
          const nextValidation = error.response.data?.validation;
          if (nextValidation) setValidation(nextValidation);
          showToast(t('settings.toast.configInvalid'), 'error');
        } else {
          showToast(t('settings.toast.configSaveFailed'), 'error');
        }
      } finally {
        setSaving(false);
      }
    },
    [config, fetchConfig, showToast, t],
  );

  const handleTestLLM = useCallback(async () => {
    if (!config) return;
    setTestingLLM(true);
    setLlmTestResult(null);
    try {
      const res = await axios.post('/api/system/test-llm', {
        ...config.llm,
        prompt: llmTestPrompt,
      });
      setLlmTestResult(res.data.response);
      showToast(t('settings.toast.llmTestSucceeded'), 'success');
    } catch (error: unknown) {
      const message = axios.isAxiosError(error)
        ? error.response?.data?.error || error.message || t('settings.toast.llmTestFallback')
        : t('settings.toast.llmTestFallback');
      setLlmTestResult(`${t('common.errorPrefix')}: ${message}`);
      showToast(t('settings.toast.llmTestFailed'), 'error');
    } finally {
      setTestingLLM(false);
    }
  }, [config, llmTestPrompt, showToast, t]);

  const handleAction = useCallback(async (path: string, successMessage: string, errorMessage?: string) => {
    try {
      const res = await axios.post(path);
      showToast(res.data.message || successMessage, 'success');
    } catch (error) {
      console.error(error);
      showToast(errorMessage || t('settings.toast.actionFailed'), 'error');
    }
  }, [showToast, t]);

  const saveKOReader = useCallback(async () => {
    if (!koreaderForm) return;
    setSavingKOReader(true);
    try {
      const res = await axios.post<KOReaderStatus>('/api/system/koreader', koreaderForm);
      const requiresMaintenance = Boolean(
        koreaderStatus &&
          (koreaderStatus.match_mode !== koreaderForm.match_mode ||
            koreaderStatus.path_ignore_extension !== koreaderForm.path_ignore_extension),
      );
      setKOReaderStatus(res.data);
      const nextForm = buildKOReaderForm(config?.koreader, res.data, koreaderForm);
      setKOReaderForm(nextForm);
      setInitialKOReaderForm(nextForm);
      setNeedsMatchingMaintenance(requiresMaintenance);
      setKOReaderValidation({ valid: true, issues: [] });
      showToast(t('settings.toast.koreaderSaved'), 'success');
      await Promise.all([fetchConfig(), fetchKOReaderAccounts(), fetchKOReaderUnmatched()]);
    } catch (error) {
      console.error(error);
      if (axios.isAxiosError(error) && error.response?.status === 422) {
        const nextValidation = error.response.data?.validation;
        if (nextValidation) setKOReaderValidation(nextValidation);
        showToast(t('settings.toast.koreaderInvalid'), 'error');
      } else {
        showToast(t('settings.toast.koreaderSaveFailed'), 'error');
      }
    } finally {
      setSavingKOReader(false);
    }
  }, [config?.koreader, fetchConfig, fetchKOReaderAccounts, fetchKOReaderUnmatched, koreaderForm, koreaderStatus, showToast, t]);

  const handleApplyMatchingChanges = useCallback(async () => {
    setApplyingMatching(true);
    try {
      const res = await axios.post('/api/system/koreader/apply-matching');
      showToast(res.data?.message || t('settings.toast.koreaderApplyMatchingStarted'), 'success');
      setNeedsMatchingMaintenance(false);
      await fetchKOReader();
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderApplyMatchingFailed'), 'error');
    } finally {
      setApplyingMatching(false);
    }
  }, [fetchKOReader, showToast, t]);

  const handleCreateKOReaderAccount = useCallback(async () => {
    if (!koreaderAccountForm.username.trim()) return;
    setCreatingAccount(true);
    try {
      const res = await axios.post<KOReaderAccount>('/api/system/koreader/accounts', {
        username: koreaderAccountForm.username.trim(),
      });
      showToast(t('settings.toast.koreaderAccountCreated', { username: res.data.username }), 'success');
      setKOReaderAccountForm({ username: '' });
      await Promise.all([fetchKOReader(), fetchKOReaderAccounts()]);
    } catch (error) {
      console.error(error);
      if (axios.isAxiosError(error) && error.response?.status === 422) {
        const nextValidation = error.response.data?.validation;
        if (nextValidation) setKOReaderValidation(nextValidation);
      }
      showToast(t('settings.toast.koreaderAccountCreateFailed'), 'error');
    } finally {
      setCreatingAccount(false);
    }
  }, [fetchKOReader, fetchKOReaderAccounts, koreaderAccountForm.username, showToast, t]);

  const handleCopySyncKey = useCallback(async (account: KOReaderAccount) => {
    try {
      await navigator.clipboard.writeText(account.sync_key);
      showToast(t('settings.toast.koreaderSyncKeyCopied', { username: account.username }), 'success');
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderSyncKeyCopyFailed'), 'error');
    }
  }, [showToast, t]);

  const handleRotateKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    setAccountActionId(account.id);
    try {
      await axios.post(`/api/system/koreader/accounts/${account.id}/rotate-key`);
      showToast(t('settings.toast.koreaderSyncKeyRotated', { username: account.username }), 'success');
      await fetchKOReaderAccounts();
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderSyncKeyRotateFailed'), 'error');
    } finally {
      setAccountActionId(null);
    }
  }, [fetchKOReaderAccounts, showToast, t]);

  const handleToggleKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    setAccountActionId(account.id);
    try {
      await axios.post(`/api/system/koreader/accounts/${account.id}/toggle`, {
        enabled: !account.enabled,
      });
      showToast(
        t('settings.toast.koreaderAccountToggled', {
          username: account.username,
          action: account.enabled ? t('settings.toast.koreaderAccountDisabled') : t('settings.toast.koreaderAccountEnabled'),
        }),
        'success',
      );
      await Promise.all([fetchKOReader(), fetchKOReaderAccounts()]);
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderAccountToggleFailed'), 'error');
    } finally {
      setAccountActionId(null);
    }
  }, [fetchKOReader, fetchKOReaderAccounts, showToast, t]);

  const handleDeleteKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    setAccountActionId(account.id);
    try {
      await axios.delete(`/api/system/koreader/accounts/${account.id}`);
      showToast(t('settings.toast.koreaderAccountDeleted', { username: account.username }), 'success');
      await Promise.all([fetchKOReader(), fetchKOReaderAccounts()]);
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderAccountDeleteFailed'), 'error');
    } finally {
      setAccountActionId(null);
    }
  }, [fetchKOReader, fetchKOReaderAccounts, showToast, t]);

  const hasSectionChanges = useCallback(
    (section: SettingsSectionKey) => {
      if (section === 'overview' || section === 'appearance' || section === 'maintenance') return false;
      if (section === 'koreader') {
        return JSON.stringify(koreaderForm) !== JSON.stringify(initialKOReaderForm);
      }
      if (!config || !initialConfig) return false;
      return JSON.stringify(pickSectionSnapshot(config, section)) !== JSON.stringify(pickSectionSnapshot(initialConfig, section));
    },
    [config, initialConfig, initialKOReaderForm, koreaderForm],
  );

  const value = useMemo<SettingsContextValue>(
    () => ({
      config,
      setConfig,
      validation,
      capabilities,
      loading,
      saving,
      testingLLM,
      llmTestPrompt,
      setLlmTestPrompt,
      llmTestResult,
      toastMsg,
      setToastMsg,
      showToast,
      fieldErrors,
      saveConfig,
      handleTestLLM,
      koreaderStatus,
      koreaderForm,
      setKOReaderForm,
      koreaderValidation,
      koreaderFieldErrors,
      savingKOReader,
      saveKOReader,
      koreaderAccounts,
      koreaderAccountForm,
      setKOReaderAccountForm,
      unmatchedItems,
      creatingAccount,
      accountActionId,
      applyingMatching,
      needsMatchingMaintenance,
      setNeedsMatchingMaintenance,
      fetchKOReaderUnmatched,
      handleApplyMatchingChanges,
      handleCreateKOReaderAccount,
      handleCopySyncKey,
      handleRotateKOReaderAccount,
      handleToggleKOReaderAccount,
      handleDeleteKOReaderAccount,
      handleAction,
      hasSectionChanges,
      formatKOReaderLatestSync,
      formatKOReaderIndexLabel,
    }),
    [
      accountActionId,
      applyingMatching,
      capabilities,
      config,
      creatingAccount,
      fieldErrors,
      fetchKOReaderUnmatched,
      handleAction,
      handleApplyMatchingChanges,
      handleCopySyncKey,
      handleCreateKOReaderAccount,
      handleDeleteKOReaderAccount,
      handleRotateKOReaderAccount,
      handleTestLLM,
      handleToggleKOReaderAccount,
      hasSectionChanges,
      koreaderAccountForm,
      koreaderAccounts,
      koreaderFieldErrors,
      koreaderForm,
      koreaderStatus,
      koreaderValidation,
      llmTestPrompt,
      llmTestResult,
      loading,
      needsMatchingMaintenance,
      saveConfig,
      saveKOReader,
      saving,
      savingKOReader,
      showToast,
      testingLLM,
      toastMsg,
      unmatchedItems,
      validation,
    ],
  );

  return <SettingsContext.Provider value={value}>{children}</SettingsContext.Provider>;
}

export function useSettings() {
  const context = useContext(SettingsContext);
  if (!context) {
    throw new Error('useSettings must be used within SettingsProvider');
  }
  return context;
}

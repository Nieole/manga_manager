/**
 * 业务说明：本文件是业务实现，属于前端设置页面，负责运行时配置、扫描选项、元数据 Provider、缓存和系统能力的可视化管理。
 * 它把后端配置模型映射为用户可编辑表单，是系统行为变更的主要入口。
 * 维护时应关注字段默认值、保存反馈、敏感信息展示、配置刷新和与 config.Manager 的语义一致。
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import { getClientLocale, translateInLocale, useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import type { ValidationResult, SystemCapabilitiesResponse } from '../../api/generated';

export interface Config {
  server: { host: string; port: number; allowed_origins: string[] };
  database: { path: string };
  library: {
    paths: string[];
    storage_profile: string;
    io_policy: StorageIOPolicy;
    storage_policies: LibraryStoragePolicy[];
  };
  cache: { dir: string; page_disk_cache_enabled: boolean };
  logging: { level: string };
  scanner: {
    workers: number;
    scan_profile: string;
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
  protocols: {
    opds: { enabled: boolean };
    mihon: { enabled: boolean };
  };
  koreader: {
    enabled: boolean;
    base_path: string;
    allow_registration: boolean;
    match_mode: string;
    path_ignore_extension: boolean;
  };
}

// ValidationResult / Capabilities 已改由 cmd/tsgen 从 Go 结构体生成（web/src/api/generated.ts），此处按原名
// 重导出以维持既有用法；Capabilities 是后端 SystemCapabilitiesResponse 的别名。CI 的 tsgen 漂移门禁保证不与后端漂移。
export type { ValidationResult };
export type Capabilities = SystemCapabilitiesResponse;

export interface StorageIOPolicy {
  scan_concurrency: number;
  archive_open_concurrency: number;
  cover_concurrency: number;
  hash_concurrency: number;
  pause_background_when_reading: boolean;
  idle_only_heavy_tasks: boolean;
  disable_same_disk_page_cache: boolean;
}

export interface LibraryStoragePolicy {
  path: string;
  storage_profile: string;
  io_policy: StorageIOPolicy;
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

export interface KOReaderDeviceDiagnostics {
  summary: {
    device_count: number;
    healthy_devices: number;
    attention_devices: number;
    total_records: number;
    matched_records: number;
    unmatched_records: number;
    conflict_count: number;
    error_conflict_count: number;
  };
  devices: KOReaderDeviceItem[];
  conflicts: KOReaderDeviceConflictItem[];
}

export interface KOReaderDeviceItem {
  key: string;
  username: string;
  device: string;
  device_id: string;
  health: 'ready' | 'needs_reconcile' | 'error' | string;
  total_records: number;
  matched_records: number;
  unmatched_records: number;
  latest_sync_at?: string;
  latest_document: string;
  latest_matched_by: string;
  latest_error?: string;
  match_methods: Array<{ method: string; count: number }>;
  suggestion: string;
}

export interface KOReaderDeviceConflictItem {
  // "<来源表>:<主键>" 形式的复合标识，只用于列表 key。这个列表是两张表的 UNION，
  // 两边的自增主键会重号，所以它**不是**任何接口的入参。
  id: string;
  source_table: string;
  // 只在这一行确实对应一条进度记录时才有；「重置进度」只能用它。
  progress_id?: number;
  type: string;
  severity: 'warning' | 'error' | string;
  username: string;
  device: string;
  device_id: string;
  document: string;
  normalized_key: string;
  book_id?: number;
  matched_by: string;
  status: string;
  message: string;
  percentage: number;
  updated_at: string;
  suggestion: string;
}

type SettingsSectionKey = 'overview' | 'appearance' | 'library' | 'media' | 'ai' | 'koreader' | 'connections' | 'tags' | 'users' | 'maintenance';

// mega-context 已拆成两个独立 context（由同一个 SettingsProvider 分别 memo 后嵌套提供）：
// ConfigContextValue 承载核心配置域 + 共享助手；KOReaderContextValue 承载易变的 KOReader 状态
// （账户/设备/匹配/表单）。如此 KOReader 账户/设备等高频变化不再重渲仅消费配置的页面。
interface ConfigContextValue {
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
  showToast: (text: string, type?: 'success' | 'error') => void;
  fieldErrors: (field: string) => string[];
  saveConfig: (section: ConfigSectionKey, successMessage?: string) => Promise<void>;
  saveProtocols: (protocol: 'opds' | 'mihon', enabled: boolean) => Promise<void>;
  handleTestLLM: () => Promise<void>;
  handleAction: (path: string, successMessage: string, errorMessage?: string) => Promise<void>;
  hasSectionChanges: (section: SettingsSectionKey) => boolean;
}

interface KOReaderContextValue {
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

const ConfigContext = createContext<ConfigContextValue | null>(null);
const KOReaderContext = createContext<KOReaderContextValue | null>(null);

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

// connections 也是一个配置分区（协议开关落在 config.protocols 下），只是它没有 SaveBar
// ——开关一拨就即时保存。它不参与 hasSectionChanges 的脏标记正是因为没有草稿态。
export type ConfigSectionKey = Exclude<
  SettingsSectionKey,
  'overview' | 'appearance' | 'koreader' | 'tags' | 'users' | 'maintenance'
>;

// SECTION_FIELD_PATHS 是「哪些配置字段属于哪个分区」的**唯一**事实源。
//
// 脏标记与保存必须用同一份定义，否则两者会漂移：脏标记说这个分区没改，保存却把它写了出去。
// 路径用点号表示，只到需要区分的那一层——例如 scanner 下只有部分字段属于 library 分区，
// 其余属于 media 分区，所以这里必须写到叶子。
const SECTION_FIELD_PATHS: Record<ConfigSectionKey, string[]> = {
  library: [
    'server',
    'database',
    'library',
    'logging',
    'scanner.workers',
    'scanner.scan_profile',
    'scanner.archive_pool_size',
  ],
  media: [
    'cache',
    'scanner.thumbnail_format',
    'scanner.waifu2x_path',
    'scanner.realcugan_path',
    'scanner.max_ai_concurrency',
  ],
  ai: ['llm'],
  // protocols 整块属于连接分区：opds / mihon 两个子键没有被别的分区瓜分，无需写到叶子。
  connections: ['protocols'],
};

function readPath(source: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, key) => {
    if (acc && typeof acc === 'object') return (acc as Record<string, unknown>)[key];
    return undefined;
  }, source);
}

// writePath 在 target 上按路径写值，沿途只复制被改动的那一层，其余保持原引用。
function writePath(target: Record<string, unknown>, path: string, value: unknown): void {
  const keys = path.split('.');
  let node = target;
  for (let i = 0; i < keys.length - 1; i += 1) {
    const key = keys[i];
    const child = node[key];
    node[key] = child && typeof child === 'object' ? { ...(child as Record<string, unknown>) } : {};
    node = node[key] as Record<string, unknown>;
  }
  node[keys[keys.length - 1]] = value;
}

function pickSectionSnapshot(config: Config, section: ConfigSectionKey) {
  const snapshot: Record<string, unknown> = {};
  for (const path of SECTION_FIELD_PATHS[section]) {
    writePath(snapshot, path, readPath(config, path));
  }
  return snapshot;
}

// applySectionSnapshot 把 draft 里**只属于该分区**的字段叠到 base 上。
//
// 这是「保存本分区」真正的语义。此前 saveConfig 直接把整份 config state 发出去，
// 而那份 state 里带着用户在**其他分区**改了却没保存的草稿——点一下「保存媒体设置」，
// 顺手把没打算提交的 AI 密钥、扫描并发数一起写进了后端，界面上没有任何提示。
export function applySectionSnapshot(base: Config, draft: Config, section: ConfigSectionKey): Config {
  const merged = { ...(base as unknown as Record<string, unknown>) };
  for (const path of SECTION_FIELD_PATHS[section]) {
    writePath(merged, path, readPath(draft, path));
  }
  return merged as unknown as Config;
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
  const [koreaderStatus, setKOReaderStatus] = useState<KOReaderStatus | null>(null);
  const [koreaderForm, setKOReaderForm] = useState<KOReaderForm | null>(null);
  const [initialKOReaderForm, setInitialKOReaderForm] = useState<KOReaderForm | null>(null);
  const [koreaderValidation, setKOReaderValidation] = useState<ValidationResult>({ valid: true, issues: [] });
  const [koreaderAccounts, setKOReaderAccounts] = useState<KOReaderAccount[]>([]);
  const [koreaderAccountForm, setKOReaderAccountForm] = useState<KOReaderAccountForm>({ username: '' });
  const [unmatchedItems, setUnmatchedItems] = useState<KOReaderUnmatchedItem[]>([]);
  const [koreaderDevices, setKOReaderDevices] = useState<KOReaderDeviceDiagnostics | null>(null);
  const [applyingMatching, setApplyingMatching] = useState(false);
  const [needsMatchingMaintenance, setNeedsMatchingMaintenance] = useState(false);
  const [creatingAccount, setCreatingAccount] = useState(false);
  const [accountActionId, setAccountActionId] = useState<number | null>(null);
  const configRef = useRef<Config | null>(null);
  const koreaderStatusRef = useRef<KOReaderStatus | null>(null);
  const { showToast } = useToast();

  useEffect(() => {
    configRef.current = config;
  }, [config]);

  useEffect(() => {
    koreaderStatusRef.current = koreaderStatus;
  }, [koreaderStatus]);

  // savedSection 非空表示「刚保存完这个分区」：此时只把该分区刷成服务端的新值，
  // 其余分区保留用户手上的草稿。
  //
  // 「只保存本分区」的对偶就是「只刷新本分区」。少了这一半，保存 AI 设置会顺手把
  // 资料库分区没保存的草稿整片抹掉、侧边栏的脏点也一起熄灭，全程没有任何提示——
  // 方向比原缺陷安全（丢的是本地草稿不是写库），但同样是静默的。
  const fetchConfig = useCallback(async (savedSection?: ConfigSectionKey) => {
    const res = await apiClient.get<ConfigEnvelope>('/api/system/config');
    setConfig((prev) => (prev && savedSection ? applySectionSnapshot(prev, res.data.config, savedSection) : res.data.config));
    // initialConfig 是「服务端真相」的基线，必须整份更新。
    setInitialConfig(res.data.config);
    setValidation(res.data.validation);
    setCapabilities(res.data.capabilities);
    setKOReaderForm((current) => buildKOReaderForm(res.data.config.koreader, koreaderStatusRef.current, current));
  }, []);

  const fetchKOReader = useCallback(async () => {
    const res = await apiClient.get<KOReaderStatus>('/api/system/koreader');
    setKOReaderStatus(res.data);
    setKOReaderForm((current) => {
      const nextForm = buildKOReaderForm(configRef.current?.koreader, res.data, current);
      setInitialKOReaderForm(nextForm);
      return nextForm;
    });
    setKOReaderValidation({ valid: true, issues: [] });
  }, []);

  const fetchKOReaderAccounts = useCallback(async () => {
    const res = await apiClient.get<KOReaderAccount[]>('/api/system/koreader/accounts');
    setKOReaderAccounts(Array.isArray(res.data) ? res.data : []);
  }, []);

  const fetchKOReaderUnmatched = useCallback(async () => {
    const res = await apiClient.get<KOReaderUnmatchedItem[]>('/api/system/koreader/unmatched?limit=12');
    setUnmatchedItems(Array.isArray(res.data) ? res.data : []);
  }, []);

  const fetchKOReaderDevices = useCallback(async () => {
    const res = await apiClient.get<KOReaderDeviceDiagnostics>('/api/system/koreader/devices?limit=20');
    setKOReaderDevices(res.data);
  }, []);

  useEffect(() => {
    Promise.all([fetchConfig(), fetchKOReader(), fetchKOReaderAccounts()])
      .catch((error) => {
        console.error('Failed to fetch settings data', error);
        showToast(t('settings.toast.fetchFailed'), 'error');
      })
      .finally(() => setLoading(false));
  }, [fetchConfig, fetchKOReader, fetchKOReaderAccounts, showToast, t]);

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
    async (section: ConfigSectionKey, successMessage = t('settings.toast.configSaved')) => {
      if (!config || !initialConfig) return;
      setSaving(true);
      try {
        // 只提交本分区的字段：以服务端最近一次下发的 initialConfig 为底，
        // 把本分区的编辑叠上去。其他分区未保存的草稿留在本地，不会被顺手写出去。
        // 敏感字段（LLM APIKey 等）在 initialConfig 里是占位符，后端会据此保留原值，
        // 所以这样叠加不会把密钥抹成掩码串。
        const payload = applySectionSnapshot(initialConfig, config, section);
        const res = await apiClient.post('/api/system/config', payload);
        setValidation(res.data.validation);
        showToast(res.data.message || successMessage, 'success');
        await fetchConfig(section);
      } catch (error) {
        console.error(error);
        if (isAxiosError(error) && error.response?.status === 422) {
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
    [config, initialConfig, fetchConfig, showToast, t],
  );

  // saveProtocols 保存协议开关。
  //
  // 与 saveConfig 同一套语义：以服务端最近一次下发的 initialConfig 为底，只叠加 protocols。
  // 连接页此前自己拼 `{...config, protocols}` 再整份 POST——基底是**活的草稿**，
  // 于是「在资料库分区改了并发数没保存、切到连接页拨一下 OPDS 开关」就把那份改动一并写库了。
  const saveProtocols = useCallback(
    async (protocol: 'opds' | 'mihon', enabled: boolean) => {
      if (!config || !initialConfig) return;
      const draft = {
        ...config,
        protocols: {
          opds: { enabled: config.protocols?.opds?.enabled ?? false },
          mihon: { enabled: config.protocols?.mihon?.enabled ?? false },
        },
      } as Config;
      draft.protocols[protocol] = { enabled };
      const payload = applySectionSnapshot(initialConfig, draft, 'connections');
      const res = await apiClient.post('/api/system/config', payload);
      setValidation(res.data.validation);
      await fetchConfig('connections');
    },
    [config, initialConfig, fetchConfig],
  );

  const handleTestLLM = useCallback(async () => {
    if (!config) return;
    setTestingLLM(true);
    setLlmTestResult(null);
    try {
      const res = await apiClient.post('/api/system/test-llm', {
        ...config.llm,
        prompt: llmTestPrompt,
      });
      setLlmTestResult(res.data.response);
      showToast(t('settings.toast.llmTestSucceeded'), 'success');
    } catch (error: unknown) {
      const message = isAxiosError(error)
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
      const res = await apiClient.post(path);
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
      const res = await apiClient.post<KOReaderStatus>('/api/system/koreader', koreaderForm);
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
      await Promise.all([fetchConfig(), fetchKOReaderAccounts(), fetchKOReaderUnmatched(), fetchKOReaderDevices()]);
    } catch (error) {
      console.error(error);
      if (isAxiosError(error) && error.response?.status === 422) {
        const nextValidation = error.response.data?.validation;
        if (nextValidation) setKOReaderValidation(nextValidation);
        showToast(t('settings.toast.koreaderInvalid'), 'error');
      } else {
        showToast(t('settings.toast.koreaderSaveFailed'), 'error');
      }
    } finally {
      setSavingKOReader(false);
    }
  }, [config?.koreader, fetchConfig, fetchKOReaderAccounts, fetchKOReaderDevices, fetchKOReaderUnmatched, koreaderForm, koreaderStatus, showToast, t]);

  const handleApplyMatchingChanges = useCallback(async () => {
    setApplyingMatching(true);
    try {
      const res = await apiClient.post('/api/system/koreader/apply-matching');
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
      const res = await apiClient.post<KOReaderAccount>('/api/system/koreader/accounts', {
        username: koreaderAccountForm.username.trim(),
      });
      showToast(t('settings.toast.koreaderAccountCreated', { username: res.data.username }), 'success');
      setKOReaderAccountForm({ username: '' });
      await Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderDevices()]);
    } catch (error) {
      console.error(error);
      if (isAxiosError(error) && error.response?.status === 422) {
        const nextValidation = error.response.data?.validation;
        if (nextValidation) setKOReaderValidation(nextValidation);
      }
      showToast(t('settings.toast.koreaderAccountCreateFailed'), 'error');
    } finally {
      setCreatingAccount(false);
    }
  }, [fetchKOReader, fetchKOReaderAccounts, fetchKOReaderDevices, koreaderAccountForm.username, showToast, t]);

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
      await apiClient.post(`/api/system/koreader/accounts/${account.id}/rotate-key`);
      showToast(t('settings.toast.koreaderSyncKeyRotated', { username: account.username }), 'success');
      await Promise.all([fetchKOReaderAccounts(), fetchKOReaderDevices()]);
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderSyncKeyRotateFailed'), 'error');
    } finally {
      setAccountActionId(null);
    }
  }, [fetchKOReaderAccounts, fetchKOReaderDevices, showToast, t]);

  const handleToggleKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    setAccountActionId(account.id);
    try {
      await apiClient.post(`/api/system/koreader/accounts/${account.id}/toggle`, {
        enabled: !account.enabled,
      });
      showToast(
        t('settings.toast.koreaderAccountToggled', {
          username: account.username,
          action: account.enabled ? t('settings.toast.koreaderAccountDisabled') : t('settings.toast.koreaderAccountEnabled'),
        }),
        'success',
      );
      await Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderDevices()]);
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderAccountToggleFailed'), 'error');
    } finally {
      setAccountActionId(null);
    }
  }, [fetchKOReader, fetchKOReaderAccounts, fetchKOReaderDevices, showToast, t]);

  const handleDeleteKOReaderAccount = useCallback(async (account: KOReaderAccount) => {
    setAccountActionId(account.id);
    try {
      await apiClient.delete(`/api/system/koreader/accounts/${account.id}`);
      showToast(t('settings.toast.koreaderAccountDeleted', { username: account.username }), 'success');
      await Promise.all([fetchKOReader(), fetchKOReaderAccounts(), fetchKOReaderDevices()]);
    } catch (error) {
      console.error(error);
      showToast(t('settings.toast.koreaderAccountDeleteFailed'), 'error');
    } finally {
      setAccountActionId(null);
    }
  }, [fetchKOReader, fetchKOReaderAccounts, fetchKOReaderDevices, showToast, t]);

  const hasSectionChanges = useCallback(
    (section: SettingsSectionKey) => {
      if (section === 'overview' || section === 'appearance' || section === 'connections' || section === 'tags' || section === 'users' || section === 'maintenance') return false;
      if (section === 'koreader') {
        return JSON.stringify(koreaderForm) !== JSON.stringify(initialKOReaderForm);
      }
      if (!config || !initialConfig) return false;
      return JSON.stringify(pickSectionSnapshot(config, section)) !== JSON.stringify(pickSectionSnapshot(initialConfig, section));
    },
    [config, initialConfig, initialKOReaderForm, koreaderForm],
  );

  const configValue = useMemo<ConfigContextValue>(
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
      showToast,
      fieldErrors,
      saveConfig,
      saveProtocols,
      handleTestLLM,
      handleAction,
      hasSectionChanges,
    }),
    [
      capabilities,
      config,
      fieldErrors,
      handleAction,
      handleTestLLM,
      hasSectionChanges,
      llmTestPrompt,
      llmTestResult,
      loading,
      saveConfig,
      saveProtocols,
      saving,
      showToast,
      testingLLM,
      validation,
    ],
  );

  const koreaderValue = useMemo<KOReaderContextValue>(
    () => ({
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
      koreaderFieldErrors,
      koreaderForm,
      koreaderStatus,
      koreaderValidation,
      needsMatchingMaintenance,
      saveKOReader,
      savingKOReader,
      unmatchedItems,
    ],
  );

  return (
    <ConfigContext.Provider value={configValue}>
      <KOReaderContext.Provider value={koreaderValue}>{children}</KOReaderContext.Provider>
    </ConfigContext.Provider>
  );
}

// useSettings 返回配置域 context（保留原名，配置类页面无需改动，且不再随 KOReader 状态 churn 重渲）。
export function useSettings() {
  const context = useContext(ConfigContext);
  if (!context) {
    throw new Error('useSettings must be used within SettingsProvider');
  }
  return context;
}

// useKOReader 返回 KOReader 域 context（账户/设备/匹配/表单等易变状态）。
export function useKOReader() {
  const context = useContext(KOReaderContext);
  if (!context) {
    throw new Error('useKOReader must be used within SettingsProvider');
  }
  return context;
}

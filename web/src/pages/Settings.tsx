import { useEffect, useMemo, useState } from 'react';
import axios from 'axios';
import { AlertTriangle, CheckCircle2, Database, FolderOpen, HardDrive, Image as ImageIcon, KeyRound, RefreshCw, Save, Server, Settings as SettingsIcon, Sparkles, TabletSmartphone, Terminal } from 'lucide-react';

interface Config {
  server: { port: number };
  database: { path: string };
  library: { paths: string[] };
  cache: { dir: string };
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
  };
}

interface ValidationIssue {
  field: string;
  message: string;
  severity: string;
}

interface ValidationResult {
  valid: boolean;
  issues: ValidationIssue[];
}

interface Capabilities {
  supported_scan_formats: string[];
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

interface KOReaderStatus {
  enabled: boolean;
  base_path: string;
  allow_registration: boolean;
  username: string;
  has_password: boolean;
  stats: {
    configured: boolean;
    has_password: boolean;
    username: string;
    total_books: number;
    hashed_books: number;
    unmatched_progress_count: number;
    matched_progress_count: number;
    latest_sync_at?: { Time: string; Valid: boolean } | null;
  };
}

interface KOReaderForm {
  enabled: boolean;
  base_path: string;
  allow_registration: boolean;
  username: string;
  password: string;
}

const sectionClassName = 'bg-komgaSurface border border-gray-800 rounded-2xl p-6 shadow-sm space-y-4';
const inputClassName = 'w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/40 transition-all';

export default function Settings() {
  const [config, setConfig] = useState<Config | null>(null);
  const [validation, setValidation] = useState<ValidationResult>({ valid: true, issues: [] });
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testingLLM, setTestingLLM] = useState(false);
  const [savingKOReader, setSavingKOReader] = useState(false);
  const [llmTestPrompt, setLlmTestPrompt] = useState('你好，请做个简短的自我介绍，并确认你收到了测试请求。');
  const [llmTestResult, setLlmTestResult] = useState<string | null>(null);
  const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);
  const [koreaderStatus, setKOReaderStatus] = useState<KOReaderStatus | null>(null);
  const [koreaderForm, setKOReaderForm] = useState<KOReaderForm | null>(null);
  const [koreaderValidation, setKOReaderValidation] = useState<ValidationResult>({ valid: true, issues: [] });

  const showToast = (text: string, type: 'success' | 'error' = 'success') => {
    setToastMsg({ text, type });
    window.setTimeout(() => setToastMsg(null), 3200);
  };

  const fetchConfig = async () => {
    const res = await axios.get<ConfigEnvelope>('/api/system/config');
    setConfig(res.data.config);
    setValidation(res.data.validation);
    setCapabilities(res.data.capabilities);
  };

  const fetchKOReader = async () => {
    const res = await axios.get<KOReaderStatus>('/api/system/koreader');
    setKOReaderStatus(res.data);
    setKOReaderForm({
      enabled: res.data.enabled,
      base_path: res.data.base_path,
      allow_registration: res.data.allow_registration,
      username: res.data.username || '',
      password: '',
    });
    setKOReaderValidation({ valid: true, issues: [] });
  };

  useEffect(() => {
    Promise.all([fetchConfig(), fetchKOReader()])
      .catch((error) => {
        console.error('Failed to fetch settings data', error);
        showToast('无法加载系统配置', 'error');
      })
      .finally(() => setLoading(false));
  }, []);

  const validationByField = useMemo(() => {
    const map = new Map<string, string[]>();
    validation.issues.forEach((issue) => {
      const current = map.get(issue.field) || [];
      current.push(issue.message);
      map.set(issue.field, current);
    });
    return map;
  }, [validation]);

  const fieldErrors = (field: string) => validationByField.get(field) || [];

  const koreaderValidationByField = useMemo(() => {
    const map = new Map<string, string[]>();
    koreaderValidation.issues.forEach((issue) => {
      const current = map.get(issue.field) || [];
      current.push(issue.message);
      map.set(issue.field, current);
    });
    return map;
  }, [koreaderValidation]);

  const koreaderFieldErrors = (field: string) => koreaderValidationByField.get(field) || [];

  const handleSave = async () => {
    if (!config) return;
    setSaving(true);
    try {
      const res = await axios.post('/api/system/config', config);
      setValidation(res.data.validation);
      showToast(res.data.message || '配置已保存', 'success');
      await Promise.all([fetchConfig(), fetchKOReader()]);
    } catch (error) {
      console.error(error);
      if (axios.isAxiosError(error) && error.response?.status === 422) {
        const nextValidation = error.response.data?.validation;
        if (nextValidation) {
          setValidation(nextValidation);
        }
        showToast('配置未通过校验，请先修正高亮字段。', 'error');
      } else {
        showToast('保存失败，请检查配置和文件权限。', 'error');
      }
    } finally {
      setSaving(false);
    }
  };

  const handleTestLLM = async () => {
    if (!config) return;
    setTestingLLM(true);
    setLlmTestResult(null);
    try {
      const res = await axios.post('/api/system/test-llm', {
        ...config.llm,
        prompt: llmTestPrompt,
      });
      setLlmTestResult(res.data.response);
      showToast('LLM 联通性测试成功', 'success');
    } catch (error: any) {
      const message = error.response?.data?.error || '测试失败，请检查地址、协议模式和模型名。';
      setLlmTestResult(`Error: ${message}`);
      showToast('LLM 测试失败', 'error');
    } finally {
      setTestingLLM(false);
    }
  };

  const handleAction = async (path: string, successMessage: string) => {
    try {
      const res = await axios.post(path);
      showToast(res.data.message || successMessage, 'success');
    } catch (error) {
      console.error(error);
      showToast(successMessage.replace('已', '未能'), 'error');
    }
  };

  const handleSaveKOReader = async () => {
    if (!koreaderForm) return;
    setSavingKOReader(true);
    try {
      const res = await axios.post<KOReaderStatus>('/api/system/koreader', koreaderForm);
      setKOReaderStatus(res.data);
      setKOReaderForm({
        enabled: res.data.enabled,
        base_path: res.data.base_path,
        allow_registration: res.data.allow_registration,
        username: res.data.username || '',
        password: '',
      });
      setKOReaderValidation({ valid: true, issues: [] });
      showToast('KOReader 同步配置已保存', 'success');
      await fetchConfig();
    } catch (error) {
      console.error(error);
      if (axios.isAxiosError(error) && error.response?.status === 422) {
        const nextValidation = error.response.data?.validation;
        if (nextValidation) {
          setKOReaderValidation(nextValidation);
        }
        showToast('KOReader 配置未通过校验。', 'error');
      } else {
        showToast('保存 KOReader 配置失败。', 'error');
      }
    } finally {
      setSavingKOReader(false);
    }
  };

  const renderFieldErrors = (field: string) => (
    fieldErrors(field).map((message) => (
      <p key={`${field}-${message}`} className="mt-1 text-xs text-red-300">{message}</p>
    ))
  );

  const renderKOReaderFieldErrors = (field: string) => (
    koreaderFieldErrors(field).map((message) => (
      <p key={`${field}-${message}`} className="mt-1 text-xs text-red-300">{message}</p>
    ))
  );

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-komgaPrimary"></div>
      </div>
    );
  }

  if (!config) {
    return <div className="p-8 text-center text-gray-500">无法加载配置。</div>;
  }

  return (
    <div className="p-4 sm:p-8 max-w-5xl mx-auto space-y-6">
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="flex items-center gap-3">
          <SettingsIcon className="w-8 h-8 text-komgaPrimary" />
          <div>
            <h1 className="text-2xl font-bold text-white tracking-tight">系统设定</h1>
            <p className="text-sm text-gray-400">先修正配置正确性，再调整扫描、缓存和元数据策略。</p>
          </div>
        </div>

        <div className={`inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-sm ${validation.valid ? 'bg-emerald-500/10 text-emerald-300 border border-emerald-500/20' : 'bg-amber-500/10 text-amber-300 border border-amber-500/20'}`}>
          {validation.valid ? <CheckCircle2 className="w-4 h-4" /> : <AlertTriangle className="w-4 h-4" />}
          {validation.valid ? '配置校验通过' : `存在 ${validation.issues.length} 个待修正项`}
        </div>
      </div>

      {!validation.valid && (
        <div className="rounded-2xl border border-amber-500/20 bg-amber-500/10 p-4">
          <div className="flex items-start gap-3">
            <AlertTriangle className="w-5 h-5 text-amber-300 shrink-0 mt-0.5" />
            <div className="space-y-1">
              <p className="text-sm font-medium text-amber-100">配置还不能安全保存。</p>
              {validation.issues.slice(0, 5).map((issue) => (
                <p key={`${issue.field}-${issue.message}`} className="text-sm text-amber-200/90">
                  {issue.field}: {issue.message}
                </p>
              ))}
            </div>
          </div>
        </div>
      )}

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Server className="w-5 h-5" />
          <h2 className="text-lg font-semibold text-white">基础设置</h2>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="block text-sm text-gray-400 mb-1">服务端口</label>
            <input
              type="number"
              value={config.server.port}
              onChange={(e) => setConfig({ ...config, server: { ...config.server, port: Number(e.target.value) || 8080 } })}
              className={inputClassName}
            />
            {renderFieldErrors('server.port')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">数据库路径</label>
            <input
              type="text"
              value={config.database.path}
              onChange={(e) => setConfig({ ...config, database: { ...config.database, path: e.target.value } })}
              className={inputClassName}
            />
            {renderFieldErrors('database.path')}
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <FolderOpen className="w-5 h-5" />
          <h2 className="text-lg font-semibold text-white">书库与扫描</h2>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="block text-sm text-gray-400 mb-1">扫描工作协程</label>
            <input
              type="range"
              min="0"
              max="64"
              value={config.scanner.workers}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, workers: Number(e.target.value) || 0 } })}
              className="w-full accent-komgaPrimary"
            />
            <p className="text-xs text-gray-500 mt-1">{config.scanner.workers === 0 ? '0 表示自动调度。' : `当前固定为 ${config.scanner.workers} 个工作协程。`}</p>
            {renderFieldErrors('scanner.workers')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">归档句柄池大小</label>
            <input
              type="range"
              min="1"
              max="50"
              value={config.scanner.archive_pool_size}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, archive_pool_size: Number(e.target.value) || 1 } })}
              className="w-full accent-komgaPrimary"
            />
            <p className="text-xs text-gray-500 mt-1">大一些能减少频繁翻页的重复打开成本，但会占用更多句柄和内存。</p>
            {renderFieldErrors('scanner.archive_pool_size')}
          </div>
        </div>

        <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4 text-sm text-gray-300">
          <p className="font-medium text-white mb-1">当前支持的扫描格式</p>
          <p>{capabilities?.default_scan_formats || 'zip,cbz,rar,cbr'}</p>
          <p className="text-xs text-gray-500 mt-2">已移除 PDF 的默认宣称，避免出现“可选但实际无法解析”的错误认知。</p>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <HardDrive className="w-5 h-5" />
          <h2 className="text-lg font-semibold text-white">图片与缓存</h2>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="block text-sm text-gray-400 mb-1">缓存目录</label>
            <input
              type="text"
              value={config.cache.dir}
              onChange={(e) => setConfig({ ...config, cache: { ...config.cache, dir: e.target.value } })}
              className={inputClassName}
            />
            {renderFieldErrors('cache.dir')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">缩略图格式</label>
            <select
              value={config.scanner.thumbnail_format}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, thumbnail_format: e.target.value } })}
              className={inputClassName}
            >
              <option value="webp">WebP</option>
              <option value="avif">AVIF</option>
              <option value="jpg">JPEG</option>
            </select>
            {renderFieldErrors('scanner.thumbnail_format')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">Waifu2x 可执行文件</label>
            <input
              type="text"
              value={config.scanner.waifu2x_path}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, waifu2x_path: e.target.value } })}
              className={inputClassName}
            />
            {renderFieldErrors('scanner.waifu2x_path')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">Real-CUGAN 可执行文件</label>
            <input
              type="text"
              value={config.scanner.realcugan_path}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, realcugan_path: e.target.value } })}
              className={inputClassName}
            />
            {renderFieldErrors('scanner.realcugan_path')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">AI 超分并发上限</label>
            <input
              type="range"
              min="1"
              max="10"
              value={config.scanner.max_ai_concurrency}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, max_ai_concurrency: Number(e.target.value) || 1 } })}
              className="w-full accent-purple-500"
            />
            {renderFieldErrors('scanner.max_ai_concurrency')}
          </div>

          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4 text-sm text-gray-300">
            <div className="flex items-center gap-2 text-white mb-2">
              <ImageIcon className="w-4 h-4 text-komgaPrimary" />
              超分引擎说明
            </div>
            <p>建议先确保本地命令可直接运行，再把绝对路径填到这里。保存前会验证文件是否存在。</p>
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-purple-400">
          <Sparkles className="w-5 h-5" />
          <h2 className="text-lg font-semibold text-white">AI / 元数据</h2>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="block text-sm text-gray-400 mb-1">提供方</label>
            <select
              value={config.llm.provider}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, provider: e.target.value } })}
              className={inputClassName}
            >
              <option value="ollama">Ollama</option>
              <option value="openai">OpenAI Compatible</option>
            </select>
            {renderFieldErrors('llm.provider')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">协议模式</label>
            <select
              value={config.llm.api_mode || 'responses'}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, api_mode: e.target.value } })}
              className={inputClassName}
              disabled={config.llm.provider !== 'openai'}
            >
              <option value="responses">Responses API</option>
              <option value="chat_completions">Chat Completions</option>
            </select>
            {renderFieldErrors('llm.api_mode')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">Base URL</label>
            <input
              type="text"
              value={config.llm.base_url}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, base_url: e.target.value } })}
              className={inputClassName}
            />
            {renderFieldErrors('llm.base_url')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">请求路径</label>
            <input
              type="text"
              value={config.llm.request_path}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, request_path: e.target.value } })}
              className={inputClassName}
              disabled={config.llm.provider !== 'openai'}
            />
            {renderFieldErrors('llm.request_path')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">模型名</label>
            <input
              type="text"
              value={config.llm.model}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, model: e.target.value } })}
              className={inputClassName}
            />
            {renderFieldErrors('llm.model')}
          </div>

          <div>
            <label className="block text-sm text-gray-400 mb-1">API Key</label>
            <input
              type="password"
              value={config.llm.api_key}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, api_key: e.target.value } })}
              className={inputClassName}
            />
          </div>
        </div>

        <div>
          <label className="block text-sm text-gray-400 mb-1">超时时间（秒）</label>
          <input
            type="range"
            min="10"
            max="600"
            step="10"
            value={config.llm.timeout}
            onChange={(e) => setConfig({ ...config, llm: { ...config.llm, timeout: Number(e.target.value) || 120 } })}
            className="w-full accent-komgaPrimary"
          />
          <p className="text-xs text-gray-500 mt-1">当前：{config.llm.timeout}s</p>
          {renderFieldErrors('llm.timeout')}
        </div>

        <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4 space-y-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium text-white">联通性测试</p>
              <p className="text-xs text-gray-500">保存前先确认地址、协议模式和模型名能正常响应。</p>
            </div>
            <button
              onClick={handleTestLLM}
              disabled={testingLLM}
              className="inline-flex items-center gap-2 rounded-lg border border-purple-500/30 bg-purple-500/10 px-4 py-2 text-sm text-purple-200 hover:bg-purple-500/20 disabled:opacity-60"
            >
              {testingLLM ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Terminal className="w-4 h-4" />}
              {testingLLM ? '测试中...' : '测试连接'}
            </button>
          </div>
          <input
            type="text"
            value={llmTestPrompt}
            onChange={(e) => setLlmTestPrompt(e.target.value)}
            className={inputClassName}
          />
          {llmTestResult && (
            <pre className="max-h-56 overflow-auto rounded-lg border border-gray-800 bg-black/40 p-3 text-sm text-gray-200 whitespace-pre-wrap">
              {llmTestResult}
            </pre>
          )}
        </div>
      </section>

      {koreaderForm && (
        <section className={sectionClassName}>
          <div className="flex items-center gap-2 text-sky-400">
            <TabletSmartphone className="w-5 h-5" />
            <h2 className="text-lg font-semibold text-white">KOReader Sync</h2>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
              <label className="flex items-center justify-between gap-3">
                <div>
                  <p className="text-sm font-medium text-white">启用 KOReader 同步服务</p>
                  <p className="text-xs text-gray-500 mt-1">启用后，KOReader 可以把本程序当作自定义 progress sync server。</p>
                </div>
                <input
                  type="checkbox"
                  checked={koreaderForm.enabled}
                  onChange={(e) => setKOReaderForm({ ...koreaderForm, enabled: e.target.checked })}
                  className="h-5 w-5 rounded border-gray-700 bg-gray-900 text-komgaPrimary"
                />
              </label>
            </div>

            <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4">
              <p className="text-sm font-medium text-white">服务状态</p>
              <p className="text-xs text-gray-500 mt-1">
                已匹配 {koreaderStatus?.stats.matched_progress_count ?? 0} 条，同步待重关联 {koreaderStatus?.stats.unmatched_progress_count ?? 0} 条。
              </p>
              <p className="text-xs text-gray-500 mt-2">
                指纹进度 {koreaderStatus?.stats.hashed_books ?? 0} / {koreaderStatus?.stats.total_books ?? 0}
              </p>
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-1">同步路径</label>
              <input
                type="text"
                value={koreaderForm.base_path}
                onChange={(e) => setKOReaderForm({ ...koreaderForm, base_path: e.target.value })}
                className={inputClassName}
              />
              <p className="text-xs text-gray-500 mt-1">当前启动实例监听在 `{koreaderStatus?.base_path || '/koreader'}`。修改路径后建议重启服务。</p>
              {renderKOReaderFieldErrors('koreader.base_path')}
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-1">允许首次注册</label>
              <select
                value={koreaderForm.allow_registration ? 'true' : 'false'}
                onChange={(e) => setKOReaderForm({ ...koreaderForm, allow_registration: e.target.value === 'true' })}
                className={inputClassName}
              >
                <option value="false">关闭</option>
                <option value="true">开启</option>
              </select>
              <p className="text-xs text-gray-500 mt-1">建议单用户场景默认关闭，通过本页直接配置同步账号。</p>
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-1">同步用户名</label>
              <input
                type="text"
                value={koreaderForm.username}
                onChange={(e) => setKOReaderForm({ ...koreaderForm, username: e.target.value })}
                className={inputClassName}
              />
              {renderKOReaderFieldErrors('koreader.username')}
            </div>

            <div>
              <label className="block text-sm text-gray-400 mb-1">同步密钥</label>
              <div className="relative">
                <KeyRound className="pointer-events-none absolute left-3 top-3.5 h-4 w-4 text-gray-500" />
                <input
                  type="password"
                  value={koreaderForm.password}
                  onChange={(e) => setKOReaderForm({ ...koreaderForm, password: e.target.value })}
                  className={`${inputClassName} pl-10`}
                  placeholder={koreaderStatus?.has_password ? '留空表示保留现有密钥' : '首次启用时必填'}
                />
              </div>
              {renderKOReaderFieldErrors('koreader.password')}
            </div>
          </div>

          <div className="rounded-xl border border-sky-500/20 bg-sky-500/5 p-4 text-sm text-sky-100">
            <p className="font-medium">KOReader 配置方式</p>
            <p className="mt-1 text-sky-100/80">在 KOReader 中将 Custom sync server 设置为 `{window.location.origin}{koreaderStatus?.base_path || '/koreader'}`，用户名和同步密钥与这里保持一致。</p>
          </div>

          <div className="grid gap-3 md:grid-cols-3">
            <button
              onClick={handleSaveKOReader}
              disabled={savingKOReader}
              className="rounded-xl border border-sky-500/20 bg-sky-500/10 px-4 py-4 text-left text-sky-100 hover:bg-sky-500/15 disabled:opacity-60"
            >
              <p className="font-medium inline-flex items-center gap-2">
                {savingKOReader ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                保存同步配置
              </p>
              <p className="text-xs text-sky-100/80 mt-1">保存启用状态、路径和同步账号。首次启用需要设置同步密钥。</p>
            </button>
            <button
              onClick={() => handleAction('/api/system/koreader/rebuild-hashes', '书籍同步指纹重建已启动')}
              className="rounded-xl border border-sky-500/20 bg-sky-500/10 px-4 py-4 text-left text-sky-100 hover:bg-sky-500/15"
            >
              <p className="font-medium">重建书籍指纹</p>
              <p className="text-xs text-sky-100/80 mt-1">为现有书籍补全 Binary / 路径 / 文件名三类匹配指纹。</p>
            </button>
            <button
              onClick={() => handleAction('/api/system/koreader/reconcile', '未匹配同步记录重关联已启动')}
              className="rounded-xl border border-sky-500/20 bg-sky-500/10 px-4 py-4 text-left text-sky-100 hover:bg-sky-500/15"
            >
              <p className="font-medium">重关联未匹配记录</p>
              <p className="text-xs text-sky-100/80 mt-1">重新尝试把历史同步记录映射回已入库书籍。</p>
            </button>
          </div>
        </section>
      )}

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-red-400">
          <AlertTriangle className="w-5 h-5" />
          <h2 className="text-lg font-semibold text-white">维护工具</h2>
        </div>
        <div className="grid gap-3 md:grid-cols-3">
          <button
            onClick={() => handleAction('/api/system/rebuild-index', '搜索索引已重建')}
            className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15"
          >
            <p className="font-medium">重建搜索索引</p>
            <p className="text-xs text-red-200/80 mt-1">适合搜索结果异常、索引损坏或切换分词策略后执行。</p>
          </button>
          <button
            onClick={() => handleAction('/api/system/rebuild-thumbnails', '缩略图重建已启动')}
            className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15"
          >
            <p className="font-medium">重建缩略图缓存</p>
            <p className="text-xs text-red-200/80 mt-1">会触发大量磁盘 IO，适合封面损坏或切换缓存格式后执行。</p>
          </button>
          <button
            onClick={() => handleAction('/api/system/batch-scrape', '批量元数据刮削已启动')}
            className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15"
          >
            <p className="font-medium">批量元数据刮削</p>
            <p className="text-xs text-red-200/80 mt-1">会持续占用 LLM 或外部数据源，请优先在空闲时段运行。</p>
          </button>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Database className="w-5 h-5" />
          <h2 className="text-lg font-semibold text-white">当前已绑定目录</h2>
        </div>
        {config.library.paths?.length ? (
          <div className="space-y-2">
            {config.library.paths.map((path) => (
              <div key={path} className="rounded-lg border border-gray-800 bg-gray-900/50 px-3 py-2 text-sm text-gray-300">
                {path}
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-gray-500">还没有绑定目录。可以回到仪表板，通过首启向导添加第一个资源库。</p>
        )}
      </section>

      <div className="sticky bottom-0 z-10 -mx-4 sm:-mx-8 border-t border-gray-800 bg-komgaDark/90 px-4 py-4 backdrop-blur-md">
        <div className="mx-auto flex max-w-5xl items-center justify-between gap-4">
          <div className="text-sm text-gray-500">
            {capabilities ? `支持格式：${capabilities.default_scan_formats}` : '正在载入能力信息...'}
          </div>
          <button
            onClick={handleSave}
            disabled={saving}
            className="inline-flex items-center gap-2 rounded-xl bg-komgaPrimary px-5 py-3 text-sm font-medium text-white shadow-lg hover:bg-purple-600 disabled:opacity-60"
          >
            {saving ? <RefreshCw className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
            {saving ? '保存中...' : '保存配置'}
          </button>
        </div>
      </div>

      {toastMsg && (
        <div className={`fixed bottom-6 right-6 z-50 rounded-xl border px-4 py-3 text-sm shadow-xl ${toastMsg.type === 'success' ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-200' : 'border-red-500/30 bg-red-500/10 text-red-200'}`}>
          {toastMsg.text}
        </div>
      )}
    </div>
  );
}

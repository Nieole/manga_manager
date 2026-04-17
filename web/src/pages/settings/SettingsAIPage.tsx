import { Sparkles, Terminal } from 'lucide-react';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsAIPage() {
  const { config, setConfig, fieldErrors, testingLLM, handleTestLLM, llmTestPrompt, setLlmTestPrompt, llmTestResult, saving, saveConfig } = useSettings();

  if (!config) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title="AI / 元数据" description="配置 LLM 提供方、请求协议和模型参数，并在保存前做联通性测试。" />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Sparkles className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">模型与协议</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">提供方</label>
            <select value={config.llm.provider} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, provider: e.target.value } })} className={inputClassName}>
              <option value="ollama">Ollama</option>
              <option value="openai">OpenAI Compatible</option>
            </select>
            <FieldErrors messages={fieldErrors('llm.provider')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">协议模式</label>
            <select
              value={config.llm.api_mode || 'responses'}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, api_mode: e.target.value } })}
              className={inputClassName}
              disabled={config.llm.provider !== 'openai'}
            >
              <option value="responses">Responses API</option>
              <option value="chat_completions">Chat Completions</option>
            </select>
            <FieldErrors messages={fieldErrors('llm.api_mode')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">Base URL</label>
            <input type="text" value={config.llm.base_url} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, base_url: e.target.value } })} className={inputClassName} />
            <FieldErrors messages={fieldErrors('llm.base_url')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">请求路径</label>
            <input type="text" value={config.llm.request_path} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, request_path: e.target.value } })} className={inputClassName} disabled={config.llm.provider !== 'openai'} />
            <FieldErrors messages={fieldErrors('llm.request_path')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">模型名</label>
            <input type="text" value={config.llm.model} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, model: e.target.value } })} className={inputClassName} />
            <FieldErrors messages={fieldErrors('llm.model')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">API Key</label>
            <input type="password" value={config.llm.api_key} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, api_key: e.target.value } })} className={inputClassName} />
          </div>
        </div>

        <div>
          <label className="mb-1 block text-sm text-gray-400">超时时间（秒）</label>
          <input
            type="range"
            min="10"
            max="600"
            step="10"
            value={config.llm.timeout}
            onChange={(e) => setConfig({ ...config, llm: { ...config.llm, timeout: Number(e.target.value) || 120 } })}
            className="w-full accent-komgaPrimary"
          />
          <p className="mt-1 text-xs text-gray-500">当前：{config.llm.timeout}s</p>
          <FieldErrors messages={fieldErrors('llm.timeout')} />
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-semibold text-white">联通性测试</h3>
            <p className="mt-1 text-sm text-gray-400">保存前建议先确认地址、协议模式和模型名能正常响应。</p>
          </div>
          <button
            onClick={handleTestLLM}
            disabled={testingLLM}
            className="inline-flex items-center gap-2 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2 text-sm text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60"
          >
            {testingLLM ? <Terminal className="h-4 w-4 animate-spin" /> : <Terminal className="h-4 w-4" />}
            {testingLLM ? '测试中...' : '测试连接'}
          </button>
        </div>
        <input type="text" value={llmTestPrompt} onChange={(e) => setLlmTestPrompt(e.target.value)} className={inputClassName} />
        {llmTestResult && <pre className="max-h-56 overflow-auto rounded-lg border border-gray-800 bg-black/40 p-3 text-sm text-gray-200 whitespace-pre-wrap">{llmTestResult}</pre>}
      </section>

      <SettingsSaveBar saving={saving} label="保存 AI 配置" hint="这里只保存 LLM 和元数据抓取相关配置。" onSave={() => saveConfig('AI / 元数据配置已保存')} />
    </div>
  );
}

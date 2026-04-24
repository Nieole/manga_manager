import { Sparkles, Terminal } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsAIPage() {
  const { t } = useI18n();
  const { config, setConfig, fieldErrors, testingLLM, handleTestLLM, llmTestPrompt, setLlmTestPrompt, llmTestResult, saving, saveConfig } = useSettings();

  if (!config) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settings.ai.title')} description={t('settings.ai.description')} />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Sparkles className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.ai.modelTitle')}</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.provider')}</label>
            <select value={config.llm.provider} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, provider: e.target.value } })} className={inputClassName}>
              <option value="ollama">{t('settings.ai.provider.ollama')}</option>
              <option value="openai">{t('settings.ai.provider.openaiCompatible')}</option>
            </select>
            <FieldErrors messages={fieldErrors('llm.provider')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.protocolMode')}</label>
            <select
              value={config.llm.api_mode || 'responses'}
              onChange={(e) => setConfig({ ...config, llm: { ...config.llm, api_mode: e.target.value } })}
              className={inputClassName}
              disabled={config.llm.provider !== 'openai'}
            >
              <option value="responses">{t('settings.ai.apiMode.responses')}</option>
              <option value="chat_completions">{t('settings.ai.apiMode.chatCompletions')}</option>
            </select>
            <FieldErrors messages={fieldErrors('llm.api_mode')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.baseUrl')}</label>
            <input type="text" value={config.llm.base_url} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, base_url: e.target.value } })} className={inputClassName} />
            <FieldErrors messages={fieldErrors('llm.base_url')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.requestPath')}</label>
            <input type="text" value={config.llm.request_path} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, request_path: e.target.value } })} className={inputClassName} disabled={config.llm.provider !== 'openai'} />
            <FieldErrors messages={fieldErrors('llm.request_path')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.modelName')}</label>
            <input type="text" value={config.llm.model} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, model: e.target.value } })} className={inputClassName} />
            <FieldErrors messages={fieldErrors('llm.model')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.apiKey')}</label>
            <input type="password" value={config.llm.api_key} onChange={(e) => setConfig({ ...config, llm: { ...config.llm, api_key: e.target.value } })} className={inputClassName} />
          </div>
        </div>

        <div>
          <label className="mb-1 block text-sm text-gray-400">{t('settings.ai.timeout', { count: config.llm.timeout })}</label>
          <input
            type="range"
            min="10"
            max="600"
            step="10"
            value={config.llm.timeout}
            onChange={(e) => setConfig({ ...config, llm: { ...config.llm, timeout: Number(e.target.value) || 120 } })}
            className="w-full accent-komgaPrimary"
          />
          <p className="mt-1 text-xs text-gray-500">{t('settings.ai.timeoutCurrent', { count: config.llm.timeout })}</p>
          <FieldErrors messages={fieldErrors('llm.timeout')} />
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center justify-between gap-3">
          <div>
            <h3 className="text-lg font-semibold text-white">{t('settings.ai.connectivityTitle')}</h3>
            <p className="mt-1 text-sm text-gray-400">{t('settings.ai.connectivityDescription')}</p>
          </div>
          <button
            onClick={handleTestLLM}
            disabled={testingLLM}
            className="inline-flex items-center gap-2 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2 text-sm text-komgaPrimary hover:bg-komgaPrimary/20 disabled:opacity-60"
          >
            {testingLLM ? <Terminal className="h-4 w-4 animate-spin" /> : <Terminal className="h-4 w-4" />}
            {testingLLM ? t('settings.ai.testing') : t('settings.ai.testConnection')}
          </button>
        </div>
        <input type="text" value={llmTestPrompt} onChange={(e) => setLlmTestPrompt(e.target.value)} className={inputClassName} />
        {llmTestResult && <pre className="max-h-56 overflow-auto rounded-lg border border-gray-800 bg-black/40 p-3 text-sm text-gray-200 whitespace-pre-wrap">{llmTestResult}</pre>}
      </section>

      <SettingsSaveBar saving={saving} label={t('settings.ai.saveLabel')} hint={t('settings.ai.saveHint')} onSave={() => saveConfig(t('settings.ai.saved'))} />
    </div>
  );
}

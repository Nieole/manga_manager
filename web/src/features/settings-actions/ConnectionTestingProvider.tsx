import { createContext, useContext, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import { getClientLocale, translateInLocale, useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { useSettings } from '../settings-editor/SettingsEditorProvider';
import { useRequestLifetime } from '../../session/useRequestLifetime';

interface ConnectionTest { testingLLM: boolean; llmTestPrompt: string; setLlmTestPrompt: (value: string) => void; llmTestResult: string | null; handleTestLLM: () => Promise<void> }
const ConnectionContext = createContext<ConnectionTest | null>(null);

export function ConnectionTestingProvider({ children }: { children: ReactNode }) {
  const { config } = useSettings();
  const { t } = useI18n();
  const { showToast } = useToast();
  const [testingLLM, setTestingLLM] = useState(false);
  const [llmTestPrompt, setLlmTestPrompt] = useState(() =>
    translateInLocale(getClientLocale(), 'settings.ai.defaultTestPrompt'),
  );
  const [llmTestResult, setLlmTestResult] = useState<string | null>(null);
  const beginRequest = useRequestLifetime();
  const request = useRef(0);
  const inputKey = JSON.stringify({ connection: config?.llm, prompt: llmTestPrompt });
  const latestInput = useRef(inputKey);
  latestInput.current = inputKey;
  useEffect(() => {
    request.current += 1;
    setLlmTestResult(null);
    setTestingLLM(false);
  }, [inputKey]);
  const handleTestLLM = useCallback(async () => {
    if (!config) return;
    const alive = beginRequest();
    const id = ++request.current;
    const current = () => alive() && id === request.current && inputKey === latestInput.current;
    setTestingLLM(true);
    setLlmTestResult(null);
    try {
      const res = await apiClient.post('/api/system/test-llm', {
        ...config.llm,
        prompt: llmTestPrompt,
      });
      if (!current()) return;
      setLlmTestResult(res.data.response);
      showToast(t('settings.toast.llmTestSucceeded'), 'success');
    } catch (error: unknown) {
      if (!current()) return;
      const message = isAxiosError(error)
        ? error.response?.data?.error || error.message || t('settings.toast.llmTestFallback')
        : t('settings.toast.llmTestFallback');
      setLlmTestResult(`${t('common.errorPrefix')}: ${message}`);
      showToast(t('settings.toast.llmTestFailed'), 'error');
    } finally {
      if (current()) setTestingLLM(false);
    }
  }, [beginRequest, config, inputKey, llmTestPrompt, showToast, t]);

  return <ConnectionContext.Provider value={{ testingLLM, llmTestPrompt, setLlmTestPrompt, llmTestResult, handleTestLLM }}>{children}</ConnectionContext.Provider>;
}

export function useConnectionTest() {
  const value = useContext(ConnectionContext);
  if (!value) throw new Error('useConnectionTest must be used within ConnectionTestingProvider');
  return value;
}

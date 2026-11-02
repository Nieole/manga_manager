import { useCallback } from 'react';
import { apiClient } from '../../api/client';
import { useI18n } from '../../i18n/LocaleProvider';
import { useToast } from '../../components/ToastProvider';
import { useRequestLifetime } from '../../session/useRequestLifetime';

export function useSettingsMaintenance() {
  const { t } = useI18n();
  const { showToast } = useToast();
  const beginRequest = useRequestLifetime();
  const handleAction = useCallback(async (path: string, successMessage: string, errorMessage?: string) => {
    const alive = beginRequest();
    try {
      const res = await apiClient.post(path);
      if (!alive()) return;
      showToast(res.data.message || successMessage, 'success');
    } catch (error) {
      if (!alive()) return;
      console.error(error);
      showToast(errorMessage || t('settings.toast.actionFailed'), 'error');
    }
  }, [beginRequest, showToast, t]);

  return { handleAction, showToast };
}

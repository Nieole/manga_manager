import { useCallback, useState } from 'react';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';
import type { SeriesFailedTask } from '../types';


interface UseSeriesFailedTasksParams {
  seriesId: string | undefined;
  setFailedTasks: React.Dispatch<React.SetStateAction<SeriesFailedTask[]>>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesFailedTasks({ seriesId, setFailedTasks, showToast, t }: UseSeriesFailedTasksParams) {
  const [retryingTaskId, setRetryingTaskId] = useState<number | null>(null);

  const retry = useCallback(
    async (taskId: number) => {
      setRetryingTaskId(taskId);
      try {
        await apiClient.post(`/api/system/tasks/${taskId}/retry`);
        showToast(t('series.toast.retryTaskQueued'), 'success');
        if (seriesId) {
          const res = await apiClient.get(`/api/system/tasks?scope=series&scope_id=${seriesId}&status=failed&limit=5`);
          setFailedTasks(Array.isArray(res.data) ? res.data : []);
        }
      } catch (err) {
        showToast(getApiErrorMessage(err, t('series.toast.retryTaskFailed')), 'error');
      } finally {
        setRetryingTaskId(null);
      }
    },
    [seriesId, setFailedTasks, showToast, t],
  );

  return { retryingTaskId, retry };
}

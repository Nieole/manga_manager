import { useCallback, useState } from 'react';
import axios from 'axios';
import type { SeriesFailedTask } from '../types';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

interface UseSeriesFailedTasksParams {
  seriesId: string | undefined;
  setFailedTasks: React.Dispatch<React.SetStateAction<SeriesFailedTask[]>>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesFailedTasks({ seriesId, setFailedTasks, showToast, t }: UseSeriesFailedTasksParams) {
  const [retryingTaskKey, setRetryingTaskKey] = useState<string | null>(null);

  const retry = useCallback(
    async (taskKey: string) => {
      setRetryingTaskKey(taskKey);
      try {
        await axios.post(`/api/system/tasks/${encodeURIComponent(taskKey)}/retry`);
        showToast(t('series.toast.retryTaskQueued'), 'success');
        if (seriesId) {
          const res = await axios.get(`/api/system/tasks?scope=series&scope_id=${seriesId}&status=failed&limit=5`);
          setFailedTasks(Array.isArray(res.data) ? res.data : []);
        }
      } catch (err) {
        showToast(getApiErrorMessage(err, t('series.toast.retryTaskFailed')), 'error');
      } finally {
        setRetryingTaskKey(null);
      }
    },
    [seriesId, setFailedTasks, showToast, t],
  );

  return { retryingTaskKey, retry };
}

/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import { useCallback, useState } from 'react';
import axios from 'axios';
import { getApiErrorMessage } from '../../../api/client';
import type { SeriesFailedTask } from '../types';


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

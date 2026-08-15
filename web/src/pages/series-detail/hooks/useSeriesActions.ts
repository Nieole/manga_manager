import { useCallback, useState } from 'react';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage } from '../../../api/client';


interface UseSeriesActionsParams {
  seriesId: string | undefined;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesActions({ seriesId, showToast, t }: UseSeriesActionsParams) {
  const [isRescanning, setIsRescanning] = useState(false);
  const [isOpeningDirectory, setIsOpeningDirectory] = useState(false);

  const rescan = useCallback(async () => {
    if (!seriesId) return;
    setIsRescanning(true);
    try {
      await apiClient.post(`/api/series/${seriesId}/rescan`);
      showToast(t('series.toast.rescanQueued'), 'success');
    } catch (err) {
      showToast(`${t('series.toast.rescanFailed')}: ${getApiErrorMessage(err, t('series.toast.rescanFailed'))}`, 'error');
    } finally {
      setIsRescanning(false);
    }
  }, [seriesId, showToast, t]);

  const openDirectory = useCallback(async () => {
    if (!seriesId) return;
    setIsOpeningDirectory(true);
    try {
      await apiClient.post(`/api/series/${seriesId}/open-dir`);
      showToast(t('series.toast.openDirSuccess'), 'success');
    } catch (err) {
      showToast(`${t('series.toast.openDirFailed')}: ${getApiErrorMessage(err, t('series.toast.openDirFailed'))}`, 'error');
    } finally {
      setIsOpeningDirectory(false);
    }
  }, [seriesId, showToast, t]);

  const exportComicInfo = useCallback(() => {
    if (!seriesId) return;
    window.location.href = `/api/series/${seriesId}/comicinfo.zip`;
  }, [seriesId]);

  return { isRescanning, isOpeningDirectory, rescan, openDirectory, exportComicInfo };
}

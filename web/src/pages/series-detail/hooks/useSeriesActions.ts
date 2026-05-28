import { useCallback, useState } from 'react';
import axios from 'axios';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

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
      await axios.post(`/api/series/${seriesId}/rescan`);
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
      await axios.post(`/api/series/${seriesId}/open-dir`);
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

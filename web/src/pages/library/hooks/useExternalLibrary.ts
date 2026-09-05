import { useCallback, useEffect, useState } from 'react';
import { apiClient } from '../../../api/client';
import { useLatestRequest } from '../../../hooks/useLatestRequest';
import type { BrowseDirEntry, BrowseDrive } from '../../../components/layout/types';
import {
  type ExternalSession,
  type ExternalSessionCreateResponse,
  type ExternalSeriesStatus,
} from '../types';

const RECENT_PATHS_KEY = 'manga_manager_recent_external_paths';
const IGNORE_EXT_KEY = 'manga_manager_external_ignore_extension';

interface UseExternalLibraryParams {
  libId: string | undefined;
  refreshTrigger: number;
  allSeriesIds: number[];
  onError: (msg: string) => void;
}

interface UseExternalLibraryResult {
  externalSession: ExternalSession | null;
  externalSeriesMap: Record<number, ExternalSeriesStatus>;
  externalPath: string;
  externalIgnoreExtension: boolean;
  recentExternalPaths: string[];
  externalScanTaskKey: string | null;
  externalTransferTaskKey: string | null;
  startingExternalScan: boolean;
  startingTransfer: boolean;
  externalBrowsing: boolean;
  externalBrowseCurrent: string;
  externalBrowseParent: string;
  externalBrowseDirs: BrowseDirEntry[];
  externalBrowseDrives: BrowseDrive[];
  setExternalPath: (value: string) => void;
  setExternalIgnoreExtension: (value: boolean) => void;
  startExternalLibraryScan: () => Promise<void>;
  clearExternalSession: () => Promise<void>;
  openExternalDirectoryBrowser: () => void;
  navigateExternalDirectoryBrowser: (path: string) => void;
  closeExternalDirectoryBrowser: () => void;
  chooseCurrentExternalDirectory: () => void;
  fetchExternalSeriesStatus: (sessionId?: string, ids?: number[]) => Promise<void>;
  queryExternalSeriesStatus: (ids: number[]) => Promise<ExternalSeriesStatus[]>;
  startExternalTransfer: (
    seriesIds: number[],
    onSuccess: () => void,
  ) => Promise<void>;
}

export function useExternalLibrary({
  libId,
  refreshTrigger,
  allSeriesIds,
  onError,
}: UseExternalLibraryParams): UseExternalLibraryResult {
  const [externalSession, setExternalSession] = useState<ExternalSession | null>(null);
  const [externalSeriesMap, setExternalSeriesMap] = useState<Record<number, ExternalSeriesStatus>>({});
  const [externalPath, setExternalPath] = useState('');
  const [externalIgnoreExtension, setExternalIgnoreExtensionState] = useState(false);
  const [recentExternalPaths, setRecentExternalPaths] = useState<string[]>([]);
  const [startingExternalScan, setStartingExternalScan] = useState(false);
  const [startingTransfer, setStartingTransfer] = useState(false);
  const [externalScanTaskKey, setExternalScanTaskKey] = useState<string | null>(null);
  const [externalTransferTaskKey, setExternalTransferTaskKey] = useState<string | null>(null);

  const [externalBrowsing, setExternalBrowsing] = useState(false);
  const [externalBrowseDirs, setExternalBrowseDirs] = useState<BrowseDirEntry[]>([]);
  const [externalBrowseCurrent, setExternalBrowseCurrent] = useState('');
  const [externalBrowseParent, setExternalBrowseParent] = useState('');
  const [externalBrowseDrives, setExternalBrowseDrives] = useState<BrowseDrive[]>([]);
  const sessionRequest = useLatestRequest();

  // 1. 初始化：localStorage 读取
  useEffect(() => {
    try {
      const stored = localStorage.getItem(RECENT_PATHS_KEY);
      if (stored) {
        const parsed = JSON.parse(stored) as unknown;
        if (Array.isArray(parsed)) {
          setRecentExternalPaths(parsed.filter((item) => typeof item === 'string'));
        }
      }
      if (localStorage.getItem(IGNORE_EXT_KEY) === 'true') {
        setExternalIgnoreExtensionState(true);
      }
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    localStorage.setItem(IGNORE_EXT_KEY, externalIgnoreExtension ? 'true' : 'false');
  }, [externalIgnoreExtension]);

  // 2. 切库时清掉外部会话，并作废在飞的会话请求——1.5 秒一次的轮询响应回来时库可能已经换了，
  // 落回去就是把上一个库的外部会话装进新库的抽屉。
  useEffect(() => {
    sessionRequest.invalidate();
    setExternalSession(null);
    setExternalSeriesMap({});
    setExternalScanTaskKey(null);
    setExternalTransferTaskKey(null);
  }, [libId, sessionRequest]);

  const setExternalIgnoreExtension = useCallback((value: boolean) => {
    setExternalIgnoreExtensionState(value);
  }, []);

  const saveRecentExternalPath = useCallback((path: string) => {
    const normalized = path.trim();
    if (!normalized) return;
    setRecentExternalPaths((prev) => {
      const next = [normalized, ...prev.filter((item) => item !== normalized)].slice(0, 5);
      localStorage.setItem(RECENT_PATHS_KEY, JSON.stringify(next));
      return next;
    });
  }, []);

  const fetchExternalSession = useCallback(
    async (sessionId: string | undefined): Promise<ExternalSession | null> => {
      if (!libId || !sessionId) return null;
      const session = await sessionRequest.run(
        () => apiClient.get<ExternalSession>(
          `/api/libraries/${libId}/external-libraries/session/${sessionId}`,
          { params: { _ts: Date.now() } },
        ).then((res) => res.data),
        {
          onSuccess: (data) => setExternalSession(data),
          onError: (err) => {
            console.error('Failed to fetch external session', err);
            setExternalSession(null);
            setExternalSeriesMap({});
          },
        },
      );
      return session ?? null;
    },
    [libId, sessionRequest],
  );

  const requestSeriesStatus = useCallback(
    async (sessionId: string, ids: number[]): Promise<ExternalSeriesStatus[]> => {
      const res = await apiClient.get<ExternalSeriesStatus[]>(
        `/api/libraries/${libId}/external-libraries/session/${sessionId}/series`,
        { params: { ids: ids.join(','), _ts: Date.now() } },
      );
      return res.data || [];
    },
    [libId],
  );

  const fetchExternalSeriesStatus = useCallback(
    async (sessionId?: string, ids: number[] = allSeriesIds) => {
      const sid = sessionId ?? externalSession?.session_id;
      if (!libId || !sid || ids.length === 0) {
        setExternalSeriesMap({});
        return;
      }
      try {
        const items = await requestSeriesStatus(sid, ids);
        const next: Record<number, ExternalSeriesStatus> = {};
        items.forEach((item) => {
          next[item.series_id] = item;
        });
        setExternalSeriesMap(next);
      } catch (err) {
        console.error('Failed to fetch external series coverage', err);
        setExternalSeriesMap({});
      }
    },
    [libId, externalSession?.session_id, allSeriesIds, requestSeriesStatus],
  );

  // 把覆盖结果交回调用方而不写进 externalSeriesMap：那张表是当前页卡片的数据源，
  // 掺进别的页的系列会让「当前页外部命中情况」这条汇总算错。会话不可用时抛错，
  // 调用方必须区分「问不到」与「问到了、结果是没有」。
  const queryExternalSeriesStatus = useCallback(
    async (ids: number[]): Promise<ExternalSeriesStatus[]> => {
      const sid = externalSession?.session_id;
      if (!libId || !sid) throw new Error('external library session unavailable');
      if (ids.length === 0) return [];
      return requestSeriesStatus(sid, ids);
    },
    [libId, externalSession?.session_id, requestSeriesStatus],
  );

  const startExternalLibraryScan = useCallback(async () => {
    if (!libId || !externalPath.trim()) {
      onError('home.external.pickPathFirst');
      return;
    }
    setStartingExternalScan(true);
    try {
      const res = await apiClient.post<ExternalSessionCreateResponse>(
        `/api/libraries/${libId}/external-libraries/session`,
        {
          external_path: externalPath.trim(),
          ignore_extension: externalIgnoreExtension,
        },
      );
      setExternalSession(res.data.session);
      setExternalScanTaskKey(res.data.task_key);
      saveRecentExternalPath(externalPath.trim());
    } catch (err) {
      console.error('Failed to start external scan', err);
      onError('home.external.scanStartFailed');
    } finally {
      setStartingExternalScan(false);
    }
  }, [libId, externalPath, externalIgnoreExtension, saveRecentExternalPath, onError]);

  const clearExternalSession = useCallback(async () => {
    if (!libId || !externalSession?.session_id) {
      setExternalSession(null);
      setExternalSeriesMap({});
      return;
    }
    try {
      await apiClient.delete(
        `/api/libraries/${libId}/external-libraries/session/${externalSession.session_id}`,
      );
    } catch (err) {
      console.warn('Failed to delete external session', err);
    }
    setExternalSession(null);
    setExternalSeriesMap({});
    setExternalScanTaskKey(null);
    setExternalTransferTaskKey(null);
  }, [libId, externalSession?.session_id]);

  const openExternalDirectoryBrowser = useCallback(() => {
    setExternalBrowsing(true);
    apiClient
      .get('/api/browse-dirs')
      .then((res) => {
        setExternalBrowseDirs(res.data.dirs || []);
        setExternalBrowseCurrent(res.data.current);
        setExternalBrowseParent(res.data.parent);
        setExternalBrowseDrives(res.data.drives || []);
      })
      .catch((err) => console.error('Failed to open external directory browser', err));
  }, []);

  const navigateExternalDirectoryBrowser = useCallback((path: string) => {
    apiClient
      .get(`/api/browse-dirs?path=${encodeURIComponent(path)}`)
      .then((res) => {
        setExternalBrowseDirs(res.data.dirs || []);
        setExternalBrowseCurrent(res.data.current);
        setExternalBrowseParent(res.data.parent);
        setExternalBrowseDrives(res.data.drives || []);
      })
      .catch((err) => console.error('Failed to navigate external directory browser', err));
  }, []);

  const closeExternalDirectoryBrowser = useCallback(() => setExternalBrowsing(false), []);
  const chooseCurrentExternalDirectory = useCallback(() => {
    setExternalPath(externalBrowseCurrent);
    setExternalBrowsing(false);
  }, [externalBrowseCurrent]);

  const startExternalTransfer = useCallback(
    async (seriesIds: number[], onSuccess: () => void) => {
      if (!libId || !externalSession?.session_id) {
        onError('home.external.scanFirst');
        return;
      }
      if (externalSession.status !== 'ready') {
        onError('home.external.stillScanning');
        return;
      }
      setStartingTransfer(true);
      try {
        const res = await apiClient.post<{ task_key: string }>(
          `/api/libraries/${libId}/external-libraries/session/${externalSession.session_id}/transfer`,
          { series_ids: seriesIds },
        );
        setExternalTransferTaskKey(res.data.task_key);
        onSuccess();
      } catch (err) {
        console.error('Failed to start external transfer', err);
        onError('home.external.transferFailed');
      } finally {
        setStartingTransfer(false);
      }
    },
    [libId, externalSession, onError],
  );

  // 3. 监听后台任务进度（来自 task-progress 事件）
  useEffect(() => {
    const handler = (event: Event) => {
      const customEvent = event as CustomEvent<{ key?: string; type?: string; status?: string }>;
      const progress = customEvent.detail;
      if (!progress || !externalSession?.session_id || !libId) return;
      const isScan = progress.key === externalScanTaskKey && progress.type === 'scan_external_library';
      const isTransfer = progress.key === externalTransferTaskKey && progress.type === 'transfer_external_library';
      if (!isScan && !isTransfer) return;
      if (progress.status !== 'completed' && progress.status !== 'failed') return;

      fetchExternalSession(externalSession.session_id).then((session) => {
        if (session?.status === 'ready') {
          fetchExternalSeriesStatus(session.session_id);
        }
      });

      if (isScan) setExternalScanTaskKey(null);
      if (isTransfer) setExternalTransferTaskKey(null);
    };
    window.addEventListener('manga-manager:task-progress', handler as EventListener);
    return () => window.removeEventListener('manga-manager:task-progress', handler as EventListener);
  }, [
    externalSession?.session_id,
    externalScanTaskKey,
    externalTransferTaskKey,
    libId,
    fetchExternalSession,
    fetchExternalSeriesStatus,
  ]);

  // 4. 扫描中轮询
  useEffect(() => {
    if (!externalSession?.session_id || externalSession.status !== 'scanning') return;
    const timer = window.setInterval(() => {
      fetchExternalSession(externalSession.session_id).then((session) => {
        if (!session) return;
        if (session.status === 'ready') {
          fetchExternalSeriesStatus(session.session_id);
        }
      });
    }, 1500);
    return () => window.clearInterval(timer);
  }, [externalSession?.session_id, externalSession?.status, fetchExternalSession, fetchExternalSeriesStatus]);

  // 5. allSeries 变化或 refresh：会话就绪时重拉系列覆盖
  useEffect(() => {
    if (!externalSession?.session_id || externalSession.status !== 'ready') return;
    fetchExternalSeriesStatus(externalSession.session_id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [externalSession?.session_id, externalSession?.status, allSeriesIds.join(','), refreshTrigger]);

  return {
    externalSession,
    externalSeriesMap,
    externalPath,
    externalIgnoreExtension,
    recentExternalPaths,
    externalScanTaskKey,
    externalTransferTaskKey,
    startingExternalScan,
    startingTransfer,
    externalBrowsing,
    externalBrowseCurrent,
    externalBrowseParent,
    externalBrowseDirs,
    externalBrowseDrives,
    setExternalPath,
    setExternalIgnoreExtension,
    startExternalLibraryScan,
    clearExternalSession,
    openExternalDirectoryBrowser,
    navigateExternalDirectoryBrowser,
    closeExternalDirectoryBrowser,
    chooseCurrentExternalDirectory,
    fetchExternalSeriesStatus,
    queryExternalSeriesStatus,
    startExternalTransfer,
  };
}

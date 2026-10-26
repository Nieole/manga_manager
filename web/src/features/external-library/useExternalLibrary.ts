import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient } from '../../api/client';
import { useLatestRequest } from '../../hooks/useLatestRequest';
import type { BrowseDirEntry, BrowseDrive } from '../../components/layout/types';
import {
  type ExternalSession,
  type ExternalSessionCreateResponse,
  type ExternalSeriesStatus,
} from '../../pages/library/types';

import { useLibraryInvalidation, useLibraryRefresh, useSessionRecovery, useSessionRunPush } from '../../session/ApplicationSessionProvider';
import { isTerminalRunStatus } from '../../utils/runStatus';
import type { RunSnapshot } from '../../api/generated';

const RECENT_PATHS_KEY = 'manga_manager_recent_external_paths';
const IGNORE_EXT_KEY = 'manga_manager_external_ignore_extension';

interface UseExternalLibraryParams {
  libId: string | undefined;
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
  allSeriesIds,
  onError,
}: UseExternalLibraryParams): UseExternalLibraryResult {
  const refreshTrigger = useLibraryRefresh({ libraryId: libId });
  const invalidateLibrary = useLibraryInvalidation();
  const currentScope = useRef({ libId });
  if (currentScope.current.libId !== libId) currentScope.current = { libId };
  const scope = currentScope.current;
  const isCurrent = useCallback(() => currentScope.current === scope, [scope]);
  const [coverageRevision, setCoverageRevision] = useState(0);
  const [externalSession, setExternalSession] = useState<ExternalSession | null>(null);
  const activeSessionId = useRef(externalSession?.session_id);
  activeSessionId.current = externalSession?.session_id;
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
  const coverageRequest = useLatestRequest();
  const browserRequest = useLatestRequest();
  useEffect(() => {
    currentScope.current = scope;
    return () => {
      if (currentScope.current === scope) currentScope.current = { libId: undefined };
      sessionRequest.invalidate();
      coverageRequest.invalidate();
      browserRequest.invalidate();
    };
  }, [scope, sessionRequest, coverageRequest, browserRequest]);

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
    setStartingExternalScan(false);
    setStartingTransfer(false);
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
          onSuccess: (data) => { if (isCurrent()) setExternalSession(data); },
          onError: (err) => {
            if (!isCurrent()) return;
            console.error('Failed to fetch external session', err);
            setExternalSession(null);
            setExternalSeriesMap({});
          },
        },
      );
      return session ?? null;
    },
    [libId, sessionRequest, isCurrent],
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
        const items = await coverageRequest.run(() => requestSeriesStatus(sid, ids));
        if (!items || !isCurrent()) return;
        const next: Record<number, ExternalSeriesStatus> = {};
        items.forEach((item) => {
          next[item.series_id] = item;
        });
        setExternalSeriesMap(next);
      } catch (err) {
        if (!isCurrent()) return;
        console.error('Failed to fetch external series coverage', err);
        setExternalSeriesMap({});
      }
    },
    [libId, externalSession?.session_id, allSeriesIds, requestSeriesStatus, coverageRequest, isCurrent],
  );

  // 把覆盖结果交回调用方而不写进 externalSeriesMap：那张表是当前页卡片的数据源，
  // 掺进别的页的系列会让「当前页外部命中情况」这条汇总算错。会话不可用时抛错，
  // 调用方必须区分「问不到」与「问到了、结果是没有」。
  const queryExternalSeriesStatus = useCallback(
    async (ids: number[]): Promise<ExternalSeriesStatus[]> => {
      const sid = externalSession?.session_id;
      if (!libId || !sid) throw new Error('external library session unavailable');
      if (ids.length === 0) return [];
      const items = await requestSeriesStatus(sid, ids);
      if (!isCurrent() || activeSessionId.current !== sid) throw new Error('External library scope changed');
      return items;
    },
    [libId, externalSession?.session_id, requestSeriesStatus, isCurrent],
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
      if (!isCurrent()) return;
      sessionRequest.invalidate();
      coverageRequest.invalidate();
      setExternalSession(res.data.session);
      setExternalScanTaskKey(res.data.task_key);
      saveRecentExternalPath(externalPath.trim());
    } catch (err) {
      if (!isCurrent()) return;
      console.error('Failed to start external scan', err);
      onError('home.external.scanStartFailed');
    } finally {
      if (isCurrent()) setStartingExternalScan(false);
    }
  }, [libId, externalPath, externalIgnoreExtension, saveRecentExternalPath, onError, isCurrent, coverageRequest, sessionRequest]);

  const clearExternalSession = useCallback(async () => {
    const sessionId = externalSession?.session_id;
    currentScope.current = { libId };
    sessionRequest.invalidate();
    coverageRequest.invalidate();
    setExternalSession(null);
    setExternalSeriesMap({});
    setExternalScanTaskKey(null);
    setExternalTransferTaskKey(null);
    setStartingExternalScan(false);
    setStartingTransfer(false);
    if (!libId || !sessionId) return;
    try {
      await apiClient.delete(`/api/libraries/${libId}/external-libraries/session/${sessionId}`);
    } catch (err) {
      console.warn('Failed to delete external session', err);
    }
  }, [libId, externalSession?.session_id, sessionRequest, coverageRequest]);

  const browse = useCallback((path?: string) => {
    void browserRequest.run(() => apiClient.get(path ? `/api/browse-dirs?path=${encodeURIComponent(path)}` : '/api/browse-dirs'), {
      onSuccess: res => {
        if (!isCurrent()) return;
        setExternalBrowseDirs(res.data.dirs || []);
        setExternalBrowseCurrent(res.data.current);
        setExternalBrowseParent(res.data.parent);
        setExternalBrowseDrives(res.data.drives || []);
      },
      onError: err => { if (isCurrent()) console.error('Failed to browse external directory', err); },
    });
  }, [browserRequest, isCurrent]);
  const openExternalDirectoryBrowser = useCallback(() => { setExternalBrowsing(true); browse(); }, [browse]);
  const navigateExternalDirectoryBrowser = browse;

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
        if (!isCurrent() || activeSessionId.current !== externalSession.session_id) return;
        setExternalTransferTaskKey(res.data.task_key);
        onSuccess();
      } catch (err) {
        if (!isCurrent()) return;
        console.error('Failed to start external transfer', err);
        onError('home.external.transferFailed');
      } finally {
        if (isCurrent()) setStartingTransfer(false);
      }
    },
    [libId, externalSession, onError, isCurrent],
  );

  const refreshSession = useCallback(async () => {
    return fetchExternalSession(externalSession?.session_id);
  }, [fetchExternalSession, externalSession?.session_id]);

  const receiveRun = useCallback(async (run: RunSnapshot, alreadyRefreshed = false) => {
    if (run.scope_id != null && String(run.scope_id) !== libId) return;
    if (!externalSession?.session_id || run.params?.session_id !== externalSession.session_id || !isTerminalRunStatus(run.status)) return;
    const scan = externalScanTaskKey !== null && run.type === 'scan_external_library';
    const transfer = externalTransferTaskKey !== null && run.type === 'transfer_external_library';
    if (!scan && !transfer) return;
    if (scan) setExternalScanTaskKey(null);
    if (transfer) setExternalTransferTaskKey(null);
    if (!alreadyRefreshed) await refreshSession();
    if (!isCurrent()) return;
    setCoverageRevision(value => value + 1);
    if (transfer) invalidateLibrary({ libraryId: libId, areas: ['catalog', 'collections', 'organize', 'stats'] });
  }, [externalSession?.session_id, externalScanTaskKey, externalTransferTaskKey, refreshSession, invalidateLibrary, libId, isCurrent]);
  useSessionRunPush(frame => { if (frame.run) void receiveRun(frame.run); });

  const recover = useCallback(async () => {
    if (!externalSession?.session_id) return;
    const session = await refreshSession();
    if (!isCurrent()) return;
    if (!externalTransferTaskKey || !libId) {
      if (session?.status === 'ready') setCoverageRevision(value => value + 1);
      return;
    }
    try {
      const { data } = await apiClient.get<RunSnapshot[]>(`/api/system/tasks?type=transfer_external_library&scope=library&scope_id=${libId}&limit=20`);
      if (!isCurrent()) return;
      const run = Array.isArray(data) ? data.find(item => item.params?.session_id === externalSession.session_id) : undefined;
      if (run && isTerminalRunStatus(run.status)) await receiveRun(run, true);
      else if (session?.status === 'ready') setCoverageRevision(value => value + 1);
    } catch (err) { if (isCurrent()) console.warn('Failed to recover external transfer', err); }
  }, [externalSession?.session_id, externalTransferTaskKey, libId, refreshSession, receiveRun, isCurrent]);
  useSessionRecovery(() => { void recover(); });

  useEffect(() => {
    if (!externalSession?.session_id || (externalSession.status !== 'scanning' && !externalTransferTaskKey)) return;
    const timer = window.setInterval(() => { void recover(); }, 1500);
    return () => window.clearInterval(timer);
  }, [externalSession?.session_id, externalSession?.status, externalTransferTaskKey, recover]);

  // 5. allSeries 变化或 refresh：会话就绪时重拉系列覆盖
  useEffect(() => {
    if (!externalSession?.session_id || externalSession.status !== 'ready') return;
    fetchExternalSeriesStatus(externalSession.session_id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [externalSession?.session_id, externalSession?.status, allSeriesIds.join(','), refreshTrigger, coverageRevision]);

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

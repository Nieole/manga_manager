import { useCallback, createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { RunPush } from '../api/generated';
import { parseRunPush, trackRunPush } from '../utils/runPush';
import { useEventStream } from './useEventStream';
import { useAuth } from '../auth/AuthProvider';

export type LibraryArea = 'catalog' | 'collections' | 'organize' | 'reviews' | 'stats' | 'reading-lists';
export interface LibraryChange {
  libraryId?: string;
  seriesId?: string;
  areas?: LibraryArea[];
}
interface Session {
  refresh: Record<string, number>;
  invalidateLibrary: (change?: LibraryChange) => void;
  runs: Set<(frame: RunPush) => void>;
  recoveries: Set<() => void>;
}
const SessionContext = createContext<Session | null>(null);

/** 身份/角色边界同时释放旧连接、订阅和功能投影；不触碰离线归属与设备偏好。 */
export function ApplicationSessionScope({ children }: { children: ReactNode }) {
  const { user, isAdmin } = useAuth();
  return <ApplicationSessionProvider identity={`${user?.id ?? 'anonymous'}:${user?.role ?? ''}`} isAdmin={isAdmin}>{children}</ApplicationSessionProvider>;
}

export function ApplicationSessionProvider({ identity, isAdmin, children }: { identity: string; isAdmin: boolean; children: ReactNode }) {
  return <SessionInstance key={identity} isAdmin={isAdmin}>{children}</SessionInstance>;
}

function SessionInstance({ isAdmin, children }: { isAdmin: boolean; children: ReactNode }) {
  const [refresh, setRefresh] = useState<Record<string, number>>({});
  const [runs] = useState(() => new Set<(frame: RunPush) => void>());
  const [recoveries] = useState(() => new Set<() => void>());
  const lastSequence = useRef<number | null>(null);
  const invalidateLibrary = useCallback((change?: LibraryChange) => setRefresh(value => {
    const next = { ...value };
    const keys = new Set<string>();
    if (!change) keys.add('*');
    else {
      for (const area of change.areas ?? ['catalog']) {
        if (area === 'catalog') {
          keys.add('catalog-any');
          if (change.libraryId) keys.add(`library:${change.libraryId}`);
          if (change.seriesId) keys.add(`series:${change.seriesId}`);
          if (!change.libraryId && !change.seriesId) keys.add('catalog');
        } else keys.add(area);
      }
    }
    keys.forEach(key => { next[key] = (next[key] ?? 0) + 1; });
    return next;
  }), []);
  const session = useMemo(() => ({ refresh, runs, recoveries, invalidateLibrary }), [refresh, runs, recoveries, invalidateLibrary]);
  useEventStream('/api/events', {
    onReconnect() {
      lastSequence.current = null;
      if (isAdmin) recoveries.forEach(listener => listener());
    },
    onMessage(data) {
      if (data === 'refresh') {
        invalidateLibrary();
        return;
      }
      if (!isAdmin) return;
      const frame = parseRunPush(data);
      if (!frame) return;
      const tracked = trackRunPush(lastSequence.current, frame);
      lastSequence.current = tracked.sequence;
      runs.forEach(listener => listener(frame));
      if (tracked.gap) recoveries.forEach(listener => listener());
    },
  });
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error('Application session scope is required');
  return session;
}

const ignoreInvalidation = () => {};
export function useLibraryInvalidation() { return useContext(SessionContext)?.invalidateLibrary ?? ignoreInvalidation; }

/** 只发布失效水位，查询结果和视图草稿始终由功能消费者持有。 */
export function useLibraryRefresh(scope: { libraryId?: string; seriesId?: string; area?: LibraryArea } = {}) {
  const versions = useContext(SessionContext)?.refresh ?? {};
  const area = scope.area ?? 'catalog';
  const keys = new Set(['*']);
  if (area !== 'catalog') {
    keys.add(area);
    if (area !== 'reviews') keys.add('catalog-any');
  } else {
    keys.add('catalog');
    if (scope.libraryId) keys.add(`library:${scope.libraryId}`);
    if (scope.seriesId) keys.add(`series:${scope.seriesId}`);
    if (!scope.libraryId && !scope.seriesId) keys.add('catalog-any');
  }
  return [...keys].reduce((total, key) => total + (versions[key] ?? 0), 0);
}

export function useSessionRunPush(onPush: (frame: RunPush) => void) {
  const { runs } = useSession();
  const callback = useRef(onPush);
  callback.current = onPush;
  useEffect(() => {
    const listener = (frame: RunPush) => callback.current(frame);
    runs.add(listener);
    return () => { runs.delete(listener); };
  }, [runs]);
}

/** 通道恢复或投递链缺口要求功能消费者补拉；消费者无需理解序号。 */
export function useSessionRecovery(onRecovery: () => void) {
  const { recoveries } = useSession();
  const callback = useRef(onRecovery);
  callback.current = onRecovery;
  useEffect(() => {
    const listener = () => callback.current();
    recoveries.add(listener);
    return () => { recoveries.delete(listener); };
  }, [recoveries]);
}

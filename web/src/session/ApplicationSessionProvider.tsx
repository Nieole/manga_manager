import { useCallback, createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { RunPush } from '../api/generated';
import { parseRunPush } from '../utils/runPush';
import { useEventStream } from './useEventStream';
import { useAuth } from '../auth/AuthProvider';

interface Session {
  refresh: number;
  invalidateLibrary: () => void;
  runs: Set<(frame: RunPush) => void>;
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
  const [refresh, setRefresh] = useState(0);
  const [runs] = useState(() => new Set<(frame: RunPush) => void>());
  const invalidateLibrary = useCallback(() => setRefresh(value => value + 1), []);
  const session = useMemo(() => ({ refresh, runs, invalidateLibrary }), [refresh, runs, invalidateLibrary]);
  useEventStream('/api/events', {
    onMessage(data) {
      if (data === 'refresh') {
        invalidateLibrary();
        return;
      }
      if (!isAdmin) return;
      const frame = parseRunPush(data);
      if (!frame) return;
      runs.forEach(listener => listener(frame));
      // 未迁消费者的单向桥只转发同一条连接解码后的帧。
      window.dispatchEvent(new CustomEvent('manga-manager:run-push', { detail: frame }));
      if (frame.run) window.dispatchEvent(new CustomEvent('manga-manager:run-snapshot', { detail: frame.run }));
    },
  });
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error('Application session scope is required');
  return session;
}

export function useLibraryInvalidation() { return useSession().invalidateLibrary; }

export function useLibraryRefresh() { return useSession().refresh; }

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

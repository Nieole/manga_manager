import { useCallback, useEffect, useRef } from 'react';
import { useSessionActive } from './ApplicationSessionProvider';

// An async consumer may publish only during the mount and application session that started it.
export function useRequestLifetime() {
  const active = useSessionActive();
  const lifetime = useRef(0);
  useEffect(() => () => { lifetime.current += 1; }, []);
  return useCallback(() => {
    const generation = lifetime.current;
    return () => generation === lifetime.current && active();
  }, [active]);
}

export function useLatestRequest() {
  const begin = useRequestLifetime();
  const requests = useRef(0);
  return useCallback(() => {
    const alive = begin();
    const id = ++requests.current;
    return () => alive() && id === requests.current;
  }, [begin]);
}

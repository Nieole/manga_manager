import { useCallback, useEffect, useRef, useState } from 'react';

const DEFAULT_IDLE_MS = 5000;

interface UseReaderImmersiveOptions {
  enabled?: boolean;
  idleMs?: number;
  forcedVisible?: boolean;
}

export interface UseReaderImmersiveResult {
  visible: boolean;
  show: () => void;
  hide: () => void;
  toggle: () => void;
  notifyActivity: () => void;
}

export function useReaderImmersive({
  enabled = true,
  idleMs = DEFAULT_IDLE_MS,
  forcedVisible = false,
}: UseReaderImmersiveOptions = {}): UseReaderImmersiveResult {
  const [visible, setVisible] = useState(true);
  const visibleRef = useRef(visible);
  const timerRef = useRef<number | null>(null);
  const enabledRef = useRef(enabled);
  const forcedRef = useRef(forcedVisible);

  useEffect(() => {
    visibleRef.current = visible;
  }, [visible]);

  useEffect(() => {
    enabledRef.current = enabled;
  }, [enabled]);

  useEffect(() => {
    forcedRef.current = forcedVisible;
    if (forcedVisible) {
       
      setVisible(true);
      if (timerRef.current != null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    }
  }, [forcedVisible]);

  const clearTimer = useCallback(() => {
    if (timerRef.current != null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const scheduleHide = useCallback(() => {
    clearTimer();
    if (!enabledRef.current || forcedRef.current) return;
    timerRef.current = window.setTimeout(() => {
      setVisible(false);
      timerRef.current = null;
    }, idleMs);
  }, [clearTimer, idleMs]);

  const show = useCallback(() => {
    setVisible(true);
    scheduleHide();
  }, [scheduleHide]);

  const hide = useCallback(() => {
    if (forcedRef.current) return;
    clearTimer();
    setVisible(false);
  }, [clearTimer]);

  const toggle = useCallback(() => {
    setVisible((prev) => {
      const next = !prev;
      if (next) {
        scheduleHide();
      } else {
        clearTimer();
      }
      return forcedRef.current ? true : next;
    });
  }, [clearTimer, scheduleHide]);

  const notifyActivity = useCallback(() => {
    if (forcedRef.current) {
      setVisible(true);
      return;
    }
    setVisible(true);
    scheduleHide();
  }, [scheduleHide]);

  useEffect(() => {
    if (!enabled) {
      clearTimer();
       
      setVisible(true);
      return undefined;
    }
    scheduleHide();
    return () => clearTimer();
  }, [clearTimer, enabled, scheduleHide]);

  useEffect(() => {
    if (!enabled) return undefined;

    const handleActivity = (event: Event) => {
      if (event.type === 'keydown') {
        const evt = event as KeyboardEvent;
        if (evt.key === 'Escape') return;
      }
      if (forcedRef.current) {
        setVisible(true);
        return;
      }
      // If the menu is currently hidden, do not wake it up on mouse move/scroll.
      // It should only be woken up by explicit clicks (onCenterTap calling toggle/show).
      if (!visibleRef.current) {
        return;
      }
      // If it is already visible, moving the mouse keeps it awake.
      scheduleHide();
    };

    window.addEventListener('mousemove', handleActivity, { passive: true });
    window.addEventListener('keydown', handleActivity);
    window.addEventListener('touchstart', handleActivity, { passive: true });
    window.addEventListener('wheel', handleActivity, { passive: true });

    return () => {
      window.removeEventListener('mousemove', handleActivity);
      window.removeEventListener('keydown', handleActivity);
      window.removeEventListener('touchstart', handleActivity);
      window.removeEventListener('wheel', handleActivity);
    };
  }, [enabled, scheduleHide]);

  return { visible, show, hide, toggle, notifyActivity };
}

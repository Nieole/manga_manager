/**
 * 取数世代号的唯一产地：切换选中项后只有最新一次的响应能落到界面上。run 领一个新世代并作废在飞的旧请求，
 * follow 跟当前世代（会被后发的作废，但不作废别人），invalidate 用在选中项被清空、没有后继请求的地方。
 * 成功 / 失败 / 收尾三条路径一律由它把关，调用方想漏也漏不掉——loading 只由当前世代关掉。
 */

import { useCallback, useMemo, useRef } from 'react';

export interface LatestRequestHandlers<T> {
  onSuccess?: (value: T) => void;
  onError?: (error: unknown) => void;
  onSettled?: () => void;
}

export interface LatestRequest {
  // 返回值在响应被判为迟到时是 undefined，调用方据此知道这一份不该再用。
  run: <T>(task: () => Promise<T>, handlers?: LatestRequestHandlers<T>) => Promise<T | undefined>;
  follow: <T>(task: () => Promise<T>, handlers?: LatestRequestHandlers<T>) => Promise<T | undefined>;
  invalidate: () => void;
}

export function useLatestRequest(): LatestRequest {
  const generationRef = useRef(0);

  const settle = useCallback(
    async <T>(generation: number, task: () => Promise<T>, handlers: LatestRequestHandlers<T>) => {
      try {
        const value = await task();
        if (generation !== generationRef.current) return undefined;
        handlers.onSuccess?.(value);
        return value;
      } catch (error) {
        if (generation !== generationRef.current) return undefined;
        handlers.onError?.(error);
        return undefined;
      } finally {
        // 迟到的失败不能把新请求的转圈提前关掉，那是另一种串台。
        if (generation === generationRef.current) handlers.onSettled?.();
      }
    },
    [],
  );

  const run = useCallback(
    <T>(task: () => Promise<T>, handlers: LatestRequestHandlers<T> = {}) => {
      generationRef.current += 1;
      return settle(generationRef.current, task, handlers);
    },
    [settle],
  );

  const follow = useCallback(
    <T>(task: () => Promise<T>, handlers: LatestRequestHandlers<T> = {}) =>
      settle(generationRef.current, task, handlers),
    [settle],
  );

  const invalidate = useCallback(() => {
    generationRef.current += 1;
  }, []);

  return useMemo(() => ({ run, follow, invalidate }), [run, follow, invalidate]);
}

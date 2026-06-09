/**
 * 业务说明：本文件是业务实现，属于前端工具函数层，负责封装性能统计、格式化、请求辅助和跨页面复用逻辑。
 * 它支撑业务页面保持简洁，并统一处理浏览器端的边界行为。
 * 维护时应关注副作用范围、异常返回、性能开销和调用方的业务语义。
 */

import axios, { type InternalAxiosRequestConfig } from 'axios';

const FIRST_SCREEN_CAPTURE_MS = 3500;
const FIRST_SCREEN_FORCE_FINALIZE_MS = 10000;
const MAX_STORED_METRICS = 20;
const FIRST_SCREEN_STORAGE_KEY = 'manga_manager_first_screen_metrics';
const SERIES_RENDER_STORAGE_KEY = 'manga_manager_series_list_render_metrics';

export interface FirstScreenMetric {
  id: number;
  path: string;
  started_at: string;
  finalized_at: string;
  duration_ms: number;
  request_count: number;
  failed_request_count: number;
  slow_request_count: number;
  total_bytes: number;
  max_ms: number;
  inflight_at_finalize: number;
}

export interface SeriesListRenderMetric {
  path: string;
  library_id: string;
  page: number;
  page_size: number;
  sort: string;
  filters: string;
  item_count: number;
  total_count: number;
  request_ms: number;
  render_ms: number;
  total_ms: number;
  measured_at: string;
}

export interface FrontendPerformanceSnapshot {
  firstScreens: FirstScreenMetric[];
  seriesListRenders: SeriesListRenderMetric[];
}

interface RouteSession {
  id: number;
  path: string;
  startedAt: number;
  startedIso: string;
  requestCount: number;
  failedRequestCount: number;
  slowRequestCount: number;
  totalBytes: number;
  maxMs: number;
  inflight: number;
  finalized: boolean;
  captureTimer?: number;
  forceTimer?: number;
}

interface RequestMeta {
  startedAt: number;
  sessionId: number;
  counted: boolean;
}

const requestMeta = new WeakMap<InternalAxiosRequestConfig, RequestMeta>();
let initialized = false;
let nextSessionId = 1;
let currentSession: RouteSession | null = null;

export function initializeFrontendPerformance() {
  if (initialized || typeof window === 'undefined') return;
  initialized = true;

  startRouteSession();
  patchHistoryMethod('pushState');
  patchHistoryMethod('replaceState');
  window.addEventListener('popstate', () => window.setTimeout(startRouteSession, 0));

  axios.interceptors.request.use((config) => {
    const session = currentSession;
    const startedAt = performance.now();
    const counted = Boolean(session && !session.finalized && isApiUrl(config.url) && startedAt - session.startedAt <= FIRST_SCREEN_CAPTURE_MS);

    if (session && counted) {
      session.requestCount += 1;
      session.inflight += 1;
    }

    requestMeta.set(config, {
      startedAt,
      sessionId: session?.id ?? 0,
      counted,
    });

    return config;
  });

  axios.interceptors.response.use(
    (response) => {
      completeRequest(response.config, response.status, response.headers?.['content-length']);
      return response;
    },
    (error) => {
      if (error?.config) {
        completeRequest(error.config, error.response?.status ?? 0, error.response?.headers?.['content-length']);
      }
      return Promise.reject(error);
    },
  );
}

export function getFrontendPerformanceSnapshot(): FrontendPerformanceSnapshot {
  return {
    firstScreens: readStoredMetrics<FirstScreenMetric>(FIRST_SCREEN_STORAGE_KEY),
    seriesListRenders: readStoredMetrics<SeriesListRenderMetric>(SERIES_RENDER_STORAGE_KEY),
  };
}

export function recordSeriesListRenderMetric(metric: SeriesListRenderMetric) {
  if (typeof window === 'undefined') return;
  prependMetric(SERIES_RENDER_STORAGE_KEY, metric);
  window.dispatchEvent(new CustomEvent('manga-manager:frontend-performance', {
    detail: getFrontendPerformanceSnapshot(),
  }));
}

function patchHistoryMethod(method: 'pushState' | 'replaceState') {
  const original = window.history[method];
  window.history[method] = function patchedHistoryMethod(...args) {
    const result = original.apply(this, args);
    window.setTimeout(startRouteSession, 0);
    return result;
  };
}

function startRouteSession() {
  finalizeCurrentSession(true);

  const session: RouteSession = {
    id: nextSessionId++,
    path: `${window.location.pathname}${window.location.search}`,
    startedAt: performance.now(),
    startedIso: new Date().toISOString(),
    requestCount: 0,
    failedRequestCount: 0,
    slowRequestCount: 0,
    totalBytes: 0,
    maxMs: 0,
    inflight: 0,
    finalized: false,
  };

  session.captureTimer = window.setTimeout(() => finalizeCurrentSession(false), FIRST_SCREEN_CAPTURE_MS);
  session.forceTimer = window.setTimeout(() => finalizeCurrentSession(true), FIRST_SCREEN_FORCE_FINALIZE_MS);
  currentSession = session;
}

function completeRequest(config: InternalAxiosRequestConfig, status: number, contentLength: unknown) {
  const meta = requestMeta.get(config);
  if (!meta?.counted) return;

  const session = currentSession;
  if (!session || session.id !== meta.sessionId || session.finalized) return;

  const durationMs = Math.round(performance.now() - meta.startedAt);
  session.inflight = Math.max(0, session.inflight - 1);
  session.maxMs = Math.max(session.maxMs, durationMs);

  if (durationMs >= 500) {
    session.slowRequestCount += 1;
  }
  if (status >= 400 || status === 0) {
    session.failedRequestCount += 1;
  }

  const parsedBytes = Number(contentLength);
  if (Number.isFinite(parsedBytes) && parsedBytes > 0) {
    session.totalBytes += parsedBytes;
  }

  if (performance.now() - session.startedAt >= FIRST_SCREEN_CAPTURE_MS) {
    finalizeCurrentSession(false);
  }
}

function finalizeCurrentSession(force: boolean) {
  const session = currentSession;
  if (!session || session.finalized) return;

  const elapsed = performance.now() - session.startedAt;
  if (!force && session.inflight > 0 && elapsed < FIRST_SCREEN_FORCE_FINALIZE_MS) {
    return;
  }

  session.finalized = true;
  if (session.captureTimer) window.clearTimeout(session.captureTimer);
  if (session.forceTimer) window.clearTimeout(session.forceTimer);

  const metric: FirstScreenMetric = {
    id: session.id,
    path: session.path,
    started_at: session.startedIso,
    finalized_at: new Date().toISOString(),
    duration_ms: Math.round(elapsed),
    request_count: session.requestCount,
    failed_request_count: session.failedRequestCount,
    slow_request_count: session.slowRequestCount,
    total_bytes: session.totalBytes,
    max_ms: session.maxMs,
    inflight_at_finalize: session.inflight,
  };

  prependMetric(FIRST_SCREEN_STORAGE_KEY, metric);
  window.dispatchEvent(new CustomEvent('manga-manager:frontend-performance', {
    detail: getFrontendPerformanceSnapshot(),
  }));
}

function isApiUrl(rawUrl: string | undefined) {
  if (!rawUrl) return false;
  try {
    const url = new URL(rawUrl, window.location.origin);
    return url.pathname.startsWith('/api/');
  } catch {
    return rawUrl.startsWith('/api/');
  }
}

function prependMetric<T>(key: string, metric: T) {
  const next = [metric, ...readStoredMetrics<T>(key)].slice(0, MAX_STORED_METRICS);
  try {
    localStorage.setItem(key, JSON.stringify(next));
  } catch {
    // Storage failures should not affect normal reading flows.
  }
}

function readStoredMetrics<T>(key: string): T[] {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed as T[] : [];
  } catch {
    return [];
  }
}

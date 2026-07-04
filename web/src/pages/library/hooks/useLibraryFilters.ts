/**
 * 业务说明：本文件是业务实现，属于前端资料库页面，负责漫画列表、筛选排序、批量操作、扫描入口和外部库状态展示。
 * 它是用户管理本地漫画资产的主工作台，需要同步 URL 状态、后端分页和本地交互状态。
 * 维护时应关注查询参数、选择状态、空结果提示、任务刷新和大列表渲染性能。
 */

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { DEFAULT_PAGE_SIZE } from '../types';

export interface SavedLibrarySettings {
  activeTag?: string | null;
  activeAuthor?: string | null;
  activeStatus?: string | null;
  activeLetter?: string | null;
  sortByField?: string;
  sortDir?: string;
  keyword?: string;
  pageSize?: number;
  page?: number;
}

interface UseLibraryFiltersResult {
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  keyword: string;
  page: number;
  pageSize: number;
  settingsReady: boolean;
  setActiveTag: (value: string | null) => void;
  setActiveAuthor: (value: string | null) => void;
  setActiveStatus: (value: string | null) => void;
  setActiveLetter: (value: string | null) => void;
  setSortByField: (value: string) => void;
  setSortDir: (value: string) => void;
  setKeyword: (value: string) => void;
  setPage: (value: number) => void;
  setPageSize: (value: number) => void;
  resetAll: () => void;
  applySnapshot: (snapshot: Partial<SavedLibrarySettings>) => void;
}

const VALID_SORT_DIRS = new Set(['asc', 'desc']);
const SUPPORTS_CURSOR_FIELDS = new Set(['name', 'updated', 'created', 'favorite']);

const settingsStorageKey = (libId: string) => `library:${libId}:settings`;

function readStoredSettings(libId: string): SavedLibrarySettings | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = window.localStorage.getItem(settingsStorageKey(libId));
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? (parsed as SavedLibrarySettings) : null;
  } catch {
    return null;
  }
}

function writeStoredSettings(libId: string, payload: SavedLibrarySettings) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(settingsStorageKey(libId), JSON.stringify(payload));
  } catch {
    // 配额或隐私模式下写入失败不影响 UI
  }
}

export function supportsCursorPagination(field: string) {
  return SUPPORTS_CURSOR_FIELDS.has(field);
}

/**
 * useLibraryFilters：把过滤、排序、分页从 Home.tsx 抽出，并与服务端持久化、
 * URL query 同步。其余 UI 不再直接持有这部分 state。
 */
export function useLibraryFilters({ libId }: { libId: string | undefined }): UseLibraryFiltersResult {
  const [searchParams, setSearchParams] = useSearchParams();
  const [activeTag, setActiveTag] = useState<string | null>(null);
  const [activeAuthor, setActiveAuthor] = useState<string | null>(null);
  const [activeStatus, setActiveStatus] = useState<string | null>(null);
  const [activeLetter, setActiveLetter] = useState<string | null>(null);
  const [sortByField, setSortByField] = useState<string>('name');
  const [sortDir, setSortDir] = useState<string>('asc');
  const [keyword, setKeyword] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const [settingsReady, setSettingsReady] = useState(false);
  const [settingsReadyLibId, setSettingsReadyLibId] = useState<string | null>(null);
  const lastWrittenSettings = useRef<string>('');
  const currentSettingsReady = settingsReady && settingsReadyLibId === libId;

  // 1. 进入或切库时：读 URL 优先，缺省再读服务端 saved settings
  useEffect(() => {
    if (!libId) {
      setSettingsReadyLibId(null);
      setSettingsReady(true);
      return;
    }
    setSettingsReady(false);
    setSettingsReadyLibId(null);
    lastWrittenSettings.current = '';
    const fromUrl = parseFiltersFromSearch(searchParams);
    if (fromUrl) {
      setActiveTag(fromUrl.activeTag);
      setActiveAuthor(fromUrl.activeAuthor);
      setActiveStatus(fromUrl.activeStatus);
      setActiveLetter(fromUrl.activeLetter);
      setSortByField(fromUrl.sortByField);
      setSortDir(fromUrl.sortDir);
      setKeyword(fromUrl.keyword);
      setPage(fromUrl.page);
      setPageSize(fromUrl.pageSize);
      setSettingsReadyLibId(libId);
      setSettingsReady(true);
      return;
    }
    const stored = readStoredSettings(libId);
    if (stored) {
      setActiveTag(stored.activeTag ?? null);
      setActiveAuthor(stored.activeAuthor ?? null);
      setActiveStatus(stored.activeStatus ?? null);
      setActiveLetter(stored.activeLetter ?? null);
      setSortByField(stored.sortByField || 'name');
      setSortDir(stored.sortDir || 'asc');
      setKeyword(stored.keyword || '');
      setPage(stored.page || 1);
      setPageSize(stored.pageSize || DEFAULT_PAGE_SIZE);
    }
    setSettingsReadyLibId(libId);
    setSettingsReady(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [libId]);

  // 2. 状态变更后写回 URL（非空字段）+ 持久化到 server settings（节流）
  useEffect(() => {
    if (!libId || !currentSettingsReady) return;
    const next = new URLSearchParams(searchParams);
    setOrDelete(next, 'tag', activeTag);
    setOrDelete(next, 'author', activeAuthor);
    setOrDelete(next, 'status', activeStatus);
    setOrDelete(next, 'letter', activeLetter);
    setOrDelete(next, 'q', keyword.trim());
    setOrDelete(next, 'sort', sortByField === 'name' ? null : sortByField);
    setOrDelete(next, 'dir', VALID_SORT_DIRS.has(sortDir) && sortDir !== 'asc' ? sortDir : null);
    setOrDelete(next, 'size', pageSize === DEFAULT_PAGE_SIZE ? null : String(pageSize));
    setOrDelete(next, 'page', page === 1 ? null : String(page));
    if (next.toString() !== searchParams.toString()) {
      setSearchParams(next, { replace: true });
    }

    const payload: SavedLibrarySettings = {
      activeTag,
      activeAuthor,
      activeStatus,
      activeLetter,
      sortByField,
      sortDir,
      keyword: keyword.trim(),
      pageSize,
      page,
    };
    const signature = JSON.stringify(payload);
    if (signature === lastWrittenSettings.current) return;
    lastWrittenSettings.current = signature;
    const timer = window.setTimeout(() => {
      writeStoredSettings(libId, payload);
    }, 400);
    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    libId,
    currentSettingsReady,
    activeTag,
    activeAuthor,
    activeStatus,
    activeLetter,
    keyword,
    sortByField,
    sortDir,
    pageSize,
    page,
  ]);

  const resetAll = useCallback(() => {
    setActiveTag(null);
    setActiveAuthor(null);
    setActiveStatus(null);
    setActiveLetter(null);
    setKeyword('');
    setSortByField('name');
    setSortDir('asc');
    setPageSize(DEFAULT_PAGE_SIZE);
    setPage(1);
  }, []);

  const applySnapshot = useCallback((snapshot: Partial<SavedLibrarySettings>) => {
    if ('activeTag' in snapshot) setActiveTag(snapshot.activeTag ?? null);
    if ('activeAuthor' in snapshot) setActiveAuthor(snapshot.activeAuthor ?? null);
    if ('activeStatus' in snapshot) setActiveStatus(snapshot.activeStatus ?? null);
    if ('activeLetter' in snapshot) setActiveLetter(snapshot.activeLetter ?? null);
    if ('keyword' in snapshot) setKeyword(snapshot.keyword ?? '');
    if (snapshot.sortByField) setSortByField(snapshot.sortByField);
    if (snapshot.sortDir) setSortDir(snapshot.sortDir);
    if (snapshot.pageSize) setPageSize(snapshot.pageSize);
    setPage(1);
  }, []);

  return useMemo(
    () => ({
      activeTag,
      activeAuthor,
      activeStatus,
      activeLetter,
      sortByField,
      sortDir,
      keyword,
      page,
      pageSize,
      settingsReady: currentSettingsReady,
      setActiveTag,
      setActiveAuthor,
      setActiveStatus,
      setActiveLetter,
      setSortByField,
      setSortDir,
      setKeyword,
      setPage,
      setPageSize,
      resetAll,
      applySnapshot,
    }),
    [
      activeTag,
      activeAuthor,
      activeStatus,
      activeLetter,
      keyword,
      sortByField,
      sortDir,
      page,
      pageSize,
      currentSettingsReady, // 已含 settingsReady/settingsReadyLibId/libId 的派生结果，无需再单列后两者
      resetAll,
      applySnapshot,
    ],
  );
}

function setOrDelete(params: URLSearchParams, key: string, value: string | null) {
  if (value && value !== '') {
    params.set(key, value);
  } else {
    params.delete(key);
  }
}

function parseFiltersFromSearch(params: URLSearchParams): {
  activeTag: string | null;
  activeAuthor: string | null;
  activeStatus: string | null;
  activeLetter: string | null;
  sortByField: string;
  sortDir: string;
  keyword: string;
  page: number;
  pageSize: number;
} | null {
  // 至少有一个 query 参数才认为 URL 携带了完整意图（避免覆盖服务端 settings）
  const hasAny = ['tag', 'author', 'status', 'letter', 'q', 'sort', 'dir', 'size', 'page'].some((k) => params.has(k));
  if (!hasAny) return null;
  const sizeRaw = parseInt(params.get('size') || '', 10);
  const pageRaw = parseInt(params.get('page') || '', 10);
  return {
    activeTag: params.get('tag') || null,
    activeAuthor: params.get('author') || null,
    activeStatus: params.get('status') || null,
    activeLetter: params.get('letter') || null,
    keyword: params.get('q') || '',
    sortByField: params.get('sort') || 'name',
    sortDir: VALID_SORT_DIRS.has(params.get('dir') || '') ? (params.get('dir') as string) : 'asc',
    pageSize: Number.isFinite(sizeRaw) && sizeRaw > 0 ? sizeRaw : DEFAULT_PAGE_SIZE,
    page: Number.isFinite(pageRaw) && pageRaw > 0 ? pageRaw : 1,
  };
}

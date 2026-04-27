import { useState, useEffect, useRef } from 'react';
import axios, { type AxiosResponse } from 'axios';
import { useParams, Link, useOutletContext } from 'react-router-dom';
import { CheckCircle2, HardDrive, Heart, ImageIcon, Loader2, RefreshCw, Send, FolderHeart, PackageCheck, ChevronDown, ChevronUp } from 'lucide-react';
import AddToCollectionModal from '../components/AddToCollectionModal';
import { DirectoryPicker } from '../components/layout/DirectoryPicker';
import type { BrowseDirEntry, BrowseDrive } from '../components/layout/types';
import { ModalShell } from '../components/ui/ModalShell';
import { modalGhostButtonClass, modalPrimaryButtonClass } from '../components/ui/modalStyles';
import { AIRecommendationsSection } from './home/AIRecommendationsSection';
import { HomeFilters } from './home/HomeFilters';
import { HomeSmartFilters } from './home/HomeSmartFilters';
import { HomeToolbar } from './home/HomeToolbar';
import { RecentSeriesStrip } from './home/RecentSeriesStrip';
import type { AIRecommendation, NamedOption, SavedSmartFilter, Series } from './home/types';
import { useI18n } from '../i18n/LocaleProvider';

const DEFAULT_PAGE_SIZE = 30;

interface SeriesSearchResponse {
    items?: Series[];
    total?: number;
}

interface SavedLibrarySettings {
    activeTag?: string | null;
    activeAuthor?: string | null;
    activeStatus?: string | null;
    activeLetter?: string | null;
    sortByField?: string;
    sortDir?: string;
    pageSize?: number;
    page?: number;
}

const inflightSeriesSearchRequests = new Map<string, Promise<AxiosResponse<SeriesSearchResponse>>>();

interface ExternalSession {
    session_id: string;
    library_id: number;
    external_path: string;
    ignore_extension: boolean;
    status: 'scanning' | 'ready' | 'failed';
    error?: string;
    scanned_files: number;
    matched_books: number;
    unmatched_files: number;
    total_books: number;
    created_at: string;
    updated_at: string;
}

interface ExternalSessionCreateResponse {
    session: ExternalSession;
    task_key: string;
}

interface ExternalSeriesStatus {
    series_id: number;
    series_name?: string;
    external_match_count: number;
    external_total_count: number;
    external_sync_status: 'missing' | 'partial' | 'complete';
}

function getApiErrorMessage(error: unknown, fallback: string) {
    if (axios.isAxiosError(error)) {
        return error.response?.data?.error || error.message || fallback;
    }
    if (error instanceof Error) {
        return error.message;
    }
    return fallback;
}

function requestSeriesSearch(query: string) {
    const existing = inflightSeriesSearchRequests.get(query);
    if (existing) {
        return existing;
    }

    const request = axios.get(`/api/series/search?${query}`)
        .finally(() => {
            if (inflightSeriesSearchRequests.get(query) === request) {
                inflightSeriesSearchRequests.delete(query);
            }
        });
    inflightSeriesSearchRequests.set(query, request);
    return request;
}

function smartFilterStorageKey(libraryId: string) {
    return `lib_smart_filters_${libraryId}`;
}

function readSavedSmartFilters(libraryId: string) {
    try {
        const saved = localStorage.getItem(smartFilterStorageKey(libraryId));
        if (!saved) return [];
        const parsed = JSON.parse(saved) as unknown;
        if (!Array.isArray(parsed)) return [];
        return parsed.filter((item): item is SavedSmartFilter => {
            return Boolean(
                item
                && typeof item === 'object'
                && 'id' in item
                && 'name' in item
                && typeof item.id === 'string'
                && typeof item.name === 'string'
            );
        });
    } catch {
        return [];
    }
}

export default function Home() {
    const { t, formatNumber } = useI18n();
    const { libId } = useParams();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number; libraries?: { id: string; name: string; koreader_sync_enabled?: boolean }[] }>() || { refreshTrigger: 0, libraries: [] };
    const [allSeries, setAllSeries] = useState<Series[]>([]);
    const [recentSeries, setRecentSeries] = useState<Series[]>([]);
    const [totalSeries, setTotalSeries] = useState(0);
    const [loading, setLoading] = useState(false);
    const [activeTag, setActiveTag] = useState<string | null>(null);
    const [activeAuthor, setActiveAuthor] = useState<string | null>(null);
    const [activeStatus, setActiveStatus] = useState<string | null>(null);
    const [activeLetter, setActiveLetter] = useState<string | null>(null);
    const [sortByField, setSortByField] = useState<string>('name');
    const [sortDir, setSortDir] = useState<string>('asc');
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
    const [savedSmartFilters, setSavedSmartFilters] = useState<SavedSmartFilter[]>([]);
    const [settingsReady, setSettingsReady] = useState(false);

    const [isSelectionMode, setIsSelectionMode] = useState(false);
    const [selectedSeries, setSelectedSeries] = useState<number[]>([]);
    const [showCollectionModal, setShowCollectionModal] = useState(false);
    const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);
    const [rescanningId, setRescanningId] = useState<number | null>(null);
    const [recentExternalPaths, setRecentExternalPaths] = useState<string[]>([]);
    const [externalPath, setExternalPath] = useState('');
    const [externalIgnoreExtension, setExternalIgnoreExtension] = useState(false);
    const [externalSession, setExternalSession] = useState<ExternalSession | null>(null);
    const [externalSeriesMap, setExternalSeriesMap] = useState<Record<number, ExternalSeriesStatus>>({});
    const [startingExternalScan, setStartingExternalScan] = useState(false);
    const [startingTransfer, setStartingTransfer] = useState(false);
    const [externalScanTaskKey, setExternalScanTaskKey] = useState<string | null>(null);
    const [externalTransferTaskKey, setExternalTransferTaskKey] = useState<string | null>(null);
    const [showTransferConfirmModal, setShowTransferConfirmModal] = useState(false);
    const [pendingTransferSummary, setPendingTransferSummary] = useState<{ total: number; matched: number; missing: number } | null>(null);

    const [externalBrowsing, setExternalBrowsing] = useState(false);
    const [externalBrowseDirs, setExternalBrowseDirs] = useState<BrowseDirEntry[]>([]);
    const [externalBrowseCurrent, setExternalBrowseCurrent] = useState('');
    const [externalBrowseParent, setExternalBrowseParent] = useState('');
    const [externalBrowseDrives, setExternalBrowseDrives] = useState<BrowseDrive[]>([]);

    // External Library section collapse state
    const [isExternalExpanded, setIsExternalExpanded] = useState(false);
    const hasMountedRefreshEffect = useRef(false);

    const showToast = (text: string, type: 'success' | 'error') => {
        setToastMsg({ text, type });
        setTimeout(() => setToastMsg(null), 3000);
    };

    const persistSmartFilters = (items: SavedSmartFilter[]) => {
        if (!libId) return;
        setSavedSmartFilters(items);
        localStorage.setItem(smartFilterStorageKey(libId), JSON.stringify(items));
    };

    const buildCurrentSmartFilter = (name: string): SavedSmartFilter => ({
        id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        name,
        activeTag,
        activeAuthor,
        activeStatus,
        activeLetter,
        sortByField,
        sortDir,
        pageSize,
        createdAt: new Date().toISOString(),
    });

    const handleSaveSmartFilter = (rawName: string) => {
        if (!libId) return;
        const name = rawName.trim() || t('home.smartFilters.defaultName', { count: savedSmartFilters.length + 1 });
        const next = [
            buildCurrentSmartFilter(name),
            ...savedSmartFilters.filter((item) => item.name !== name),
        ].slice(0, 20);
        persistSmartFilters(next);
        showToast(t('home.smartFilters.saved'), 'success');
    };

    const handleApplySmartFilter = (filter: SavedSmartFilter) => {
        setActiveTag(filter.activeTag ?? null);
        setActiveAuthor(filter.activeAuthor ?? null);
        setActiveStatus(filter.activeStatus ?? null);
        setActiveLetter(filter.activeLetter ?? null);
        setSortByField(filter.sortByField || 'name');
        setSortDir(filter.sortDir || 'asc');
        setPageSize(filter.pageSize || DEFAULT_PAGE_SIZE);
        setPage(1);
        showToast(t('home.smartFilters.applied', { name: filter.name }), 'success');
    };

    const handleDeleteSmartFilter = (id: string) => {
        persistSmartFilters(savedSmartFilters.filter((item) => item.id !== id));
        showToast(t('home.smartFilters.deleted'), 'success');
    };

    const handleResetSmartFilters = () => {
        setActiveTag(null);
        setActiveAuthor(null);
        setActiveStatus(null);
        setActiveLetter(null);
        setSortByField('name');
        setSortDir('asc');
        setPageSize(DEFAULT_PAGE_SIZE);
        setPage(1);
    };

    const [allTags, setAllTags] = useState<NamedOption[]>([]);
    const [allAuthors, setAllAuthors] = useState<NamedOption[]>([]);
    const allStatuses = ['completed', 'ongoing', 'cancelled', 'hiatus'];

    const [aiRecommendations, setAiRecommendations] = useState<AIRecommendation[]>([]);
    const [loadingAI, setLoadingAI] = useState(false);
    const [hasFetchedAI, setHasFetchedAI] = useState(false);
    const currentPageSeriesIds = allSeries.map((series) => series.id);
    const currentPageSelectedCount = currentPageSeriesIds.filter((id) => selectedSeries.includes(id)).length;
    const allCurrentPageSelected = currentPageSeriesIds.length > 0 && currentPageSelectedCount === currentPageSeriesIds.length;

    const externalVisibilitySummary = allSeries.reduce((acc, series) => {
        const externalStatus = externalSeriesMap[series.id];
        const total = externalStatus?.external_total_count ?? series.actual_book_count ?? 0;
        const matched = externalStatus?.external_match_count ?? 0;
        const status = externalStatus?.external_sync_status
            ?? (matched > 0
                ? (matched >= total && total > 0 ? 'complete' : 'partial')
                : 'missing');

        if (status === 'complete') acc.complete += 1;
        else if (status === 'partial') acc.partial += 1;
        else acc.missing += 1;
        return acc;
    }, { complete: 0, partial: 0, missing: 0 });

    const saveRecentExternalPath = (path: string) => {
        const normalized = path.trim();
        if (!normalized) return;
        const next = [normalized, ...recentExternalPaths.filter((item) => item !== normalized)].slice(0, 5);
        setRecentExternalPaths(next);
        localStorage.setItem('manga_manager_recent_external_paths', JSON.stringify(next));
    };

    const openExternalDirectoryBrowser = () => {
        setExternalBrowsing(true);
        axios.get('/api/browse-dirs')
            .then(res => {
                setExternalBrowseDirs(res.data.dirs || []);
                setExternalBrowseCurrent(res.data.current);
                setExternalBrowseParent(res.data.parent);
                setExternalBrowseDrives(res.data.drives || []);
            })
            .catch(err => console.error('Failed to open external directory browser', err));
    };

    const navigateExternalDirectoryBrowser = (path: string) => {
        axios.get(`/api/browse-dirs?path=${encodeURIComponent(path)}`)
            .then(res => {
                setExternalBrowseDirs(res.data.dirs || []);
                setExternalBrowseCurrent(res.data.current);
                setExternalBrowseParent(res.data.parent);
                setExternalBrowseDrives(res.data.drives || []);
            })
            .catch(err => console.error('Failed to navigate external directory browser', err));
    };

    const fetchExternalSession = async (sessionId = externalSession?.session_id) => {
        if (!libId || !sessionId) return null;
        try {
            const res = await axios.get<ExternalSession>(`/api/libraries/${libId}/external-libraries/session/${sessionId}`, {
                params: { _ts: Date.now() },
            });
            setExternalSession(res.data);
            return res.data;
        } catch (err) {
            console.error('Failed to fetch external session', err);
            setExternalSession(null);
            setExternalSeriesMap({});
            return null;
        }
    };

    const fetchExternalSeriesStatus = async (sessionId = externalSession?.session_id, seriesIds = allSeries.map((item) => item.id)) => {
        if (!libId || !sessionId || seriesIds.length === 0) {
            setExternalSeriesMap({});
            return;
        }
        try {
            const res = await axios.get<ExternalSeriesStatus[]>(
                `/api/libraries/${libId}/external-libraries/session/${sessionId}/series`,
                {
                    params: {
                        ids: seriesIds.join(','),
                        _ts: Date.now(),
                    },
                }
            );
            const next: Record<number, ExternalSeriesStatus> = {};
            (res.data || []).forEach((item) => {
                next[item.series_id] = item;
            });
            setExternalSeriesMap(next);
        } catch (err) {
            console.error('Failed to fetch external series coverage', err);
            setExternalSeriesMap({});
        }
    };

    const startExternalLibraryScan = async () => {
        if (!libId || !externalPath.trim()) {
            showToast(t('home.external.pickPathFirst'), 'error');
            return;
        }
        setStartingExternalScan(true);
        try {
            const res = await axios.post<ExternalSessionCreateResponse>(`/api/libraries/${libId}/external-libraries/session`, {
                external_path: externalPath.trim(),
                ignore_extension: externalIgnoreExtension,
            });
            setExternalSession(res.data.session);
            setExternalSeriesMap({});
            setExternalScanTaskKey(res.data.task_key || null);
            setExternalTransferTaskKey(null);
            saveRecentExternalPath(externalPath.trim());
            showToast(t('home.external.scanStarted'), 'success');
        } catch (err: unknown) {
            showToast(getApiErrorMessage(err, t('home.external.scanStartFailed')), 'error');
        } finally {
            setStartingExternalScan(false);
        }
    };

    const clearExternalSession = () => {
        setExternalSession(null);
        setExternalSeriesMap({});
        setExternalScanTaskKey(null);
        setExternalTransferTaskKey(null);
    };

    const fetchAIRecommendations = () => {
        if (!libId) return;
        setLoadingAI(true);
        axios.get('/api/recommendations?limit=3')
            .then(res => {
                setAiRecommendations(res.data || []);
                setHasFetchedAI(true);
            })
            .catch(err => {
                console.error("Failed to fetch AI recommendations", err);
                showToast(t('home.ai.fetchFailed'), "error");
            })
            .finally(() => setLoadingAI(false));
    };

    useEffect(() => {
        Promise.all([
            axios.get('/api/tags/all').catch(() => ({ data: [] })),
            axios.get('/api/authors/all').catch(() => ({ data: [] })),
        ]).then(([tRes, aRes]) => {
            // Deduplicate authors by name since we might have Writer, Penciller combinations
            const tNames = Array.isArray(tRes.data) ? tRes.data as NamedOption[] : [];
            const aList = Array.isArray(aRes.data) ? aRes.data as NamedOption[] : [];
            const map = new Map<string, NamedOption>();
            aList.forEach((a) => map.set(a.name, a));

            setAllTags(tNames);
            setAllAuthors(Array.from(map.values()));
        });

        try {
            const stored = localStorage.getItem('manga_manager_recent_external_paths');
            if (stored) {
                const parsed = JSON.parse(stored) as unknown;
                if (Array.isArray(parsed)) {
                    setRecentExternalPaths(parsed.filter((item) => typeof item === 'string'));
                }
            }
            const storedIgnoreExtension = localStorage.getItem('manga_manager_external_ignore_extension');
            if (storedIgnoreExtension === 'true') {
                setExternalIgnoreExtension(true);
            }
        } catch {
            // ignore invalid local storage
        }
    }, []);

    useEffect(() => {
        localStorage.setItem('manga_manager_external_ignore_extension', externalIgnoreExtension ? 'true' : 'false');
    }, [externalIgnoreExtension]);

    useEffect(() => {
        if (libId) {
            axios.get(`/api/series/recent-read?libraryId=${libId}&limit=10`)
                .then(res => {
                    setRecentSeries(res.data.items || []);
                })
                .catch(err => console.error("Failed to fetch recent series:", err));
        }
    }, [libId, refreshTrigger]);

    const fetchSeriesPage = (pageNumber: number, silent = false) => {
        if (!libId || (loading && !silent)) return;

        if (!silent) setLoading(true);
        const params = new URLSearchParams();
        params.append('libraryId', libId);
        params.append('limit', pageSize.toString());
        params.append('page', pageNumber.toString());
        if (activeTag) params.append('tags', activeTag);
        if (activeAuthor) params.append('authors', activeAuthor);
        if (activeStatus) params.append('status', activeStatus);
        if (activeLetter) params.append('letter', activeLetter);
        if (sortByField && sortDir) params.append('sortBy', `${sortByField}_${sortDir}`);

        requestSeriesSearch(params.toString())
            .then((res) => {
                const newItems = res.data.items || [];
                setAllSeries(newItems);
                setTotalSeries(res.data.total || 0);
                if (!silent) {
                    setLoading(false);
                    window.scrollTo({ top: 0, behavior: 'smooth' });
                }
            })
            .catch((err: unknown) => {
                console.error("Failed to fetch series:", err);
                setLoading(false);
            });
    };

    // 1. 恢复配置 (仅在 libId 变化时执行一次)
    useEffect(() => {
        if (!libId) return;
        setSavedSmartFilters(readSavedSmartFilters(libId));
        const saved = localStorage.getItem(`lib_settings_${libId}`);
        if (saved) {
            try {
                const config = JSON.parse(saved) as SavedLibrarySettings;
                setActiveTag(config.activeTag ?? null);
                setActiveAuthor(config.activeAuthor ?? null);
                setActiveStatus(config.activeStatus ?? null);
                setActiveLetter(config.activeLetter ?? null);
                setSortByField(config.sortByField || 'name');
                setSortDir(config.sortDir || 'asc');
                setPageSize(config.pageSize || DEFAULT_PAGE_SIZE);
                setPage(config.page || 1);
            } catch (err) {
                console.warn('Failed to restore library settings:', err);
            }
        } else {
            setActiveTag(null);
            setActiveAuthor(null);
            setActiveStatus(null);
            setActiveLetter(null);
            setSortByField('name');
            setSortDir('asc');
            setPageSize(DEFAULT_PAGE_SIZE);
            setPage(1);
        }
        setSettingsReady(true);
        return () => setSettingsReady(false);
    }, [libId]);

    // 2. 状态变化处理：筛选/排序/分页变化时，延迟 300ms 再拉取（防抖），避免快速切换筛选条件时的请求洪峰
    useEffect(() => {
        if (!libId || !settingsReady) return;

        // 保存配置
        const config = { activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir, pageSize, page };
        localStorage.setItem(`lib_settings_${libId}`, JSON.stringify(config));

        // 防抖：300ms 后执行拉取
        const timer = setTimeout(() => {
            fetchSeriesPage(page);
        }, 300);

        return () => clearTimeout(timer);
        // fetchSeriesPage intentionally reads the current state captured by this trigger set.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [libId, settingsReady, page, pageSize, activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir]);

    // 筛选条件、排序或资源库切换时清空选择；仅翻页时保留跨页多选状态
    useEffect(() => {
        setIsSelectionMode(false);
        setSelectedSeries([]);
    }, [libId, activeTag, activeAuthor, activeStatus, activeLetter, sortByField, sortDir]);

    // 3. SSE 专用静默刷新
    // 注意：settingsReady 仅用作守卫条件，不作为触发依赖。
    // 如果将 settingsReady 加入依赖，当它从 false→true 变化时（配置恢复后），
    // 本 effect 会和 effect #2 同时触发，导致发出两个相同的请求。
    useEffect(() => {
        if (!hasMountedRefreshEffect.current) {
            hasMountedRefreshEffect.current = true;
            return;
        }

        if (libId && settingsReady) {
            fetchSeriesPage(page, true);
        }
        if (externalSession?.session_id) {
            fetchExternalSession(externalSession.session_id).then((session) => {
                if (session?.status === 'ready') {
                    fetchExternalSeriesStatus(session.session_id);
                    // 当 SSE refresh 先于轮询 useEffect 检测到扫描完成时，
                    // 主动 dispatch override 事件以关闭全局进度条，避免竞争条件。
                    if (externalScanTaskKey) {
                        window.dispatchEvent(new CustomEvent('manga-manager:task-progress-override', {
                            detail: {
                                key: externalScanTaskKey,
                                type: 'scan_external_library',
                                status: 'completed',
                                message: session.scanned_files > 0
                                    ? t('home.external.scanCompletedWithCount', { count: session.scanned_files })
                                    : t('home.external.scanCompleted'),
                                current: session.scanned_files,
                                total: session.scanned_files,
                            },
                        }));
                        setExternalScanTaskKey(null);
                    }
                } else if (session?.status === 'failed' && externalScanTaskKey) {
                    window.dispatchEvent(new CustomEvent('manga-manager:task-progress-override', {
                        detail: {
                            key: externalScanTaskKey,
                            type: 'scan_external_library',
                            status: 'failed',
                            message: session.error || t('home.external.statusFailed'),
                            error: session.error,
                            current: session.scanned_files,
                            total: session.scanned_files,
                        },
                    }));
                    setExternalScanTaskKey(null);
                }
            });
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [refreshTrigger]);

    useEffect(() => {
        setExternalSession(null);
        setExternalSeriesMap({});
        setExternalScanTaskKey(null);
        setExternalTransferTaskKey(null);
    }, [libId]);

    useEffect(() => {
        if (!externalSession?.session_id || externalSession.status !== 'scanning') return;
        const timer = window.setInterval(() => {
            fetchExternalSession(externalSession.session_id).then((session) => {
                if (!session || !externalScanTaskKey) return;
                if (session.status === 'ready' || session.status === 'failed') {
                    window.dispatchEvent(new CustomEvent('manga-manager:task-progress-override', {
                        detail: {
                            key: externalScanTaskKey,
                            type: 'scan_external_library',
                            status: session.status === 'ready' ? 'completed' : 'failed',
                            message: session.status === 'ready'
                                ? (session.scanned_files > 0 ? t('home.external.scanCompletedWithCount', { count: session.scanned_files }) : t('home.external.scanCompleted'))
                                : (session.error || t('home.external.statusFailed')),
                            error: session.status === 'failed' ? session.error : undefined,
                            current: session.scanned_files,
                            total: session.scanned_files,
                        },
                    }));
                    if (session.status === 'ready') {
                        fetchExternalSeriesStatus(session.session_id);
                    }
                    setExternalScanTaskKey(null);
                }
            });
        }, 1500);
        return () => window.clearInterval(timer);
        // Polling is keyed by the external session state; helper dependencies would restart the interval on unrelated renders.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [externalSession?.session_id, externalSession?.status, externalScanTaskKey, libId]);

    useEffect(() => {
        const handleTaskProgress = (event: Event) => {
            const customEvent = event as CustomEvent<{
                key?: string;
                type?: string;
                status?: string;
            }>;
            const progress = customEvent.detail;
            if (!progress || !externalSession?.session_id || !libId) return;

            const isTrackedScanTask = progress.key === externalScanTaskKey && progress.type === 'scan_external_library';
            const isTrackedTransferTask = progress.key === externalTransferTaskKey && progress.type === 'transfer_external_library';
            if (!isTrackedScanTask && !isTrackedTransferTask) return;
            if (progress.status !== 'completed' && progress.status !== 'failed') return;

            fetchExternalSession(externalSession.session_id).then((session) => {
                if (session?.status === 'ready') {
                    fetchExternalSeriesStatus(session.session_id);
                }
            });

            if (isTrackedScanTask) {
                setExternalScanTaskKey(null);
            }
            if (isTrackedTransferTask) {
                setExternalTransferTaskKey(null);
            }
        };

        window.addEventListener('manga-manager:task-progress', handleTaskProgress as EventListener);
        return () => {
            window.removeEventListener('manga-manager:task-progress', handleTaskProgress as EventListener);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [externalSession?.session_id, externalScanTaskKey, externalTransferTaskKey, allSeries, libId]);

    useEffect(() => {
        if (!externalSession?.session_id) {
            setExternalSeriesMap({});
            return;
        }

        fetchExternalSession(externalSession.session_id).then((session) => {
            if (session?.status === 'ready') {
                fetchExternalSeriesStatus(session.session_id);
            } else if (session?.status !== 'scanning') {
                setExternalSeriesMap({});
            }
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [externalSession?.session_id, allSeries, refreshTrigger]);

    const handleBulkFavoriteUpdate = async (isFav: boolean) => {
        try {
            await axios.post('/api/series/bulk-update', {
                series_ids: selectedSeries,
                is_favorite: isFav
            });
            setIsSelectionMode(false);
            setSelectedSeries([]);
            // 由于使用了 useOutletContext，无法直接修改其 state。因此我们可以借助触发重新 fetch 当前列表。
            fetchSeriesPage(page, true);
        } catch (e) {
            console.error("Bulk update failed", e);
            showToast(t('home.bulkFavoriteFailed'), 'error');
        }
    };

    const handleToggleFavorite = async (e: React.MouseEvent, seriesId: number, currentFav: boolean) => {
        e.preventDefault();
        e.stopPropagation();
        try {
            await axios.post('/api/series/bulk-update', {
                series_ids: [seriesId],
                is_favorite: !currentFav
            });
            // 静默刷新列表
            fetchSeriesPage(page, true);
        } catch (e) {
            console.error("Toggle favorite failed", e);
        }
    };

    const handleRescanSeries = async (e: React.MouseEvent, seriesId: number) => {
        e.preventDefault();
        e.stopPropagation();
        setRescanningId(seriesId);
        try {
            await axios.post(`/api/series/${seriesId}/rescan?force=true`);
            showToast(t('home.seriesRescanQueued'), 'success');
            setTimeout(() => fetchSeriesPage(page, true), 3000);
        } catch (err: unknown) {
            showToast(`${t('home.seriesRescanFailed')}: ${getApiErrorMessage(err, t('home.seriesRescanFailed'))}`, 'error');
        } finally {
            setRescanningId(null);
        }
    };

    const submitTransferSelectedSeries = async () => {
        if (!libId || !externalSession?.session_id) return;

        setStartingTransfer(true);
        try {
            const res = await axios.post(`/api/libraries/${libId}/external-libraries/session/${externalSession.session_id}/transfer`, {
                series_ids: selectedSeries,
            });
            setExternalTransferTaskKey(res.data?.task_key || null);
            showToast(res.data?.message || t('home.external.transferQueued'), 'success');
            setShowTransferConfirmModal(false);
            setPendingTransferSummary(null);
        } catch (err: unknown) {
            showToast(getApiErrorMessage(err, t('home.external.transferFailed')), 'error');
        } finally {
            setStartingTransfer(false);
        }
    };

    const handleTransferSelectedSeries = async () => {
        if (!libId || !externalSession?.session_id) {
            showToast(t('home.external.scanFirst'), 'error');
            return;
        }
        if (externalSession.status !== 'ready') {
            showToast(t('home.external.stillScanning'), 'error');
            return;
        }

        const summary = selectedSeries.reduce((acc, seriesId) => {
            const status = externalSeriesMap[seriesId];
            const total = status?.external_total_count ?? allSeries.find((item) => item.id === seriesId)?.actual_book_count ?? 0;
            const matched = status?.external_match_count ?? 0;
            acc.total += total;
            acc.matched += matched;
            acc.missing += Math.max(0, total - matched);
            return acc;
        }, { total: 0, matched: 0, missing: 0 });

        if (summary.missing === 0) {
            showToast(t('home.external.alreadyComplete'), 'success');
            return;
        }

        setPendingTransferSummary(summary);
        setShowTransferConfirmModal(true);
    };

    const handleToggleSelectCurrentPage = () => {
        if (allCurrentPageSelected) {
            setSelectedSeries((prev) => prev.filter((id) => !currentPageSeriesIds.includes(id)));
            return;
        }
        setSelectedSeries((prev) => Array.from(new Set([...prev, ...currentPageSeriesIds])));
    };

    if (!libId) {
        return (
            <div className="flex-1 flex items-center justify-center p-10 h-full text-gray-500">
                {t('home.pickLibrary')}
            </div>
        );
    }

    return (
        <div className="p-6 lg:p-10">
            <HomeToolbar
                totalSeries={totalSeries}
                hasSeries={allSeries.length > 0}
                isSelectionMode={isSelectionMode}
                allCurrentPageSelected={allCurrentPageSelected}
                selectedCount={selectedSeries.length}
                sortByField={sortByField}
                sortDir={sortDir}
                onToggleSelectionMode={() => {
                    setIsSelectionMode(!isSelectionMode);
                    if (isSelectionMode) {
                        setSelectedSeries([]);
                    }
                }}
                onToggleSelectCurrentPage={handleToggleSelectCurrentPage}
                onSortFieldChange={(value) => {
                    setSortByField(value);
                    setPage(1);
                }}
                onToggleSortDir={() => {
                    setSortDir(prev => prev === 'asc' ? 'desc' : 'asc');
                    setPage(1);
                }}
            />

            <HomeSmartFilters
                savedFilters={savedSmartFilters}
                activeTag={activeTag}
                activeAuthor={activeAuthor}
                activeStatus={activeStatus}
                activeLetter={activeLetter}
                sortByField={sortByField}
                sortDir={sortDir}
                pageSize={pageSize}
                onSave={handleSaveSmartFilter}
                onApply={handleApplySmartFilter}
                onDelete={handleDeleteSmartFilter}
                onReset={handleResetSmartFilters}
            />

            <div className="mb-6 rounded-2xl border border-gray-800 bg-gradient-to-br from-gray-900 to-gray-950 p-4 sm:p-5">
                <div
                    className="flex items-center justify-between cursor-pointer group"
                    onClick={() => setIsExternalExpanded(!isExternalExpanded)}
                >
                    <div>
                        <div className="flex items-center gap-2 text-white">
                            <HardDrive className="w-5 h-5 text-komgaPrimary" />
                            <h3 className="text-lg font-semibold">{t('home.external.title')}</h3>
                            {externalSession?.status === 'scanning' && <Loader2 className="w-4 h-4 ml-2 animate-spin text-blue-400" />}
                            {externalSession?.status === 'ready' && !isExternalExpanded && <CheckCircle2 className="w-4 h-4 ml-2 text-emerald-400" />}
                        </div>
                        <p className="mt-1 text-sm text-gray-400 transition-colors group-hover:text-gray-300">
                            {t('home.external.description')}
                        </p>
                    </div>
                    <div className="text-gray-500 group-hover:text-white transition-colors">
                        {isExternalExpanded ? <ChevronUp className="w-5 h-5" /> : <ChevronDown className="w-5 h-5" />}
                    </div>
                </div>

                {isExternalExpanded && (
                    <div className="mt-4 pt-4 border-t border-gray-800/50 flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
                        <div className="flex-1 w-full lg:max-w-3xl">
                            <DirectoryPicker
                                value={externalPath}
                                onChange={setExternalPath}
                                browsing={externalBrowsing}
                                browseCurrent={externalBrowseCurrent}
                                browseParent={externalBrowseParent}
                                browseDirs={externalBrowseDirs}
                                browseDrives={externalBrowseDrives}
                                recentPaths={recentExternalPaths}
                                onOpen={openExternalDirectoryBrowser}
                                onClose={() => setExternalBrowsing(false)}
                                onChooseCurrent={() => {
                                    setExternalPath(externalBrowseCurrent);
                                    setExternalBrowsing(false);
                                }}
                                onNavigate={navigateExternalDirectoryBrowser}
                            />
                            <label className="mt-4 inline-flex items-center gap-3 rounded-lg border border-gray-800 bg-black/20 px-3 py-2 text-sm text-gray-300">
                                <input
                                    type="checkbox"
                                    checked={externalIgnoreExtension}
                                    onChange={(event) => setExternalIgnoreExtension(event.target.checked)}
                                    className="h-4 w-4 rounded border-gray-600 bg-gray-900 text-komgaPrimary focus:ring-komgaPrimary"
                                />
                                <span>{t('home.external.ignoreExtension')}</span>
                            </label>
                            <div className="mt-4 flex flex-wrap items-center gap-3">
                                <button
                                    onClick={startExternalLibraryScan}
                                    disabled={startingExternalScan || !externalPath.trim()}
                                    className="rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-4 py-2 text-sm font-medium text-komgaPrimary hover:bg-komgaPrimary/20 disabled:cursor-not-allowed disabled:opacity-50"
                                >
                                    {startingExternalScan ? t('home.external.startingScan') : t('home.external.scanAction')}
                                </button>
                                {externalSession && (
                                    <button
                                        onClick={clearExternalSession}
                                        className="rounded-lg border border-gray-700 bg-gray-900 px-4 py-2 text-sm font-medium text-gray-300 hover:border-gray-600 hover:text-white"
                                    >
                                        {t('home.external.clearSession')}
                                    </button>
                                )}
                            </div>
                        </div>

                        {externalSession && (
                            <div className="rounded-xl border border-gray-800 bg-black/20 px-4 py-3 text-sm text-gray-300 w-full lg:min-w-[280px] lg:w-auto">
                                <div className="flex items-center gap-2">
                                    {externalSession.status === 'scanning' ? (
                                        <Loader2 className="w-4 h-4 animate-spin text-blue-400" />
                                    ) : externalSession.status === 'ready' ? (
                                        <CheckCircle2 className="w-4 h-4 text-emerald-400" />
                                    ) : (
                                        <HardDrive className="w-4 h-4 text-red-400" />
                                    )}
                                    <span className="font-medium text-white">
                                        {externalSession.status === 'scanning' ? t('home.external.statusScanning') : externalSession.status === 'ready' ? t('home.external.statusReady') : t('home.external.statusFailed')}
                                    </span>
                                </div>
                                <p className="mt-2 text-xs text-gray-400 break-all">{externalSession.external_path}</p>
                                <p className="mt-2 text-xs text-gray-500">
                                    {t('home.external.matchRule', { rule: externalSession.ignore_extension ? t('home.external.ignoreExtensionShort') : t('home.external.keepExtensionShort') })}
                                </p>
                                <div className="mt-3 grid grid-cols-3 gap-3 text-xs">
                                    <div>
                                        <p className="text-gray-500">{t('home.external.scanned')}</p>
                                        <p className="mt-1 text-white font-semibold">{externalSession.scanned_files}</p>
                                    </div>
                                    <div>
                                        <p className="text-gray-500">{t('home.external.matched')}</p>
                                        <p className="mt-1 text-white font-semibold">{externalSession.matched_books}/{externalSession.total_books}</p>
                                    </div>
                                    <div>
                                        <p className="text-gray-500">{t('home.external.unmatched')}</p>
                                        <p className="mt-1 text-white font-semibold">{externalSession.unmatched_files}</p>
                                    </div>
                                </div>
                                {externalSession.error && <p className="mt-3 text-xs text-red-300">{externalSession.error}</p>}
                            </div>
                        )}
                    </div>
                )}
            </div>

            <RecentSeriesStrip recentSeries={recentSeries} />

            <AIRecommendationsSection
                aiRecommendations={aiRecommendations}
                loadingAI={loadingAI}
                hasFetchedAI={hasFetchedAI}
                onRefresh={fetchAIRecommendations}
            />

            <HomeFilters
                allStatuses={allStatuses}
                allTags={allTags}
                allAuthors={allAuthors}
                activeStatus={activeStatus}
                activeTag={activeTag}
                activeAuthor={activeAuthor}
                activeLetter={activeLetter}
                onStatusChange={(value) => {
                    setActiveStatus(value);
                    setPage(1);
                }}
                onTagChange={(value) => {
                    setActiveTag(value);
                    setPage(1);
                }}
                onAuthorChange={(value) => {
                    setActiveAuthor(value);
                    setPage(1);
                }}
                onLetterChange={(value) => {
                    setActiveLetter(value);
                    setPage(1);
                }}
            />

            {externalSession && (
                <div className="mb-6 rounded-2xl border border-gray-800 bg-gray-950/80 p-4">
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                        <div>
                            <h4 className="text-sm font-semibold text-white">{t('home.external.currentPageTitle')}</h4>
                            <p className="mt-1 text-xs text-gray-400">
                                {t('home.external.currentPageDescription')}
                            </p>
                        </div>
                        <div className="grid grid-cols-3 gap-3 text-xs sm:text-sm">
                            <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/10 px-4 py-3">
                                <p className="text-emerald-300">{t('home.external.complete')}</p>
                                <p className="mt-1 text-xl font-semibold text-white">{externalVisibilitySummary.complete}</p>
                            </div>
                            <div className="rounded-xl border border-amber-500/20 bg-amber-500/10 px-4 py-3">
                                <p className="text-amber-300">{t('home.external.partial')}</p>
                                <p className="mt-1 text-xl font-semibold text-white">{externalVisibilitySummary.partial}</p>
                            </div>
                            <div className="rounded-xl border border-gray-700 bg-gray-900 px-4 py-3">
                                <p className="text-gray-300">{t('home.external.missing')}</p>
                                <p className="mt-1 text-xl font-semibold text-white">{externalVisibilitySummary.missing}</p>
                            </div>
                        </div>
                    </div>
                </div>
            )}

            {loading && allSeries.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-40">
                    <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-komgaPrimary mb-4"></div>
                    <div className="text-gray-400 font-medium">{t('common.loading')}</div>
                </div>
            ) : allSeries.length === 0 ? (
                <div className="text-center py-20 text-gray-500">{t('home.noMatches')}</div>
            ) : (
                <div className={`relative transition-opacity duration-300 ${loading ? 'opacity-40 pointer-events-none' : 'opacity-100'}`}>
                    <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-4 sm:gap-6 min-h-[600px] items-start">
                        {allSeries.map((s) => {
                            const isSelected = selectedSeries.includes(s.id);
                            const externalStatus = externalSeriesMap[s.id];
                            const externalTotal = externalStatus?.external_total_count ?? s.actual_book_count ?? 0;
                            const externalMatched = externalStatus?.external_match_count ?? 0;
                            const externalSyncStatus = externalStatus?.external_sync_status
                                ?? (externalMatched > 0
                                    ? (externalMatched >= externalTotal && externalTotal > 0 ? 'complete' : 'partial')
                                    : 'missing');
                            const externalPercent = externalTotal > 0 ? Math.min(100, Math.round((externalMatched / externalTotal) * 100)) : 0;
                            const externalStatusLabel = externalSyncStatus === 'complete'
                                ? t('home.external.cardComplete')
                                : externalSyncStatus === 'partial'
                                    ? t('home.external.cardPartial')
                                    : t('home.external.cardMissing');
                            const externalStatusClass = externalSyncStatus === 'complete'
                                ? 'bg-emerald-500/10 text-emerald-300 border border-emerald-500/20'
                                : externalSyncStatus === 'partial'
                                    ? 'bg-amber-500/10 text-amber-300 border border-amber-500/20'
                                    : 'bg-gray-900 text-gray-300 border border-gray-700';

                            const handleCardClick = (e: React.MouseEvent) => {
                                if (isSelectionMode) {
                                    e.preventDefault();
                                    setSelectedSeries((prev: number[]) => prev.includes(s.id) ? prev.filter((id: number) => id !== s.id) : [...prev, s.id]);
                                }
                            };

                            return (
                                <Link
                                    key={s.id}
                                    to={`/series/${s.id}`}
                                    onClick={handleCardClick}
                                    className={`group relative rounded-xl overflow-hidden bg-komgaSurface border ${isSelected ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20' : 'border-gray-800 hover:border-komgaPrimary/50 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10'} transition-all duration-300 cursor-pointer block h-fit`}
                                >
                                    <div className="aspect-[1/1.4] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                        {isSelectionMode && (
                                            <div className="absolute top-2 left-2 z-30">
                                                <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'}`}>
                                                    {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                                                </div>
                                            </div>
                                        )}
                                        {s.cover_path?.Valid && s.cover_path?.String ? (
                                            <img src={`/api/thumbnails/${s.cover_path.String}${s.updated_at ? `?v=${new Date(s.updated_at).getTime()}` : ''}`} alt={t('common.cover')} loading="lazy" className="absolute inset-0 w-full h-full object-cover transition-transform duration-500 group-hover:scale-105" />
                                        ) : (
                                            <ImageIcon className="h-12 w-12 text-gray-700 opacity-50 transition-opacity group-hover:opacity-100 relative z-10" />
                                        )}
                                        <div className="absolute inset-x-0 top-0 p-3 z-20 flex justify-between items-start">
                                            {s.rating?.Valid && s.rating.Float64 > 0 && (
                                                <span className="flex items-center text-xs font-bold text-yellow-400 bg-black/70 px-1.5 py-0.5 rounded backdrop-blur border border-yellow-400/20 shadow-md pointer-events-none">
                                                    ★ {s.rating.Float64.toFixed(1)}
                                                </span>
                                            )}
                                            {!isSelectionMode && (
                                                <div className="flex gap-1.5">
                                                    <button
                                                        onClick={(e) => handleRescanSeries(e, s.id)}
                                                        disabled={rescanningId === s.id}
                                                        className={`p-1.5 rounded-full backdrop-blur border shadow-md transition-all bg-black/60 border-white/10 text-white/40 hover:text-blue-400 hover:bg-blue-400/20 hover:border-blue-400/40 opacity-0 group-hover:opacity-100 disabled:opacity-100 disabled:cursor-not-allowed`}
                                                        title={t('home.seriesRescan')}
                                                    >
                                                        <RefreshCw className={`w-3.5 h-3.5 ${rescanningId === s.id ? 'animate-spin text-blue-400' : ''}`} />
                                                    </button>
                                                    <button
                                                        onClick={(e) => handleToggleFavorite(e, s.id, s.is_favorite)}
                                                        className={`p-1.5 rounded-full backdrop-blur border shadow-md transition-all ${s.is_favorite
                                                            ? 'bg-red-500/20 border-red-500/40 text-red-500'
                                                            : 'bg-black/60 border-white/10 text-white/40 hover:text-red-400 hover:bg-red-400/20 hover:border-red-400/40 opacity-0 group-hover:opacity-100'
                                                            }`}
                                                    >
                                                        <Heart className={`w-3.5 h-3.5 ${s.is_favorite ? 'fill-current' : ''}`} />
                                                    </button>
                                                </div>
                                            )}
                                        </div>
                                        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/60 to-transparent p-3 pt-8 z-10 pointer-events-none">
                                            <div className="flex justify-between text-[11px] font-medium text-gray-300">
                                                <span>
                                                    {s.volume_count > 0 ? t('home.seriesCountsWithVolumes', { volumes: s.volume_count, books: s.actual_book_count }) : t('home.seriesCountsBooksOnly', { books: s.actual_book_count })}
                                                </span>
                                                <span>{formatNumber(s.total_pages?.Valid ? s.total_pages.Float64 : 0)} P</span>
                                            </div>
                                            {s.total_pages?.Valid && s.total_pages.Float64 > 0 && (
                                                <div className="w-full h-1 bg-gray-700/60 rounded-full mt-1.5 overflow-hidden">
                                                    <div
                                                        className={`h-full ${s.read_count >= s.total_pages.Float64 ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                                                        style={{ width: `${Math.min(100, (s.read_count / s.total_pages.Float64) * 100)}%` }}
                                                    />
                                                </div>
                                            )}
                                        </div>
                                    </div>
                                    <div className="p-3">
                                        <div>
                                            <h4 className="text-sm font-bold text-gray-200 line-clamp-1 leading-tight group-hover:text-komgaPrimary transition-colors mb-1.5">
                                                {s.title?.Valid ? s.title.String : s.name}
                                            </h4>
                                            {s.summary?.Valid && (
                                                <p className="text-[11px] text-gray-500 line-clamp-2 leading-tight opacity-70">
                                                    {s.summary.String}
                                                </p>
                                            )}
                                        </div>
                                        {externalSession && (
                                            <div className="mt-3 rounded-lg border border-gray-800 bg-gray-950/70 px-3 py-2">
                                                <div className="flex items-center justify-between gap-2">
                                                    <span className={`rounded-full px-2 py-0.5 text-[10px] font-bold ${externalStatusClass}`}>
                                                        {externalStatusLabel}
                                                    </span>
                                                    <span className="text-[11px] font-medium text-gray-400">
                                                        {externalMatched}/{externalTotal}
                                                    </span>
                                                </div>
                                                <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-800">
                                                    <div
                                                        className={`h-full transition-all ${externalSyncStatus === 'complete'
                                                                ? 'bg-emerald-400'
                                                                : externalSyncStatus === 'partial'
                                                                    ? 'bg-amber-400'
                                                                    : 'bg-gray-500'
                                                            }`}
                                                        style={{ width: `${externalPercent}%` }}
                                                    />
                                                </div>
                                            </div>
                                        )}
                                        {/* 移除底部的“系列”字样，保持清爽 */}
                                    </div>
                                </Link>
                            );
                        })}
                    </div>

                    {/* 分页控制栏 */}
                    <div className="mt-12 mb-8 flex flex-col xl:flex-row items-center justify-between gap-6 border-t border-gray-800 pt-8">
                        <div className="flex items-center gap-4 text-sm">
                            <span className="text-gray-500">
                                {t('home.pagination.totalSeries', { count: totalSeries })}
                            </span>
                            <div className="h-4 w-px bg-gray-800"></div>
                            <div className="flex items-center gap-2 text-gray-400">
                                {t('home.pagination.pageSize')}
                                <select
                                    value={pageSize}
                                    onChange={(e) => {
                                        setPageSize(Number(e.target.value));
                                        setPage(1);
                                    }}
                                    className="bg-transparent border border-gray-700 text-white rounded focus:ring-komgaPrimary focus:border-komgaPrimary px-1 py-0.5 outline-none transition-colors"
                                >
                                    <option value={30}>30</option>
                                    <option value={50}>50</option>
                                    <option value={100}>100</option>
                                </select>
                            </div>
                            <div className="h-4 w-px bg-gray-800"></div>
                            <span className="text-gray-500">
                                {t('home.pagination.currentPage', { page, total: Math.ceil(totalSeries / pageSize) || 1 })}
                            </span>
                        </div>
                        <div className="flex items-center gap-2">
                            <button
                                onClick={() => setPage(1)}
                                disabled={page === 1}
                                className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                {t('home.pagination.first')}
                            </button>
                            <button
                                onClick={() => setPage(p => Math.max(1, p - 1))}
                                disabled={page === 1}
                                className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                {t('home.pagination.prev')}
                            </button>

                            <div className="flex items-center gap-1 mx-1 sm:mx-2 overflow-x-auto">
                                {[...Array(Math.min(5, Math.ceil(totalSeries / pageSize) || 1))].map((_, i) => {
                                    const totalPages = Math.ceil(totalSeries / pageSize) || 1;
                                    let pNum = page;

                                    if (page <= 3) {
                                        pNum = i + 1;
                                    } else if (page >= totalPages - 2) {
                                        pNum = Math.max(1, totalPages - 4 + i);
                                    } else {
                                        pNum = page - 2 + i;
                                    }

                                    if (pNum <= 0 || pNum > totalPages) return null;

                                    return (
                                        <button
                                            key={`page-${i}-${pNum}`}
                                            onClick={() => setPage(pNum)}
                                            className={`w-8 h-8 sm:w-9 sm:h-9 shrink-0 flex items-center justify-center rounded-lg text-sm font-bold transition-all ${page === pNum ? 'bg-komgaPrimary text-white shadow-md' : 'bg-transparent text-gray-400 hover:bg-white/5 hover:text-white'}`}
                                        >
                                            {pNum}
                                        </button>
                                    );
                                })}
                            </div>

                            <button
                                onClick={() => setPage(p => Math.min(Math.ceil(totalSeries / pageSize) || 1, p + 1))}
                                disabled={page >= (Math.ceil(totalSeries / pageSize) || 1)}
                                className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                {t('home.pagination.next')}
                            </button>
                            <button
                                onClick={() => setPage(Math.ceil(totalSeries / pageSize) || 1)}
                                disabled={page >= (Math.ceil(totalSeries / pageSize) || 1)}
                                className="px-3 py-1.5 bg-gray-900 border border-gray-800 rounded-lg text-gray-400 hover:text-white disabled:opacity-30 disabled:cursor-not-allowed transition-colors text-sm font-medium"
                            >
                                {t('home.pagination.last')}
                            </button>
                            
                            <div className="hidden sm:flex items-center gap-2 ml-2 pl-4 border-l border-gray-800 text-sm text-gray-500">
                                {t('home.pagination.jumpTo')}
                                <input
                                    type="number"
                                    min={1}
                                    max={Math.ceil(totalSeries / pageSize) || 1}
                                    className="w-14 select-text bg-gray-900 border border-gray-800 rounded-lg text-white text-center py-1 focus:border-komgaPrimary outline-none placeholder:text-gray-700"
                                    placeholder={page.toString()}
                                    onMouseDown={(e) => e.stopPropagation()}
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            const val = parseInt(e.currentTarget.value);
                                            const max = Math.ceil(totalSeries / pageSize) || 1;
                                            if (val > 0 && val <= max) setPage(val);
                                            e.currentTarget.value = '';
                                        }
                                    }}
                                />
                                {t('home.pagination.page')}
                            </div>
                        </div>
                    </div>


                    {/* 悬浮多选操作栏 */}
                    {isSelectionMode && selectedSeries.length > 0 && (
                        <div className="fixed bottom-8 left-1/2 -translate-x-1/2 w-max max-w-[95vw] bg-gray-900 border border-gray-700 shadow-[0_20px_50px_-12px_rgba(0,0,0,0.8)] rounded-2xl px-4 sm:px-6 py-4 flex flex-wrap justify-center items-center gap-4 sm:gap-6 z-50 animate-in slide-in-from-bottom-5">
                            <span className="text-white font-medium text-sm whitespace-nowrap shrink-0">
                                {t('home.selection.selectedCount', { count: selectedSeries.length })}
                                {currentPageSelectedCount > 0 ? ` · ${t('home.selection.currentPageCount', { count: currentPageSelectedCount })}` : ''}
                            </span>
                            <div className="flex flex-wrap items-center justify-center gap-2 sm:gap-3">
                                <button
                                    onClick={() => handleBulkFavoriteUpdate(true)}
                                    className="bg-red-500/10 hover:bg-red-500/20 text-red-500 border border-red-500/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2 whitespace-nowrap"
                                >
                                    <Heart className="w-4 h-4 fill-current" /> {t('home.selection.markFavorite')}
                                </button>
                                <button
                                    onClick={() => handleBulkFavoriteUpdate(false)}
                                    className="bg-gray-800 hover:bg-gray-700 text-gray-300 border border-gray-700 px-4 py-2 rounded-lg text-sm font-medium transition-colors whitespace-nowrap"
                                >
                                    {t('home.selection.removeFavorite')}
                                </button>
                                <button
                                    onClick={() => setShowCollectionModal(true)}
                                    className="bg-komgaPrimary/10 hover:bg-komgaPrimary/20 text-komgaPrimary border border-komgaPrimary/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2 whitespace-nowrap"
                                >
                                    <FolderHeart className="w-4 h-4" /> {t('home.selection.addToCollection')}
                                </button>
                                <button
                                    onClick={handleTransferSelectedSeries}
                                    disabled={startingTransfer || !externalSession || externalSession.status !== 'ready'}
                                    className="bg-blue-500/10 hover:bg-blue-500/20 text-blue-300 border border-blue-500/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors flex items-center gap-2 whitespace-nowrap disabled:opacity-40 disabled:cursor-not-allowed"
                                >
                                    <Send className="w-4 h-4" /> {startingTransfer ? t('home.transfer.submitting') : t('home.transfer.action')}
                                </button>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* 添加到合集弹窗 */}
            {showCollectionModal && selectedSeries.length > 0 && (
                <AddToCollectionModal
                    seriesIds={selectedSeries}
                    onClose={() => setShowCollectionModal(false)}
                    onSuccess={() => {
                        showToast(t('home.selection.addToCollectionSuccess', { count: selectedSeries.length }), 'success');
                        setSelectedSeries([]);
                        setIsSelectionMode(false);
                    }}
                />
            )}

            <ModalShell
                open={showTransferConfirmModal}
                onClose={() => {
                    if (startingTransfer) return;
                    setShowTransferConfirmModal(false);
                    setPendingTransferSummary(null);
                }}
                title={t('home.transfer.title')}
                description={t('home.transfer.description')}
                icon={<PackageCheck className="h-5 w-5" />}
                size="compact"
                closeOnBackdrop={!startingTransfer}
                footer={
                    <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
                        <button
                            onClick={() => {
                                if (startingTransfer) return;
                                setShowTransferConfirmModal(false);
                                setPendingTransferSummary(null);
                            }}
                            className={modalGhostButtonClass}
                            disabled={startingTransfer}
                        >
                            {t('modal.cancel')}
                        </button>
                        <button
                            onClick={submitTransferSelectedSeries}
                            className={modalPrimaryButtonClass}
                            disabled={startingTransfer}
                        >
                            {startingTransfer ? (
                                <>
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                    {t('home.transfer.submitting')}
                                </>
                            ) : (
                                <>
                                    <Send className="h-4 w-4" />
                                    {t('home.transfer.confirm')}
                                </>
                            )}
                        </button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <div className="rounded-2xl border border-gray-800 bg-gray-950/60 p-4">
                        <p className="text-sm text-gray-300 leading-6">
                            {t('home.transfer.summary', { count: selectedSeries.length })}
                        </p>
                        <p className="mt-2 break-all text-xs text-gray-500">{externalSession?.external_path}</p>
                    </div>

                    <div className="grid grid-cols-3 gap-3 text-sm">
                        <div className="rounded-2xl border border-blue-500/20 bg-blue-500/10 p-4">
                            <p className="text-blue-300">{t('home.transfer.targetBooks')}</p>
                            <p className="mt-2 text-2xl font-semibold text-white">{pendingTransferSummary?.total ?? 0}</p>
                        </div>
                        <div className="rounded-2xl border border-emerald-500/20 bg-emerald-500/10 p-4">
                            <p className="text-emerald-300">{t('home.transfer.alreadyExists')}</p>
                            <p className="mt-2 text-2xl font-semibold text-white">{pendingTransferSummary?.matched ?? 0}</p>
                        </div>
                        <div className="rounded-2xl border border-amber-500/20 bg-amber-500/10 p-4">
                            <p className="text-amber-300">{t('home.transfer.toCopy')}</p>
                            <p className="mt-2 text-2xl font-semibold text-white">{pendingTransferSummary?.missing ?? 0}</p>
                        </div>
                    </div>

                    <div className="rounded-2xl border border-gray-800 bg-black/20 px-4 py-3 text-xs leading-6 text-gray-400">
                        {t('home.transfer.hint')}
                    </div>
                </div>
            </ModalShell>

            {/* Toast 通知 */}
            {toastMsg && (
                <div className="fixed bottom-6 right-6 z-50 animate-in slide-in-from-bottom-5 fade-in duration-300">
                    <div className={`px-4 py-3 rounded-lg shadow-lg flex items-center gap-3 border ${toastMsg.type === 'success' ? 'bg-green-900 border-green-700 text-green-100' : 'bg-red-900 border-red-700 text-red-100'
                        }`}>
                        <span className="text-sm font-medium">{toastMsg.text}</span>
                        <button onClick={() => setToastMsg(null)} className="ml-2 text-white/50 hover:text-white">✕</button>
                    </div>
                </div>
            )}
        </div>
    );
}

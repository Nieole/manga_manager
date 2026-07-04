/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

import { Outlet, Link, useParams, useNavigate, useLocation } from 'react-router-dom';
import { useState, useEffect, useRef, lazy, Suspense, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import axios from 'axios';
import { apiClient } from '../api/client';
import { Activity, BookOpen, ClipboardCheck, FolderOpen, Plus, X, Loader2, RefreshCw, Search, Trash2, Settings as SettingsIcon, Menu, LayoutDashboard, FolderHeart, Download, Eraser, MoreHorizontal, Sparkles, PanelLeftClose, PanelLeftOpen, ListOrdered, GitCompareArrows, HardDriveDownload, ChevronDown, Wrench, HelpCircle } from 'lucide-react';
import { DEFAULT_SCAN_FORMATS, DEFAULT_SCAN_INTERVAL } from './layout/constants';
import type { BrowseDirEntry, BrowseDrive, Library, SearchHit } from './layout/types';
import { useGlobalSearch } from './layout/useGlobalSearch';
import { ConfirmDialog } from './ui/ConfirmDialog';
import { useI18n } from '../i18n/LocaleProvider';
import { withApiToken } from '../utils/apiAuth';
import { useToast } from './ToastProvider';
import { ShortcutsPanel } from './ShortcutsPanel';
import { SidebarTaskBubble, type TaskBubbleEntry } from './SidebarTaskBubble';

const LibraryFormModal = lazy(() => import('./layout/LibraryFormModal').then((module) => ({ default: module.LibraryFormModal })));
const SearchModal = lazy(() => import('./layout/SearchModal').then((module) => ({ default: module.SearchModal })));

interface ConfirmDialogState {
    open: boolean;
    title: string;
    description?: string;
    confirmLabel?: string;
    tone?: 'primary' | 'warning' | 'danger';
    onConfirm: (() => Promise<void> | void) | null;
}

interface SidebarGroupProps {
    label: string;
    collapsed: boolean;
    expanded: boolean;
    onToggle: () => void;
    collapsedIcon: ReactNode;
    children: ReactNode;
}

function SidebarGroup({ label, collapsed, expanded, onToggle, collapsedIcon, children }: SidebarGroupProps) {
    return (
        <div className="space-y-1">
            {!collapsed ? (
                <button
                    onClick={onToggle}
                    className="w-full flex items-center justify-between px-3 py-1.5 text-[11px] font-semibold tracking-wider text-gray-500 uppercase rounded-md hover:bg-gray-800/40 hover:text-gray-300 transition-colors"
                >
                    <span>{label}</span>
                    <ChevronDown className={`w-3 h-3 transition-transform duration-200 ${expanded ? 'rotate-0' : '-rotate-90'}`} />
                </button>
            ) : (
                <div className="w-full flex justify-center py-2 text-gray-600">
                    {collapsedIcon}
                </div>
            )}
            {(expanded || collapsed) && <div className="space-y-0.5">{children}</div>}
        </div>
    );
}

interface SidebarLinkProps {
    to: string;
    icon: ReactNode;
    label: string;
    collapsed: boolean;
    pathname: string;
    matcher?: (pathname: string) => boolean;
    exact?: boolean;
    onClick?: () => void;
}

function SidebarLink({ to, icon, label, collapsed, pathname, matcher, exact, onClick }: SidebarLinkProps) {
    const active = matcher ? matcher(pathname) : exact ? pathname === to : pathname === to;
    return (
        <Link
            to={to}
            onClick={onClick}
            title={label}
            className={`w-full flex items-center gap-3 px-3 py-2 rounded-md transition-colors text-sm border-l-2 ${
                active
                    ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium border-komgaPrimary'
                    : 'text-gray-400 border-transparent hover:bg-gray-800/40 hover:text-white'
            } ${collapsed ? 'md:justify-center md:px-0 md:border-l-0' : ''}`}
        >
            {icon}
            <span className={collapsed ? 'md:hidden' : 'block'}>{label}</span>
        </Link>
    );
}

export default function Layout() {
    const { t } = useI18n();
    const [recentLibraryPaths, setRecentLibraryPaths] = useState<string[]>([]);
    const [supportedScanFormats, setSupportedScanFormats] = useState(DEFAULT_SCAN_FORMATS);
    const [libraries, setLibraries] = useState<Library[]>([]);
    const [loading, setLoading] = useState(true);
    const [showAddModal, setShowAddModal] = useState(false);
    const [isSidebarOpen, setIsSidebarOpen] = useState(false);
    const [isDesktopSidebarCollapsed, setIsDesktopSidebarCollapsed] = useState(() => {
        try {
            return localStorage.getItem('manga_manager_sidebar_collapsed') === 'true';
        } catch { return false; }
    });
    const [openMenuId, setOpenMenuId] = useState<string | null>(null);
    const [menuPos, setMenuPos] = useState<{ top: number; left: number } | null>(null);
    const [isShelfExpanded, setIsShelfExpanded] = useState(true);
    const [isViewsExpanded, setIsViewsExpanded] = useState(true);
    const [isCurateExpanded, setIsCurateExpanded] = useState(true);
    const [isSystemExpanded, setIsSystemExpanded] = useState(true);
    const [isLibrariesExpanded, setIsLibrariesExpanded] = useState(() => {
        try {
            const stored = localStorage.getItem('manga_manager_libraries_expanded');
            return stored == null ? true : stored === 'true';
        } catch { return true; }
    });
    const [librariesQuery, setLibrariesQuery] = useState('');
    const toggleLibrariesExpanded = () => {
        setIsLibrariesExpanded((prev) => {
            const next = !prev;
            try {
                localStorage.setItem('manga_manager_libraries_expanded', String(next));
            } catch { /* ignore */ }
            return next;
        });
    };

    const toggleDesktopSidebar = () => {
        setIsDesktopSidebarCollapsed(prev => {
            const next = !prev;
            localStorage.setItem('manga_manager_sidebar_collapsed', String(next));
            return next;
        });
    };
    const [newLibName, setNewLibName] = useState("");
    const [newLibPath, setNewLibPath] = useState("");
    const [newLibScanMode, setNewLibScanMode] = useState("none");
    const [newLibKOReaderSyncEnabled, setNewLibKOReaderSyncEnabled] = useState(true);
    const [newLibScanInterval, setNewLibScanInterval] = useState(DEFAULT_SCAN_INTERVAL);
    const [newLibScanFormats, setNewLibScanFormats] = useState(DEFAULT_SCAN_FORMATS);
    const [adding, setAdding] = useState(false);

    // 编辑资源库状态
    const [showEditModal, setShowEditModal] = useState(false);
    const [editLibId, setEditLibId] = useState("");
    const [editLibName, setEditLibName] = useState("");
    const [editLibPath, setEditLibPath] = useState("");
    const [editLibScanMode, setEditLibScanMode] = useState("none");
    const [editLibKOReaderSyncEnabled, setEditLibKOReaderSyncEnabled] = useState(true);
    const [editLibScanInterval, setEditLibScanInterval] = useState(DEFAULT_SCAN_INTERVAL);
    const [editLibScanFormats, setEditLibScanFormats] = useState(DEFAULT_SCAN_FORMATS);
    const [editing, setEditing] = useState(false);

    // 用于向所有 Outlet 子路由向下传递全局刷新信号的计数器
    const [refreshTrigger, setRefreshTrigger] = useState(0);

    // 全局任务进度状态
    const [taskBubbleEntries, setTaskBubbleEntries] = useState<Record<string, TaskBubbleEntry>>({});
    const taskBubbleCleanupTimers = useRef<Map<string, number>>(new Map());

    // 文件夹浏览器状态
    const [browsing, setBrowsing] = useState(false);
    const [browseDirs, setBrowseDirs] = useState<BrowseDirEntry[]>([]);
    const [browseCurrent, setBrowseCurrent] = useState('');
    const [browseParent, setBrowseParent] = useState('');
    const [browseDrives, setBrowseDrives] = useState<BrowseDrive[]>([]);
    const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>({
        open: false,
        title: '',
        description: '',
        confirmLabel: undefined,
        tone: 'primary',
        onConfirm: null,
    });
    const [confirmLoading, setConfirmLoading] = useState(false);
    const [shortcutsOpen, setShortcutsOpen] = useState(false);

    const {
        searchQuery,
        setSearchQuery,
        searchResults,
        isSearchModalOpen,
        setIsSearchModalOpen,
        selectedIndex,
        setSelectedIndex,
        searchTarget,
        setSearchTarget,
        resetSearch,
    } = useGlobalSearch();

    const { libId } = useParams();
    const navigate = useNavigate();
    const location = useLocation();
    const { showToast } = useToast();

    const openConfirmDialog = (next: Omit<ConfirmDialogState, 'open'>) => {
        setConfirmDialog({ open: true, ...next });
    };

    const openEditLibraryModal = (libraryId: string | number) => {
        const target = libraries.find((lib) => String(lib.id) === String(libraryId));
        if (!target) return;
        setEditLibId(String(target.id));
        setEditLibName(target.name || "");
        setEditLibPath(target.path || "");
        setEditLibScanMode(target.scan_mode || "none");
        setEditLibKOReaderSyncEnabled(target.koreader_sync_enabled ?? true);
        setEditLibScanInterval(target.scan_interval || DEFAULT_SCAN_INTERVAL);
        setEditLibScanFormats(target.scan_formats || supportedScanFormats);
        setShowEditModal(true);
    };

    const saveRecentLibraryPath = (path: string) => {
        const normalized = path.trim();
        if (!normalized) return;
        const next = [normalized, ...recentLibraryPaths.filter((item) => item !== normalized)].slice(0, 5);
        setRecentLibraryPaths(next);
        localStorage.setItem('manga_manager_recent_library_paths', JSON.stringify(next));
    };

    const extractErrorMessage = (error: unknown, fallback: string) => {
        if (axios.isAxiosError(error)) {
            const validationIssues = error.response?.data?.validation?.issues;
            if (Array.isArray(validationIssues) && validationIssues.length > 0) {
                return validationIssues.map((issue: { field: string; message: string }) => `${issue.field}: ${issue.message}`).join('\n');
            }
            return error.response?.data?.error || fallback;
        }
        return fallback;
    };

    const openDirectoryBrowser = () => {
        setBrowsing(true);
        apiClient.get('/api/browse-dirs')
            .then(res => {
                setBrowseDirs(res.data.dirs || []);
                setBrowseCurrent(res.data.current);
                setBrowseParent(res.data.parent);
                setBrowseDrives(res.data.drives || []);
            })
            .catch(() => { });
    };

    const navigateDirectoryBrowser = (path: string) => {
        apiClient.get(`/api/browse-dirs?path=${encodeURIComponent(path)}`)
            .then(res => {
                setBrowseDirs(res.data.dirs || []);
                setBrowseCurrent(res.data.current);
                setBrowseParent(res.data.parent);
                setBrowseDrives(res.data.drives || []);
            });
    };

    const handleSelectResult = (hit: SearchHit) => {
        setIsSearchModalOpen(false);
        resetSearch();

        const isSeries = hit.fields?.type === 'series' || hit.id.startsWith('s_');
        if (isSeries) {
            navigate(`/series/${hit.id.replace('s_', '')}`);
        } else {
            navigate(`/reader/${hit.id.replace('b_', '')}`);
        }
    };

    const handleSearchKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            setSelectedIndex(prev => Math.min(prev + 1, Math.max(0, searchResults.length - 1)));
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            setSelectedIndex(prev => Math.max(prev - 1, 0));
        } else if (e.key === 'Enter') {
            e.preventDefault();
            if (searchResults[selectedIndex]) {
                handleSelectResult(searchResults[selectedIndex]);
            }
        } else if (e.key === 'Escape') {
            setIsSearchModalOpen(false);
        }
    };

    const modalFallback = (
        <div className="fixed inset-0 z-95 flex items-center justify-center bg-black/50 backdrop-blur-xs">
            <div className="flex items-center gap-3 rounded-2xl border border-gray-800 bg-gray-900/90 px-5 py-4 text-sm text-gray-300 shadow-xl shadow-black/40">
                <Loader2 className="h-4 w-4 animate-spin text-komgaPrimary" />
                <span>{t('common.loading')}</span>
            </div>
        </div>
    );

    const fetchLibraries = () => {
        setLoading(true);
        apiClient.get('/api/libraries')
            .then(res => {
                setLibraries(res.data);
                if (res.data.length > 0 && !libId && location.pathname === '/') {
                    // 仅在首页时默认跳转到第一个资源库，避免覆盖 /series/xxx 等子路由
                    navigate(`/library/${res.data[0].id}`, { replace: true });
                }
                setLoading(false);
            })
            .catch(err => {
                console.error("Failed to load libraries", err);
                setLoading(false);
            });
    };

    useEffect(() => {
        const cleanupTimers = taskBubbleCleanupTimers.current;
        fetchLibraries();
        try {
            const stored = localStorage.getItem('manga_manager_recent_library_paths');
            if (stored) {
                const parsed = JSON.parse(stored);
                if (Array.isArray(parsed)) {
                    setRecentLibraryPaths(parsed.filter((item) => typeof item === 'string'));
                }
            }
        } catch {
            // ignore invalid local storage
        }
        apiClient.get('/api/system/capabilities')
            .then((res) => {
                if (res.data?.default_scan_formats) {
                    setSupportedScanFormats(res.data.default_scan_formats);
                    setNewLibScanFormats(res.data.default_scan_formats);
                    setEditLibScanFormats(res.data.default_scan_formats);
                }
            })
            .catch(() => { });

        const openAddLibrary = () => setShowAddModal(true);
        const openEditLibrary = (event: Event) => {
            const customEvent = event as CustomEvent<{ libraryId?: string | number }>;
            if (customEvent.detail?.libraryId != null) {
                openEditLibraryModal(customEvent.detail.libraryId);
            }
        };
        window.addEventListener('manga-manager:open-add-library', openAddLibrary);
        window.addEventListener('manga-manager:open-edit-library', openEditLibrary as EventListener);
        const handleTaskProgressOverride = (event: Event) => {
            const customEvent = event as CustomEvent<{
                key?: string;
                status?: string;
                message?: string;
                error?: string;
                current?: number;
                total?: number;
                type?: string;
            }>;
            const detail = customEvent.detail;
            if (!detail?.key) return;

            setTaskBubbleEntries((prev) => {
                const existing = prev[detail.key!];
                if (!existing) return prev;
                return {
                    ...prev,
                    [detail.key!]: {
                        ...existing,
                        status: detail.status || existing.status,
                        message: detail.message || existing.message,
                        error: detail.error ?? existing.error,
                        current: detail.current ?? existing.current,
                        total: detail.total ?? existing.total,
                        type: detail.type || existing.type,
                        updatedAt: Date.now(),
                    },
                };
            });
        };
        window.addEventListener('manga-manager:task-progress-override', handleTaskProgressOverride as EventListener);

        // 挂载 Server-Sent Events 流监听器（启用鉴权时通过 token 查询参数携带令牌）
        const eventSource = new EventSource(withApiToken('/api/events'));

        eventSource.onmessage = (event) => {
            const data = event.data as string;
            if (data === "refresh") {
                console.log("Receive SSE refresh signal, triggering child refresh...");
                // 仅递增刷新信号通知当前活跃的子页面重新拉取数据
                // 不再调用 fetchLibraries()：避免闭包捕获过期路由状态导致页面跳转，
                // 同时侧边栏资源库列表无需因扫描而刷新
                setRefreshTrigger(prev => prev + 1);
            } else if (data.startsWith('task_progress:')) {
                try {
                    const progress = JSON.parse(data.slice('task_progress:'.length));
                    window.dispatchEvent(new CustomEvent('manga-manager:task-progress', { detail: progress }));
                    if (progress.key) {
                        const entry: TaskBubbleEntry = {
                            key: progress.key,
                            type: progress.type || '',
                            status: progress.status || 'running',
                            message: progress.message || '',
                            error: progress.error,
                            current: progress.current ?? 0,
                            total: progress.total ?? 0,
                            scope_name: progress.scope_name,
                            updatedAt: Date.now(),
                        };
                        setTaskBubbleEntries((prev) => ({ ...prev, [progress.key]: entry }));
                        const existingTimer = taskBubbleCleanupTimers.current.get(progress.key);
                        if (existingTimer) {
                            clearTimeout(existingTimer);
                            taskBubbleCleanupTimers.current.delete(progress.key);
                        }
                        if (entry.status === 'completed' || entry.status === 'failed' || entry.status === 'canceled') {
                            const timer = window.setTimeout(() => {
                                setTaskBubbleEntries((prev) => {
                                    if (!prev[progress.key]) return prev;
                                    const next = { ...prev };
                                    delete next[progress.key];
                                    return next;
                                });
                                taskBubbleCleanupTimers.current.delete(progress.key);
                            }, entry.status === 'completed' ? 8000 : 20000);
                            taskBubbleCleanupTimers.current.set(progress.key, timer);
                        }
                    }
                } catch (e) {
                    console.warn('Failed to parse task progress SSE:', e);
                }
            }
        };

        eventSource.onerror = (error) => {
            console.error("SSE connection error:", error);
            // EventSource 默认会自己处理重连
        };

        return () => {
            window.removeEventListener('manga-manager:open-add-library', openAddLibrary);
            window.removeEventListener('manga-manager:open-edit-library', openEditLibrary as EventListener);
            window.removeEventListener('manga-manager:task-progress-override', handleTaskProgressOverride as EventListener);
            cleanupTimers.forEach((timer) => clearTimeout(timer));
            cleanupTimers.clear();
            eventSource.close();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useEffect(() => {
        let pendingPrefix: string | null = null;
        let pendingTimer: number | null = null;
        const isEditable = (target: EventTarget | null) => {
            if (!(target instanceof HTMLElement)) return false;
            const tag = target.tagName;
            if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
            if (target.isContentEditable) return true;
            return false;
        };
        const clearPrefix = () => {
            pendingPrefix = null;
            if (pendingTimer) {
                window.clearTimeout(pendingTimer);
                pendingTimer = null;
            }
        };
        const handler = (e: KeyboardEvent) => {
            if (e.metaKey || e.ctrlKey || e.altKey) {
                if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
                    e.preventDefault();
                    setIsSearchModalOpen(true);
                }
                return;
            }
            if (isEditable(e.target)) return;
            if (e.key === '?' || (e.shiftKey && e.key === '/')) {
                e.preventDefault();
                setShortcutsOpen((prev) => !prev);
                clearPrefix();
                return;
            }
            if (e.key === '/') {
                e.preventDefault();
                setIsSearchModalOpen(true);
                clearPrefix();
                return;
            }
            if (e.key === 'Escape') {
                setShortcutsOpen(false);
                clearPrefix();
                return;
            }
            if (e.key === '[') {
                e.preventDefault();
                toggleDesktopSidebar();
                clearPrefix();
                return;
            }
            if (pendingPrefix === 'g') {
                const key = e.key.toLowerCase();
                const map: Record<string, string> = {
                    h: '/',
                    r: '/reviews',
                    o: '/ops',
                    c: '/collections',
                    l: '/reading-lists',
                    s: '/settings',
                    f: '/offline',
                };
                if (map[key]) {
                    e.preventDefault();
                    navigate(map[key]);
                }
                clearPrefix();
                return;
            }
            if (e.key === 'g') {
                pendingPrefix = 'g';
                if (pendingTimer) window.clearTimeout(pendingTimer);
                pendingTimer = window.setTimeout(clearPrefix, 1200);
            }
        };
        window.addEventListener('keydown', handler);
        return () => {
            window.removeEventListener('keydown', handler);
            if (pendingTimer) window.clearTimeout(pendingTimer);
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [navigate]);

    const handleAddLibrary = async (e: React.FormEvent) => {
        e.preventDefault();
        setAdding(true);
        try {
            await apiClient.post('/api/libraries', {
                name: newLibName,
                path: newLibPath,
                scan_mode: newLibScanMode,
                koreader_sync_enabled: newLibKOReaderSyncEnabled,
                scan_interval: newLibScanInterval,
                scan_formats: newLibScanFormats
            });
            setShowAddModal(false);
            setNewLibName("");
            setNewLibPath("");
            setNewLibScanMode("none");
            setNewLibKOReaderSyncEnabled(true);
            setNewLibScanInterval(DEFAULT_SCAN_INTERVAL);
            setNewLibScanFormats(DEFAULT_SCAN_FORMATS);
            saveRecentLibraryPath(newLibPath);
            fetchLibraries();
            setRefreshTrigger(prev => prev + 1);
        } catch (error) {
            console.error(error);
            showToast(extractErrorMessage(error, t('layout.toast.addLibraryFailed')), 'error');
        } finally {
            setAdding(false);
        }
    };

    const handleEditLibrarySubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setEditing(true);
        try {
            await apiClient.put(`/api/libraries/${editLibId}`, {
                name: editLibName,
                path: editLibPath,
                scan_mode: editLibScanMode,
                koreader_sync_enabled: editLibKOReaderSyncEnabled,
                scan_interval: editLibScanInterval,
                scan_formats: editLibScanFormats
            });
            setShowEditModal(false);
            saveRecentLibraryPath(editLibPath);
            fetchLibraries();
        } catch (error) {
            console.error(error);
            showToast(extractErrorMessage(error, t('layout.toast.editLibraryFailed')), 'error');
        } finally {
            setEditing(false);
        }
    };

    const handleScanLibrary = async (id: string, force: boolean = false) => {
        try {
            await apiClient.post(`/api/libraries/${id}/scan?force=${force}`);
            // 不必手动刷新界面，后端的 SSE 会通过 onmessage 广播数据到达
            showToast(force ? t('layout.toast.scanForcedQueued') : t('layout.toast.scanIncrementalQueued'), 'success');
        } catch (error) {
            console.error("Trigger scan failed", error);
            showToast(t('layout.toast.scanFailed'), 'error');
        }
    };

    const handleScrapeLibrary = async (id: string) => {
        try {
            await apiClient.post(`/api/libraries/${id}/scrape`, { provider: 'bangumi' });
            showToast(t('layout.toast.scrapeQueued'), 'success');
        } catch (error) {
            console.error("Trigger scrape failed", error);
            showToast(t('layout.toast.scrapeFailed'), 'error');
        }
    };

    const handleCleanupLibrary = async (id: string) => {
        try {
            await apiClient.post(`/api/libraries/${id}/cleanup`);
            showToast(t('layout.toast.cleanupQueued'), 'success');
        } catch (error) {
            console.error("Trigger cleanup failed", error);
            showToast(t('layout.toast.cleanupFailed'), 'error');
        }
    };

    const handleAIGrouping = async (id: string) => {
        try {
            await apiClient.post(`/api/libraries/${id}/ai-grouping`);
            showToast(t('layout.toast.aiGroupingQueued'), 'success');
        } catch (error) {
            console.error("Trigger AI grouping failed", error);
            showToast(t('layout.toast.aiGroupingFailed'), 'error');
        }
    };

    const handleDeleteLibrary = async (library: Library) => {
        try {
            await apiClient.delete(`/api/libraries/${library.id}`);
            showToast(t('layout.toast.libraryDeleted', { name: library.name }), 'success');
            fetchLibraries();
            navigate('/');
        } catch (error) {
            console.error('Delete library failed', error);
            showToast(t('layout.toast.deleteFailed'), 'error');
        }
    };

    return (
        <div className="min-h-screen bg-komgaDark text-gray-200 font-sans flex flex-col relative">
            <header className="bg-komgaSurface shadow-md sticky top-0 z-20 px-4 sm:px-6 py-4 flex items-center justify-between border-b border-gray-800">
                <div className="flex items-center">
                    <button
                        onClick={() => setIsSidebarOpen(true)}
                        className="md:hidden p-2 -ml-2 mr-2 text-gray-400 hover:text-white transition-colors"
                        title={t('layout.openMenu')}
                    >
                        <Menu className="w-6 h-6" />
                    </button>
                    <Link to="/" className="flex items-center space-x-2 sm:space-x-3 w-auto sm:w-56">
                        <BookOpen className="text-komgaPrimary h-7 w-7 sm:h-8 sm:w-8" />
                        <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-white hover:text-komgaPrimary transition hidden sm:block whitespace-nowrap">{t('app.name')}</h1>
                    </Link>
                </div>

                <div className="flex-1 max-w-xl flex justify-end sm:justify-center px-2 sm:px-4">
                    {/* 桌面端搜索框 */}
                    <button
                        onClick={() => setIsSearchModalOpen(true)}
                        className="hidden md:flex w-full max-w-md bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 items-center justify-between text-sm text-gray-500 hover:border-gray-700 hover:text-gray-300 transition-all opacity-80 hover:opacity-100 shadow-inner group overflow-hidden"
                    >
                        <div className="flex items-center min-w-0 overflow-hidden">
                            <Search className="w-4 h-4 mr-3 group-hover:text-komgaPrimary transition-colors shrink-0" />
                            <span className="truncate">{t('layout.searchPlaceholder')}</span>
                        </div>
                        <kbd className="hidden md:inline-block bg-gray-800 border border-gray-700 rounded-sm px-2 py-0.5 text-xs font-mono text-gray-400 shrink-0 ml-3">⌘K</kbd>
                    </button>
                    {/* 移动端搜索图标 */}
                    <button
                        onClick={() => setIsSearchModalOpen(true)}
                        className="md:hidden p-2 text-gray-400 hover:text-komgaPrimary hover:bg-gray-800 rounded-full transition-colors"
                        title={t('layout.searchPlaceholder')}
                        aria-label={t('layout.searchPlaceholder')}
                    >
                        <Search className="w-5 h-5" />
                    </button>
                </div>

                <div className="w-auto sm:w-64 flex justify-end items-center gap-1">
                    <button
                        type="button"
                        onClick={() => setShortcutsOpen(true)}
                        className="p-2 text-gray-400 hover:text-komgaPrimary hover:bg-gray-800 rounded-full transition-colors"
                        title={t('shortcuts.title')}
                        aria-label={t('shortcuts.title')}
                    >
                        <HelpCircle className="w-5 h-5" />
                    </button>
                    <Link
                        to="/settings"
                        className="p-2 text-gray-400 hover:text-komgaPrimary hover:bg-gray-800 rounded-full transition-colors"
                        title={t('layout.settings')}
                    >
                        <SettingsIcon className="w-6 h-6" />
                    </Link>
                </div>
            </header>

            <main className="flex-1 flex overflow-hidden relative">
                {/* 移动端侧边栏半透明深色遮罩 */}
                {isSidebarOpen && (
                    <div
                        className="fixed inset-0 bg-black/70 z-40 md:hidden backdrop-blur-xs transition-opacity"
                        onClick={() => setIsSidebarOpen(false)}
                    />
                )}

                <aside className={`fixed inset-y-0 left-0 top-[73px] z-50 bg-komgaSurface border-r border-gray-800 flex flex-col pt-4 transform transition-all duration-300 ease-in-out md:relative md:top-0 md:translate-x-0 ${isSidebarOpen ? 'translate-x-0' : '-translate-x-full'} md:h-[calc(100vh-73px)] ${isDesktopSidebarCollapsed ? 'w-64 md:w-[72px]' : 'w-64'}`}>
                    {/* 折叠按钮与顶栏控制 */}
                    <div className={`mb-4 flex items-center text-xs font-semibold uppercase tracking-wider shrink-0 ${isDesktopSidebarCollapsed ? 'md:px-0 md:justify-center' : 'px-6 justify-between'}`}>
                        <span className={`transition-opacity duration-300 text-gray-500 ${isDesktopSidebarCollapsed ? 'md:hidden' : 'block'}`}>{t('layout.sidebar.menu')}</span>
                        <button
                            onClick={toggleDesktopSidebar}
                            className="text-gray-400 hover:text-white transition-colors hidden md:block p-1 hover:bg-gray-800 rounded-md"
                            title={isDesktopSidebarCollapsed ? t('layout.sidebar.expand') : t('layout.sidebar.collapse')}
                        >
                            {isDesktopSidebarCollapsed ? <PanelLeftOpen className="w-4 h-4" /> : <PanelLeftClose className="w-4 h-4" />}
                        </button>
                    </div>

                    <div className="flex-1 overflow-y-auto overflow-x-hidden space-y-3 px-2 select-none">
                        {/* 1. 我的书架 */}
                        <SidebarGroup
                            label={t('layout.sidebar.groupShelf')}
                            collapsed={isDesktopSidebarCollapsed}
                            expanded={isShelfExpanded}
                            onToggle={() => setIsShelfExpanded(!isShelfExpanded)}
                            collapsedIcon={<LayoutDashboard className="w-5 h-5" />}
                        >
                            <SidebarLink
                                to="/"
                                exact
                                icon={<LayoutDashboard className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.overview')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                        </SidebarGroup>

                        {/* 2. 我的视图 */}
                        <SidebarGroup
                            label={t('layout.sidebar.groupViews')}
                            collapsed={isDesktopSidebarCollapsed}
                            expanded={isViewsExpanded}
                            onToggle={() => setIsViewsExpanded(!isViewsExpanded)}
                            collapsedIcon={<FolderHeart className="w-5 h-5" />}
                        >
                            <SidebarLink
                                to="/collections"
                                icon={<FolderHeart className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.collections')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                            <SidebarLink
                                to="/reading-lists"
                                icon={<ListOrdered className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.readingLists')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                            <SidebarLink
                                to="/offline"
                                icon={<HardDriveDownload className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.offlineShelf')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                        </SidebarGroup>

                        {/* 3. 整理与审核 */}
                        <SidebarGroup
                            label={t('layout.sidebar.groupCurate')}
                            collapsed={isDesktopSidebarCollapsed}
                            expanded={isCurateExpanded}
                            onToggle={() => setIsCurateExpanded(!isCurateExpanded)}
                            collapsedIcon={<Wrench className="w-5 h-5" />}
                        >
                            <SidebarLink
                                to="/organize"
                                icon={<ClipboardCheck className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.organize')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                            <SidebarLink
                                to="/reviews"
                                icon={<GitCompareArrows className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.reviewsInbox')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                matcher={(p) => p.startsWith('/reviews') || p === '/metadata-reviews' || p === '/ai-grouping-reviews'}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                        </SidebarGroup>

                        {/* 4. 系统 */}
                        <SidebarGroup
                            label={t('layout.sidebar.groupSystem')}
                            collapsed={isDesktopSidebarCollapsed}
                            expanded={isSystemExpanded}
                            onToggle={() => setIsSystemExpanded(!isSystemExpanded)}
                            collapsedIcon={<Activity className="w-5 h-5" />}
                        >
                            <SidebarLink
                                to="/ops"
                                icon={<Activity className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.ops')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                matcher={(p) => p.startsWith('/ops') || p === '/logs' || p === '/organize/tasks'}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                            <SidebarLink
                                to="/settings"
                                icon={<SettingsIcon className="w-4 h-4 shrink-0" />}
                                label={t('layout.sidebar.settings')}
                                collapsed={isDesktopSidebarCollapsed}
                                pathname={location.pathname}
                                matcher={(p) => p.startsWith('/settings')}
                                onClick={() => setIsSidebarOpen(false)}
                            />
                        </SidebarGroup>

                        {/* 分割线 */}
                        <div className="border-t border-gray-800/60 my-2 mx-3"></div>

                        {/* 资源库列表 */}
                        <div className="space-y-1">
                            {!isDesktopSidebarCollapsed ? (
                                <div className="flex items-center justify-between px-3 py-2 text-xs font-bold tracking-wider text-amber-400 uppercase rounded-lg hover:bg-gray-800/40 transition-colors group">
                                    <button
                                        type="button"
                                        onClick={toggleLibrariesExpanded}
                                        className="flex items-center gap-2 flex-1 text-left text-amber-400 hover:text-amber-300 transition-colors"
                                    >
                                        <FolderOpen className="w-4 h-4 text-amber-400" />
                                        <span>{t('layout.sidebar.libraries')}</span>
                                        {libraries.length > 0 && (
                                            <span className="text-[10px] font-mono text-amber-400/60">{libraries.length}</span>
                                        )}
                                        <ChevronDown className={`w-3 h-3 transition-transform duration-200 ${isLibrariesExpanded ? 'rotate-0' : '-rotate-90'}`} />
                                    </button>
                                    <button
                                        onClick={() => setShowAddModal(true)}
                                        className="text-gray-500 hover:text-white transition-colors"
                                        title={t('layout.sidebar.addLibrary')}
                                    >
                                        <Plus className="w-4 h-4" />
                                    </button>
                                </div>
                            ) : (
                                <div className="w-full flex justify-center py-2 text-amber-400/50">
                                    <FolderOpen className="w-5 h-5 cursor-pointer hover:text-white" onClick={() => setShowAddModal(true)} />
                                </div>
                            )}

                            {!isDesktopSidebarCollapsed && isLibrariesExpanded && libraries.length > 6 && (
                                <div className="px-2">
                                    <div className="relative">
                                        <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-500" />
                                        <input
                                            type="text"
                                            value={librariesQuery}
                                            onChange={(e) => setLibrariesQuery(e.target.value)}
                                            placeholder={t('layout.sidebar.searchLibraries')}
                                            className="w-full pl-7 pr-2 py-1.5 text-xs bg-gray-900/60 border border-gray-800 rounded-md text-gray-200 placeholder:text-gray-600 focus:outline-hidden focus:border-komgaPrimary/40 focus:bg-gray-900"
                                        />
                                        {librariesQuery && (
                                            <button
                                                type="button"
                                                onClick={() => setLibrariesQuery('')}
                                                className="absolute right-1.5 top-1/2 -translate-y-1/2 text-gray-500 hover:text-white"
                                                title={t('common.close')}
                                            >
                                                <X className="w-3 h-3" />
                                            </button>
                                        )}
                                    </div>
                                </div>
                            )}

                            {(isDesktopSidebarCollapsed || isLibrariesExpanded) && (
                            <nav className="space-y-1 overflow-y-auto max-h-[25vh]">
                        {loading ? (
                            <div className="animate-pulse px-3 py-2 bg-gray-800 rounded-md h-10 w-full mb-2" />
                        ) : libraries.length === 0 ? (
                            <div className="text-gray-500 px-3 text-sm">{t('layout.sidebar.noLibraries')}</div>
                        ) : (() => {
                            const q = librariesQuery.trim().toLowerCase();
                            const filtered = !isDesktopSidebarCollapsed && q
                                ? libraries.filter((lib) => lib.name.toLowerCase().includes(q))
                                : libraries;
                            if (filtered.length === 0) {
                                return <div className="text-gray-500 px-3 text-xs italic">{t('layout.sidebar.searchEmpty')}</div>;
                            }
                            return filtered.map(lib => (
                                <Link
                                    key={lib.id}
                                    to={`/library/${lib.id}`}
                                    onClick={() => setIsSidebarOpen(false)}
                                    title={isDesktopSidebarCollapsed ? lib.name : undefined}
                                    className={`w-full flex justify-between items-center group px-3 py-2.5 rounded-lg transition-colors duration-200 ${libId === String(lib.id)
                                        ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium'
                                        : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                                        } ${isDesktopSidebarCollapsed ? 'md:justify-center md:px-0' : ''}`}
                                >
                                    <div className={`flex items-center space-x-3 overflow-hidden min-w-0 ${isDesktopSidebarCollapsed ? 'md:space-x-0 md:overflow-visible' : ''}`}>
                                        <FolderOpen className="w-5 h-5 shrink-0" />
                                        <div className={`min-w-0 ${isDesktopSidebarCollapsed ? 'md:hidden' : 'block'}`}>
                                            <span className="truncate block">{lib.name}</span>
                                            <span className={`text-[10px] ${lib.koreader_sync_enabled ?? true ? 'text-sky-400/80' : 'text-gray-500'}`}>
                                                {lib.koreader_sync_enabled ?? true ? t('layout.sidebar.koreaderOn') : t('layout.sidebar.koreaderOff')}
                                            </span>
                                        </div>
                                    </div>
                                    {!isDesktopSidebarCollapsed && (
                                    <div>
                                        <button
                                            onClick={(e) => {
                                                e.preventDefault();
                                                e.stopPropagation();
                                                if (openMenuId === String(lib.id)) {
                                                    setOpenMenuId(null);
                                                    setMenuPos(null);
                                                } else {
                                                    const rect = e.currentTarget.getBoundingClientRect();
                                                    setMenuPos({ top: rect.bottom + 4, left: rect.right - 192 });
                                                    setOpenMenuId(String(lib.id));
                                                }
                                            }}
                                            className="text-gray-500 hover:text-white opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded-sm hover:bg-gray-700 focus:outline-hidden"
                                            title={t('common.details')}
                                        >
                                            <MoreHorizontal className="w-5 h-5" />
                                        </button>

                                        {openMenuId === String(lib.id) && menuPos && createPortal(
                                            <>
                                                <div
                                                    className="fixed inset-0 z-40"
                                                    onClick={(e) => {
                                                        e.preventDefault();
                                                        e.stopPropagation();
                                                        setOpenMenuId(null);
                                                        setMenuPos(null);
                                                    }}
                                                />
                                                <div 
                                                    className="fixed w-48 bg-gray-800 border border-gray-700 rounded-lg shadow-xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200"
                                                    style={{ top: menuPos.top, left: menuPos.left }}
                                                >
                                                    <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-gray-700 bg-gray-900">
                                                        {t('layout.libraryActions.title')}
                                                    </div>
                                                    <button
                                                        onClick={(e) => {
                                                            setOpenMenuId(null);
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            openEditLibraryModal(lib.id);
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-blue-500 hover:text-white transition-colors"
                                                    >
                                                        <SettingsIcon className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.edit')}
                                                    </button>
                                                    <button
                                                        onClick={(e) => {
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            setOpenMenuId(null);
                                                            handleScanLibrary(String(lib.id), false);
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors"
                                                    >
                                                        <RefreshCw className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.scanIncremental')}
                                                    </button>
                                                    <button
                                                        onClick={(e) => {
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            setOpenMenuId(null);
                                                            openConfirmDialog({
                                                                title: t('layout.libraryActions.confirmFullScanTitle'),
                                                                description: t('layout.libraryActions.confirmFullScanDescription'),
                                                                confirmLabel: t('layout.libraryActions.confirmRun'),
                                                                tone: 'warning',
                                                                onConfirm: () => handleScanLibrary(String(lib.id), true),
                                                            });
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-orange-500 hover:text-white transition-colors"
                                                    >
                                                        <RefreshCw className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.scanFull')}
                                                    </button>
                                                    <button
                                                        onClick={(e) => {
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            setOpenMenuId(null);
                                                            openConfirmDialog({
                                                                title: t('layout.libraryActions.confirmScrapeTitle'),
                                                                description: t('layout.libraryActions.confirmScrapeDescription'),
                                                                confirmLabel: t('layout.libraryActions.startScrape'),
                                                                tone: 'primary',
                                                                onConfirm: () => handleScrapeLibrary(String(lib.id)),
                                                            });
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-green-500 hover:text-white transition-colors"
                                                    >
                                                        <Download className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.scrape')}
                                                    </button>
                                                    <button
                                                        onClick={(e) => {
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            setOpenMenuId(null);
                                                            openConfirmDialog({
                                                                title: t('layout.libraryActions.confirmCleanupTitle'),
                                                                description: t('layout.libraryActions.confirmCleanupDescription'),
                                                                confirmLabel: t('layout.libraryActions.confirmCleanup'),
                                                                tone: 'warning',
                                                                onConfirm: () => handleCleanupLibrary(String(lib.id)),
                                                            });
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-yellow-500 hover:text-white transition-colors"
                                                    >
                                                        <Eraser className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.cleanup')}
                                                    </button>
                                                    <button
                                                        onClick={(e) => {
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            setOpenMenuId(null);
                                                            openConfirmDialog({
                                                                title: t('layout.libraryActions.confirmAiGroupingTitle'),
                                                                description: t('layout.libraryActions.confirmAiGroupingDescription'),
                                                                confirmLabel: t('layout.libraryActions.startCompute'),
                                                                tone: 'warning',
                                                                onConfirm: () => handleAIGrouping(String(lib.id)),
                                                            });
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-komgaPrimary hover:bg-komgaPrimary hover:text-white transition-colors"
                                                    >
                                                        <Sparkles className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.aiGrouping')}
                                                    </button>
                                                    <div className="border-t border-gray-700"></div>
                                                    <button
                                                        onClick={(e) => {
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            setOpenMenuId(null);
                                                            openConfirmDialog({
                                                                title: t('layout.libraryActions.confirmDeleteTitle'),
                                                                description: t('layout.libraryActions.confirmDeleteDescription', { name: lib.name }),
                                                                confirmLabel: t('layout.libraryActions.confirmDelete'),
                                                                tone: 'danger',
                                                                onConfirm: () => handleDeleteLibrary(lib),
                                                            });
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-red-500 hover:text-white transition-colors"
                                                    >
                                                        <Trash2 className="w-4 h-4 mr-2" />
                                                        {t('layout.libraryActions.delete')}
                                                    </button>
                                                </div>
                                            </>,
                                            document.body
                                        )}
                                    </div>
                                    )}
                                </Link>
                            ));
                        })()}
                            </nav>
                            )}
                        </div>
                    </div>
                </aside>

                <div className="flex-1 overflow-y-auto bg-komgaDark relative h-[calc(100vh-73px)]">
                    <Outlet context={{ refreshTrigger, libraries }} />
                </div>

                <SidebarTaskBubble
                    tasks={Object.values(taskBubbleEntries)}
                    onDismiss={(key) => {
                        setTaskBubbleEntries((prev) => {
                            if (!prev[key]) return prev;
                            const next = { ...prev };
                            delete next[key];
                            return next;
                        });
                        const timer = taskBubbleCleanupTimers.current.get(key);
                        if (timer) {
                            clearTimeout(timer);
                            taskBubbleCleanupTimers.current.delete(key);
                        }
                    }}
                    onClearFinished={() => {
                        setTaskBubbleEntries((prev) => {
                            const next: Record<string, TaskBubbleEntry> = {};
                            for (const [key, entry] of Object.entries(prev)) {
                                if (entry.status !== 'completed' && entry.status !== 'failed' && entry.status !== 'canceled') {
                                    next[key] = entry;
                                } else {
                                    const timer = taskBubbleCleanupTimers.current.get(key);
                                    if (timer) {
                                        clearTimeout(timer);
                                        taskBubbleCleanupTimers.current.delete(key);
                                    }
                                }
                            }
                            return next;
                        });
                    }}
                />
            </main>

            <ShortcutsPanel open={shortcutsOpen} onClose={() => setShortcutsOpen(false)} />

            {(showAddModal || showEditModal || isSearchModalOpen) ? (
                <Suspense fallback={modalFallback}>
                    <LibraryFormModal
                        title={t('layout.libraryModal.addTitle')}
                        submitLabel={t('layout.libraryModal.addSubmit')}
                        submittingLabel={t('layout.libraryModal.addSubmitting')}
                        open={showAddModal}
                        name={newLibName}
                        path={newLibPath}
                        scanMode={newLibScanMode}
                        koreaderSyncEnabled={newLibKOReaderSyncEnabled}
                        scanInterval={newLibScanInterval}
                        scanFormats={newLibScanFormats}
                        submitting={adding}
                        browsing={browsing}
                        browseCurrent={browseCurrent}
                        browseParent={browseParent}
                        browseDirs={browseDirs}
                        browseDrives={browseDrives}
                        recentPaths={recentLibraryPaths}
                        supportedScanFormats={supportedScanFormats}
                        onClose={() => setShowAddModal(false)}
                        onSubmit={handleAddLibrary}
                        onNameChange={setNewLibName}
                        onPathChange={setNewLibPath}
                        onScanModeChange={setNewLibScanMode}
                        onKOReaderSyncEnabledChange={setNewLibKOReaderSyncEnabled}
                        onScanIntervalChange={setNewLibScanInterval}
                        onScanFormatsChange={setNewLibScanFormats}
                        onOpenDirectoryBrowser={openDirectoryBrowser}
                        onCloseDirectoryBrowser={() => setBrowsing(false)}
                        onChooseCurrentDirectory={() => {
                            setNewLibPath(browseCurrent);
                            setBrowsing(false);
                        }}
                        onNavigateDirectory={navigateDirectoryBrowser}
                    />

                    <LibraryFormModal
                        title={t('layout.libraryModal.editTitle')}
                        submitLabel={t('layout.libraryModal.editSubmit')}
                        submittingLabel={t('layout.libraryModal.editSubmitting')}
                        open={showEditModal}
                        name={editLibName}
                        path={editLibPath}
                        scanMode={editLibScanMode}
                        koreaderSyncEnabled={editLibKOReaderSyncEnabled}
                        scanInterval={editLibScanInterval}
                        scanFormats={editLibScanFormats}
                        submitting={editing}
                        browsing={browsing}
                        browseCurrent={browseCurrent}
                        browseParent={browseParent}
                        browseDirs={browseDirs}
                        browseDrives={browseDrives}
                        recentPaths={recentLibraryPaths}
                        supportedScanFormats={supportedScanFormats}
                        onClose={() => setShowEditModal(false)}
                        onSubmit={handleEditLibrarySubmit}
                        onNameChange={setEditLibName}
                        onPathChange={setEditLibPath}
                        onScanModeChange={setEditLibScanMode}
                        onKOReaderSyncEnabledChange={setEditLibKOReaderSyncEnabled}
                        onScanIntervalChange={setEditLibScanInterval}
                        onScanFormatsChange={setEditLibScanFormats}
                        onOpenDirectoryBrowser={openDirectoryBrowser}
                        onCloseDirectoryBrowser={() => setBrowsing(false)}
                        onChooseCurrentDirectory={() => {
                            setEditLibPath(browseCurrent);
                            setBrowsing(false);
                        }}
                        onNavigateDirectory={navigateDirectoryBrowser}
                    />

                    <SearchModal
                        open={isSearchModalOpen}
                        searchQuery={searchQuery}
                        searchResults={searchResults}
                        selectedIndex={selectedIndex}
                        searchTarget={searchTarget}
                        onClose={() => setIsSearchModalOpen(false)}
                        onSearchQueryChange={setSearchQuery}
                        onSearchKeyDown={handleSearchKeyDown}
                        onResetSearch={resetSearch}
                        onSearchTargetChange={setSearchTarget}
                        onSelectResult={handleSelectResult}
                        onHighlightIndex={setSelectedIndex}
                    />
                </Suspense>
            ) : null}

            <ConfirmDialog
                open={confirmDialog.open}
                onClose={() => {
                    if (confirmLoading) return;
                    setConfirmDialog((prev) => ({ ...prev, open: false, onConfirm: null }));
                }}
                onConfirm={async () => {
                    if (!confirmDialog.onConfirm) return;
                    setConfirmLoading(true);
                    try {
                        await confirmDialog.onConfirm();
                        setConfirmDialog((prev) => ({ ...prev, open: false, onConfirm: null }));
                    } finally {
                        setConfirmLoading(false);
                    }
                }}
                title={confirmDialog.title}
                description={confirmDialog.description}
                confirmLabel={confirmDialog.confirmLabel}
                tone={confirmDialog.tone}
                loading={confirmLoading}
            />
        </div>
    );
}

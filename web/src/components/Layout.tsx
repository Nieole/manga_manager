import { Outlet, Link, useParams, useNavigate, useLocation } from 'react-router-dom';
import { useState, useEffect, useRef } from 'react';
import axios from 'axios';
import { BookOpen, FolderOpen, Plus, X, Loader2, RefreshCw, Search, Trash2, Settings as SettingsIcon, Menu, LayoutDashboard, FolderHeart, Terminal, Download, Eraser, MoreHorizontal, Sparkles } from 'lucide-react';
import { DEFAULT_SCAN_FORMATS, DEFAULT_SCAN_INTERVAL } from './layout/constants';
import { LibraryFormModal } from './layout/LibraryFormModal';
import { SearchModal } from './layout/SearchModal';
import type { BrowseDirEntry, BrowseDrive, Library, SearchHit } from './layout/types';
import { useGlobalSearch } from './layout/useGlobalSearch';

export default function Layout() {
    const [recentLibraryPaths, setRecentLibraryPaths] = useState<string[]>([]);
    const [supportedScanFormats, setSupportedScanFormats] = useState(DEFAULT_SCAN_FORMATS);
    const [libraries, setLibraries] = useState<Library[]>([]);
    const [loading, setLoading] = useState(true);
    const [showAddModal, setShowAddModal] = useState(false);
    const [isSidebarOpen, setIsSidebarOpen] = useState(false);
    const [openMenuId, setOpenMenuId] = useState<string | null>(null);
    const [newLibName, setNewLibName] = useState("");
    const [newLibPath, setNewLibPath] = useState("");
    const [newLibAutoScan, setNewLibAutoScan] = useState(false);
    const [newLibKOReaderSyncEnabled, setNewLibKOReaderSyncEnabled] = useState(true);
    const [newLibScanInterval, setNewLibScanInterval] = useState(DEFAULT_SCAN_INTERVAL);
    const [newLibScanFormats, setNewLibScanFormats] = useState(DEFAULT_SCAN_FORMATS);
    const [adding, setAdding] = useState(false);

    // 编辑资源库状态
    const [showEditModal, setShowEditModal] = useState(false);
    const [editLibId, setEditLibId] = useState("");
    const [editLibName, setEditLibName] = useState("");
    const [editLibPath, setEditLibPath] = useState("");
    const [editLibAutoScan, setEditLibAutoScan] = useState(false);
    const [editLibKOReaderSyncEnabled, setEditLibKOReaderSyncEnabled] = useState(true);
    const [editLibScanInterval, setEditLibScanInterval] = useState(DEFAULT_SCAN_INTERVAL);
    const [editLibScanFormats, setEditLibScanFormats] = useState(DEFAULT_SCAN_FORMATS);
    const [editing, setEditing] = useState(false);

    // 用于向所有 Outlet 子路由向下传递全局刷新信号的计数器
    const [refreshTrigger, setRefreshTrigger] = useState(0);

    // 全局任务进度状态
    const [taskProgress, setTaskProgress] = useState<{
        status: string;
        message: string;
        error?: string;
        current: number;
        total: number;
        type: string;
    } | null>(null);
    const taskDismissTimer = useRef<number | null>(null);

    // 文件夹浏览器状态
    const [browsing, setBrowsing] = useState(false);
    const [browseDirs, setBrowseDirs] = useState<BrowseDirEntry[]>([]);
    const [browseCurrent, setBrowseCurrent] = useState('');
    const [browseParent, setBrowseParent] = useState('');
    const [browseDrives, setBrowseDrives] = useState<BrowseDrive[]>([]);

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

    const openEditLibraryModal = (libraryId: string | number) => {
        const target = libraries.find((lib) => String(lib.id) === String(libraryId));
        if (!target) return;
        setEditLibId(String(target.id));
        setEditLibName(target.name || "");
        setEditLibPath(target.path || "");
        setEditLibAutoScan(target.auto_scan || false);
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
        axios.get('/api/browse-dirs')
            .then(res => {
                setBrowseDirs(res.data.dirs || []);
                setBrowseCurrent(res.data.current);
                setBrowseParent(res.data.parent);
                setBrowseDrives(res.data.drives || []);
            })
            .catch(() => { });
    };

    const navigateDirectoryBrowser = (path: string) => {
        axios.get(`/api/browse-dirs?path=${encodeURIComponent(path)}`)
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

    const fetchLibraries = () => {
        setLoading(true);
        axios.get('/api/libraries')
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
        axios.get('/api/system/capabilities')
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

        // 挂载 Server-Sent Events 流监听器
        const eventSource = new EventSource('/api/events');

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
                    setTaskProgress(progress);
                    // 清除之前的自动关闭计时器
                    if (taskDismissTimer.current) clearTimeout(taskDismissTimer.current);
                    // 如果任务完成（current >= total），3 秒后自动隐藏
                    if ((progress.status === 'completed' || progress.status === 'failed') && progress.total > 0) {
                        taskDismissTimer.current = window.setTimeout(() => setTaskProgress(null), 3000);
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
            eventSource.close();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const handleAddLibrary = async (e: React.FormEvent) => {
        e.preventDefault();
        setAdding(true);
        try {
            await axios.post('/api/libraries', {
                name: newLibName,
                path: newLibPath,
                auto_scan: newLibAutoScan,
                koreader_sync_enabled: newLibKOReaderSyncEnabled,
                scan_interval: newLibScanInterval,
                scan_formats: newLibScanFormats
            });
            setShowAddModal(false);
            setNewLibName("");
            setNewLibPath("");
            setNewLibAutoScan(false);
            setNewLibKOReaderSyncEnabled(true);
            setNewLibScanInterval(DEFAULT_SCAN_INTERVAL);
            setNewLibScanFormats(DEFAULT_SCAN_FORMATS);
            saveRecentLibraryPath(newLibPath);
            fetchLibraries();
            setRefreshTrigger(prev => prev + 1);
        } catch (error) {
            console.error(error);
            alert(extractErrorMessage(error, "添加资源库失败，请检查目录权限和扫描格式。"));
        } finally {
            setAdding(false);
        }
    };

    const handleEditLibrarySubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        setEditing(true);
        try {
            await axios.put(`/api/libraries/${editLibId}`, {
                name: editLibName,
                path: editLibPath,
                auto_scan: editLibAutoScan,
                koreader_sync_enabled: editLibKOReaderSyncEnabled,
                scan_interval: editLibScanInterval,
                scan_formats: editLibScanFormats
            });
            setShowEditModal(false);
            saveRecentLibraryPath(editLibPath);
            fetchLibraries();
        } catch (error) {
            console.error(error);
            alert(extractErrorMessage(error, "修改资源库失败，请检查目录权限和扫描格式。"));
        } finally {
            setEditing(false);
        }
    };

    const handleScanLibrary = async (e: React.MouseEvent, id: string, force: boolean = false) => {
        e.preventDefault();
        e.stopPropagation();
        try {
            await axios.post(`/api/libraries/${id}/scan?force=${force}`);
            // 不必手动刷新界面，后端的 SSE 会通过 onmessage 广播数据到达
        } catch (error) {
            console.error("Trigger scan failed", error);
            alert("扫描指令下发失败");
        }
    };

    const handleScrapeLibrary = async (e: React.MouseEvent, id: string) => {
        e.preventDefault();
        e.stopPropagation();
        if (confirm("是否启动后台任务，批量刮削此资源库中所有缺失元数据的系列？")) {
            try {
                await axios.post(`/api/libraries/${id}/scrape`, { provider: 'bangumi' });
                alert("刮削任务已提交，请留意界面底部的进度提示。");
            } catch (error) {
                console.error("Trigger scrape failed", error);
                alert("刮削指令下发失败");
            }
        }
    };

    const handleCleanupLibrary = async (e: React.MouseEvent, id: string) => {
        e.preventDefault();
        e.stopPropagation();
        if (confirm("确定要清理此资源库中已失效的记录吗？\n（物理文件已不在的记录将被删除）")) {
            try {
                await axios.post(`/api/libraries/${id}/cleanup`);
                alert("清理任务已提交后台处理。");
            } catch (error) {
                console.error("Trigger cleanup failed", error);
                alert("清理指令下发失败");
            }
        }
    };

    const handleAIGrouping = async (e: React.MouseEvent, id: string) => {
        e.preventDefault();
        e.stopPropagation();
        if (confirm("这可能会花费一些时间调用 AI 进行自动分类计算。是否确认执行？")) {
            try {
                await axios.post(`/api/libraries/${id}/ai-grouping`);
                alert("AI 智能分组任务已提交后台计算。");
            } catch (error) {
                console.error("Trigger AI grouping failed", error);
                alert("提交 AI 智能分组请求失败");
            }
        }
    };

    return (
        <div className="min-h-screen bg-komgaDark text-gray-200 font-sans flex flex-col relative">
            <header className="bg-komgaSurface shadow-md sticky top-0 z-20 px-4 sm:px-6 py-4 flex items-center justify-between border-b border-gray-800">
                <div className="flex items-center">
                    <button
                        onClick={() => setIsSidebarOpen(true)}
                        className="md:hidden p-2 -ml-2 mr-2 text-gray-400 hover:text-white transition-colors"
                        title="打开菜单"
                    >
                        <Menu className="w-6 h-6" />
                    </button>
                    <Link to="/" className="flex items-center space-x-2 sm:space-x-3 w-auto sm:w-56">
                        <BookOpen className="text-komgaPrimary h-7 w-7 sm:h-8 sm:w-8" />
                        <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-white hover:text-komgaPrimary transition hidden sm:block">Manga Manager</h1>
                    </Link>
                </div>

                <div className="flex-1 max-w-xl flex justify-center px-4">
                    <button
                        onClick={() => setIsSearchModalOpen(true)}
                        className="w-full max-w-md bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 flex items-center justify-between text-sm text-gray-500 hover:border-gray-700 hover:text-gray-300 transition-all opacity-80 hover:opacity-100 shadow-inner group"
                    >
                        <div className="flex items-center">
                            <Search className="w-4 h-4 mr-3 group-hover:text-komgaPrimary transition-colors" />
                            <span>搜索漫画名称、连载系列...</span>
                        </div>
                        <kbd className="hidden sm:inline-block bg-gray-800 border border-gray-700 rounded px-2 py-0.5 text-xs font-mono text-gray-400">⌘K</kbd>
                    </button>
                </div>

                <div className="w-auto sm:w-64 flex justify-end">
                    <Link
                        to="/settings"
                        className="p-2 text-gray-400 hover:text-komgaPrimary hover:bg-gray-800 rounded-full transition-colors"
                        title="系统设定"
                    >
                        <SettingsIcon className="w-6 h-6" />
                    </Link>
                </div>
            </header>

            <main className="flex-1 flex overflow-hidden relative">
                {/* 移动端侧边栏半透明深色遮罩 */}
                {isSidebarOpen && (
                    <div
                        className="fixed inset-0 bg-black/70 z-40 md:hidden backdrop-blur-sm transition-opacity"
                        onClick={() => setIsSidebarOpen(false)}
                    />
                )}

                <aside className={`fixed inset-y-0 left-0 top-[73px] z-50 w-64 bg-komgaSurface border-r border-gray-800 flex flex-col pt-6 transform transition-transform duration-300 ease-in-out md:relative md:top-0 md:translate-x-0 overflow-y-auto ${isSidebarOpen ? 'translate-x-0' : '-translate-x-full'} md:h-[calc(100vh-73px)]`}>
                    <div className="px-6 mb-4 flex items-center justify-between text-xs font-semibold text-gray-400 uppercase tracking-wider shrink-0">
                        <span>Libraries</span>
                        <button
                            onClick={() => setShowAddModal(true)}
                            className="text-gray-400 hover:text-white transition-colors"
                            title="添加新资源库"
                        >
                            <Plus className="w-4 h-4" />
                        </button>
                    </div>
                    {/* 快捷导航 */}
                    <nav className="px-4 mb-4 space-y-1" onClick={() => setOpenMenuId(null)}>
                        <Link to="/" onClick={() => setIsSidebarOpen(false)}
                            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 ${location.pathname === '/' ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium' : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                                }`}
                        >
                            <LayoutDashboard className="w-5 h-5 shrink-0" />
                            <span>仪表板</span>
                        </Link>
                        <Link to="/collections" onClick={() => setIsSidebarOpen(false)}
                            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 ${location.pathname === '/collections' ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium' : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                                }`}
                        >
                            <FolderHeart className="w-5 h-5 shrink-0" />
                            <span>合集</span>
                        </Link>
                        <Link to="/logs" onClick={() => setIsSidebarOpen(false)}
                            className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors duration-200 ${location.pathname === '/logs' ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium' : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                                }`}
                        >
                            <Terminal className="w-5 h-5 shrink-0" />
                            <span>系统日志</span>
                        </Link>
                    </nav>
                    <div className="px-6 mb-2 text-[10px] font-semibold text-gray-600 uppercase tracking-wider">Libraries</div>
                    <nav className="flex-1 space-y-1 px-4 overflow-y-auto">
                        {loading ? (
                            <div className="animate-pulse px-3 py-2 bg-gray-800 rounded-md h-10 w-full mb-2" />
                        ) : libraries.length === 0 ? (
                            <div className="text-gray-500 px-3 text-sm">No libraries found.</div>
                        ) : (
                            libraries.map(lib => (
                                <Link
                                    key={lib.id}
                                    to={`/library/${lib.id}`}
                                    onClick={() => setIsSidebarOpen(false)}
                                    className={`w-full flex justify-between items-center group px-3 py-2.5 rounded-lg transition-colors duration-200 ${libId === lib.id
                                        ? 'bg-komgaPrimary/10 text-komgaPrimary font-medium'
                                        : 'text-gray-300 hover:bg-gray-800 hover:text-white'
                                        }`}
                                >
                                    <div className="flex items-center space-x-3 overflow-hidden min-w-0">
                                        <FolderOpen className="w-5 h-5 flex-shrink-0" />
                                        <div className="min-w-0">
                                            <span className="truncate block">{lib.name}</span>
                                            <span className={`text-[10px] ${lib.koreader_sync_enabled ?? true ? 'text-sky-400/80' : 'text-gray-500'}`}>
                                                {lib.koreader_sync_enabled ?? true ? 'KOReader Sync 开启' : 'KOReader Sync 关闭'}
                                            </span>
                                        </div>
                                    </div>
                                    <div className="relative">
                                        <button
                                            onClick={(e) => {
                                                e.preventDefault();
                                                e.stopPropagation();
                                                setOpenMenuId(openMenuId === lib.id ? null : lib.id);
                                            }}
                                            className="text-gray-500 hover:text-white opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded hover:bg-gray-700 focus:outline-none"
                                            title="更多操作"
                                        >
                                            <MoreHorizontal className="w-5 h-5" />
                                        </button>

                                        {openMenuId === lib.id && (
                                            <>
                                                <div
                                                    className="fixed inset-0 z-40"
                                                    onClick={(e) => {
                                                        e.preventDefault();
                                                        e.stopPropagation();
                                                        setOpenMenuId(null);
                                                    }}
                                                />
                                                <div className="absolute right-0 mt-2 w-48 bg-gray-800 border border-gray-700 rounded-lg shadow-xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200">
                                                    <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-gray-700 bg-gray-900">
                                                        资源库操作
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
                                                        编辑资源库
                                                    </button>
                                                    <button
                                                        onClick={(e) => { setOpenMenuId(null); handleScanLibrary(e, String(lib.id), false); }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors"
                                                    >
                                                        <RefreshCw className="w-4 h-4 mr-2" />
                                                        重新扫描增量
                                                    </button>
                                                    <button
                                                        onClick={(e) => {
                                                            setOpenMenuId(null);
                                                            if (confirm("强制重新扫描将会耗费更长时间并全量读取覆盖所有元数据。是否继续？")) {
                                                                handleScanLibrary(e, String(lib.id), true);
                                                            } else {
                                                                e.preventDefault();
                                                                e.stopPropagation();
                                                            }
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-orange-500 hover:text-white transition-colors"
                                                    >
                                                        <RefreshCw className="w-4 h-4 mr-2" />
                                                        强制全量读取
                                                    </button>
                                                    <button
                                                        onClick={(e) => { setOpenMenuId(null); handleScrapeLibrary(e, String(lib.id)); }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-green-500 hover:text-white transition-colors"
                                                    >
                                                        <Download className="w-4 h-4 mr-2" />
                                                        刮削缺失元数据
                                                    </button>
                                                    <button
                                                        onClick={(e) => { setOpenMenuId(null); handleCleanupLibrary(e, String(lib.id)); }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-yellow-500 hover:text-white transition-colors"
                                                    >
                                                        <Eraser className="w-4 h-4 mr-2" />
                                                        清理失效资源
                                                    </button>
                                                    <button
                                                        onClick={(e) => { setOpenMenuId(null); handleAIGrouping(e, String(lib.id)); }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-komgaPrimary hover:bg-komgaPrimary hover:text-white transition-colors"
                                                    >
                                                        <Sparkles className="w-4 h-4 mr-2" />
                                                        AI 智能分组
                                                    </button>
                                                    <div className="border-t border-gray-700"></div>
                                                    <button
                                                        onClick={(e) => {
                                                            setOpenMenuId(null);
                                                            e.preventDefault();
                                                            e.stopPropagation();
                                                            if (confirm(`确定要删除资源库「${lib.name}」吗？\n所有关联的系列、书籍和阅读记录都将被清除。`)) {
                                                                axios.delete(`/api/libraries/${lib.id}`)
                                                                    .then(() => { fetchLibraries(); navigate('/'); })
                                                                    .catch(() => alert('删除失败'));
                                                            }
                                                        }}
                                                        className="w-full flex items-center px-4 py-2 text-sm text-gray-200 hover:bg-red-500 hover:text-white transition-colors"
                                                    >
                                                        <Trash2 className="w-4 h-4 mr-2" />
                                                        删除此资源库
                                                    </button>
                                                </div>
                                            </>
                                        )}
                                    </div>
                                </Link>
                            ))
                        )}
                    </nav>
                </aside>

                <div className="flex-1 overflow-y-auto bg-komgaDark relative h-[calc(100vh-73px)]">
                    <Outlet context={{ refreshTrigger, libraries }} />
                </div>

                {/* 全局任务进度浮动条 */}
                {taskProgress && (
                    <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 w-[90vw] max-w-lg animate-in slide-in-from-bottom-4 fade-in duration-300">
                        <div className="bg-gray-900/95 border border-gray-700 rounded-2xl px-5 py-4 shadow-2xl backdrop-blur">
                            <div className="flex items-center justify-between mb-2">
                                <div className="flex items-center gap-2">
                                    {taskProgress.status === 'failed' ? (
                                        <span className="text-red-400 text-sm">!</span>
                                    ) : taskProgress.current < taskProgress.total ? (
                                        <Loader2 className="w-4 h-4 text-komgaPrimary animate-spin" />
                                    ) : (
                                        <span className="text-green-400 text-sm">✓</span>
                                    )}
                                    <span className="text-sm text-gray-200 font-medium truncate max-w-[280px]">
                                        {taskProgress.message}
                                    </span>
                                </div>
                                <div className="flex items-center gap-3">
                                    <span className="text-xs text-gray-400 font-mono whitespace-nowrap">
                                        {taskProgress.current}/{taskProgress.total}
                                    </span>
                                    <button onClick={() => setTaskProgress(null)} className="text-gray-500 hover:text-white transition-colors">
                                        <X className="w-3.5 h-3.5" />
                                    </button>
                                </div>
                            </div>
                            {taskProgress.error && (
                                <div className="mb-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-200">
                                    {taskProgress.error}
                                </div>
                            )}
                            {taskProgress.total > 0 && (
                                <div className="w-full h-1.5 bg-gray-800 rounded-full overflow-hidden">
                                    <div
                                        className={`h-full rounded-full transition-all duration-500 ease-out ${taskProgress.status === 'failed' ? 'bg-red-500' : taskProgress.current >= taskProgress.total ? 'bg-green-500' : 'bg-komgaPrimary'
                                            }`}
                                        style={{ width: `${Math.min(100, (taskProgress.current / taskProgress.total) * 100)}%` }}
                                    />
                                </div>
                            )}
                        </div>
                    </div>
                )}
            </main>

            <LibraryFormModal
                title="添加资源库"
                submitLabel="立即添加"
                submittingLabel="扫描入库中..."
                open={showAddModal}
                name={newLibName}
                path={newLibPath}
                autoScan={newLibAutoScan}
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
                onAutoScanChange={setNewLibAutoScan}
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
                title="编辑资源库"
                submitLabel="保存修改"
                submittingLabel="保存中..."
                open={showEditModal}
                name={editLibName}
                path={editLibPath}
                autoScan={editLibAutoScan}
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
                onAutoScanChange={setEditLibAutoScan}
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
        </div>
    );
}

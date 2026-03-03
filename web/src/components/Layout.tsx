import { Outlet, Link, useParams, useNavigate, useLocation } from 'react-router-dom';
import { useState, useEffect, useRef } from 'react';
import axios from 'axios';
import { BookOpen, FolderOpen, Plus, X, Loader2, RefreshCw, Search, Trash2, Settings as SettingsIcon, Menu, ImageIcon, LayoutDashboard, FolderHeart } from 'lucide-react';

interface Library {
    id: string;
    name: string;
    path: string;
}

export default function Layout() {
    const [libraries, setLibraries] = useState<Library[]>([]);
    const [loading, setLoading] = useState(true);
    const [showAddModal, setShowAddModal] = useState(false);
    const [isSidebarOpen, setIsSidebarOpen] = useState(false);
    const [newLibName, setNewLibName] = useState("");
    const [newLibPath, setNewLibPath] = useState("");
    const [newLibAutoScan, setNewLibAutoScan] = useState(false);
    const [newLibScanInterval, setNewLibScanInterval] = useState(60);
    const [newLibScanFormats, setNewLibScanFormats] = useState("zip,cbz,rar,cbr,pdf");
    const [adding, setAdding] = useState(false);
    // 用于向所有 Outlet 子路由向下传递全局刷新信号的计数器
    const [refreshTrigger, setRefreshTrigger] = useState(0);

    // 全局任务进度状态
    const [taskProgress, setTaskProgress] = useState<{
        message: string;
        current: number;
        total: number;
        type: string;
    } | null>(null);
    const taskDismissTimer = useRef<number | null>(null);

    // 文件夹浏览器状态
    const [browsing, setBrowsing] = useState(false);
    const [browseDirs, setBrowseDirs] = useState<any[]>([]);
    const [browseCurrent, setBrowseCurrent] = useState('');
    const [browseParent, setBrowseParent] = useState('');
    const [browseDrives, setBrowseDrives] = useState<any[]>([]);

    const [searchQuery, setSearchQuery] = useState("");
    const [searchResults, setSearchResults] = useState<any[]>([]);
    const [isSearchModalOpen, setIsSearchModalOpen] = useState(false);
    const [selectedIndex, setSelectedIndex] = useState(0);
    const [searchTarget, setSearchTarget] = useState("all");

    const { libId } = useParams();
    const navigate = useNavigate();
    const location = useLocation();

    useEffect(() => {
        if (!searchQuery.trim()) {
            setSearchResults([]);
            return;
        }
        const timer = setTimeout(() => {
            axios.get(`/api/search?q=${encodeURIComponent(searchQuery)}&target=${searchTarget}`)
                .then(res => {
                    if (res.data && res.data.hits) {
                        setSearchResults(res.data.hits);
                    } else {
                        setSearchResults([]);
                    }
                })
                .catch(err => console.error("Search failed:", err));
        }, 300); // 防抖 300ms
        return () => clearTimeout(timer);
    }, [searchQuery, searchTarget]);

    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
                e.preventDefault();
                setIsSearchModalOpen(true);
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => window.removeEventListener('keydown', handleKeyDown);
    }, []);

    useEffect(() => {
        setSelectedIndex(0);
    }, [searchResults]);

    const handleSelectResult = (hit: any) => {
        setIsSearchModalOpen(false);
        setSearchQuery("");
        setSearchResults([]);

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

        // 挂载 Server-Sent Events 流监听器
        const eventSource = new EventSource('/api/events');

        eventSource.onmessage = (event) => {
            const data = event.data as string;
            if (data === "refresh") {
                console.log("Receive SSE refresh signal, reloading libraries...");
                fetchLibraries();
                // 收到后端推送后自增刷新信号以便子组件重新拉取元数据
                setRefreshTrigger(prev => prev + 1);
            } else if (data.startsWith('task_progress:')) {
                try {
                    const progress = JSON.parse(data.slice('task_progress:'.length));
                    setTaskProgress(progress);
                    // 清除之前的自动关闭计时器
                    if (taskDismissTimer.current) clearTimeout(taskDismissTimer.current);
                    // 如果任务完成（current >= total），3 秒后自动隐藏
                    if (progress.current >= progress.total && progress.total > 0) {
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
            eventSource.close();
        };
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

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

    const handleAddLibrary = async (e: React.FormEvent) => {
        e.preventDefault();
        setAdding(true);
        try {
            await axios.post('/api/libraries', {
                name: newLibName,
                path: newLibPath,
                auto_scan: newLibAutoScan,
                scan_interval: newLibScanInterval,
                scan_formats: newLibScanFormats
            });
            setShowAddModal(false);
            setNewLibName("");
            setNewLibPath("");
            setNewLibAutoScan(false);
            setNewLibScanInterval(60);
            setNewLibScanFormats("zip,cbz,rar,cbr,pdf");
            // 由于有 SSE 监听，这里甚至可以不需要主动 fetch，但为了增强即时感先拉一下基本信息
            fetchLibraries();
            setRefreshTrigger(prev => prev + 1);
        } catch (error) {
            console.error(error);
            alert("添加库失败，请检查路径是否正确及服务端状态");
        } finally {
            setAdding(false);
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

                <aside className={`fixed inset-y-0 left-0 top-[73px] z-50 w-64 bg-komgaSurface border-r border-gray-800 flex flex-col pt-6 transform transition-transform duration-300 ease-in-out md:relative md:top-0 md:translate-x-0 overflow-hidden ${isSidebarOpen ? 'translate-x-0' : '-translate-x-full'}`}>
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
                    <nav className="px-4 mb-4 space-y-1">
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
                                    <div className="flex items-center space-x-3 overflow-hidden">
                                        <FolderOpen className="w-5 h-5 flex-shrink-0" />
                                        <span className="truncate">{lib.name}</span>
                                    </div>
                                    <button
                                        onClick={(e) => handleScanLibrary(e, String(lib.id), false)}
                                        className="text-gray-500 hover:text-komgaPrimary opacity-0 group-hover:opacity-100 transition-opacity p-1"
                                        title="重新扫描库内的变动"
                                    >
                                        <RefreshCw className="w-4 h-4" />
                                    </button>
                                    <button
                                        onClick={(e) => {
                                            if (confirm("强制重新扫描将会耗费更长时间并全量读取覆盖所有元数据。是否继续？")) {
                                                handleScanLibrary(e, String(lib.id), true);
                                            } else {
                                                e.preventDefault();
                                                e.stopPropagation();
                                            }
                                        }}
                                        className="text-gray-500 hover:text-orange-400 opacity-0 group-hover:opacity-100 transition-opacity p-1"
                                        title="强制全量读取"
                                    >
                                        <RefreshCw className="w-4 h-4" />
                                    </button>
                                    <button
                                        onClick={(e) => {
                                            e.preventDefault();
                                            e.stopPropagation();
                                            if (confirm(`确定要删除资源库「${lib.name}」吗？\n所有关联的系列、书籍和阅读记录都将被清除。`)) {
                                                axios.delete(`/api/libraries/${lib.id}`)
                                                    .then(() => { fetchLibraries(); navigate('/'); })
                                                    .catch(() => alert('删除失败'));
                                            }
                                        }}
                                        className="text-gray-500 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-opacity p-1"
                                        title="删除此资源库"
                                    >
                                        <Trash2 className="w-4 h-4" />
                                    </button>
                                </Link>
                            ))
                        )}
                    </nav>
                </aside>

                <div className="flex-1 overflow-y-auto bg-komgaDark relative h-full">
                    <Outlet context={{ refreshTrigger }} />
                </div>

                {/* 全局任务进度浮动条 */}
                {taskProgress && (
                    <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 w-[90vw] max-w-lg animate-in slide-in-from-bottom-4 fade-in duration-300">
                        <div className="bg-gray-900/95 border border-gray-700 rounded-2xl px-5 py-4 shadow-2xl backdrop-blur">
                            <div className="flex items-center justify-between mb-2">
                                <div className="flex items-center gap-2">
                                    {taskProgress.current < taskProgress.total ? (
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
                            {taskProgress.total > 0 && (
                                <div className="w-full h-1.5 bg-gray-800 rounded-full overflow-hidden">
                                    <div
                                        className={`h-full rounded-full transition-all duration-500 ease-out ${taskProgress.current >= taskProgress.total ? 'bg-green-500' : 'bg-komgaPrimary'
                                            }`}
                                        style={{ width: `${Math.min(100, (taskProgress.current / taskProgress.total) * 100)}%` }}
                                    />
                                </div>
                            )}
                        </div>
                    </div>
                )}
            </main>

            {/* 新增库模态框 */}
            {showAddModal && (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
                    <div className="bg-komgaSurface rounded-xl shadow-2xl border border-gray-800 w-full max-w-md overflow-hidden animate-in fade-in zoom-in duration-200">
                        <div className="flex justify-between items-center p-6 border-b border-gray-800">
                            <h3 className="text-xl font-semibold text-white">添加资源库</h3>
                            <button onClick={() => setShowAddModal(false)} className="text-gray-400 hover:text-white transition-colors">
                                <X className="w-5 h-5" />
                            </button>
                        </div>
                        <form onSubmit={handleAddLibrary} className="p-6">
                            <div className="space-y-4">
                                <div>
                                    <label className="block text-sm font-medium text-gray-400 mb-1">名称</label>
                                    <input
                                        type="text"
                                        required
                                        value={newLibName}
                                        onChange={e => setNewLibName(e.target.value)}
                                        placeholder="例如: 日漫收藏"
                                        className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary focus:border-transparent transition-all"
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium text-gray-400 mb-1">路径</label>
                                    <div className="flex gap-2">
                                        <input
                                            type="text"
                                            required
                                            value={newLibPath}
                                            onChange={e => setNewLibPath(e.target.value)}
                                            placeholder="点击「浏览」选择文件夹"
                                            className="flex-1 bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary focus:border-transparent transition-all"
                                        />
                                        <button
                                            type="button"
                                            onClick={() => {
                                                setBrowsing(true);
                                                axios.get('/api/browse-dirs')
                                                    .then(res => { setBrowseDirs(res.data.dirs || []); setBrowseCurrent(res.data.current); setBrowseParent(res.data.parent); setBrowseDrives(res.data.drives || []); })
                                                    .catch(() => { });
                                            }}
                                            className="px-4 py-2.5 bg-gray-800 hover:bg-gray-700 text-white text-sm rounded-lg border border-gray-700 transition-colors whitespace-nowrap"
                                        >
                                            <FolderOpen className="w-4 h-4 inline mr-1" />浏览
                                        </button>
                                    </div>
                                    {browsing && (
                                        <div className="mt-3 bg-gray-900 rounded-lg border border-gray-700 overflow-hidden">
                                            <div className="px-3 py-2 bg-gray-800 flex items-center justify-between text-xs">
                                                <span className="text-gray-400 truncate flex-1 mr-2" title={browseCurrent}>{browseCurrent}</span>
                                                <div className="flex gap-1">
                                                    <button type="button" onClick={() => {
                                                        setNewLibPath(browseCurrent);
                                                        setBrowsing(false);
                                                    }} className="px-2 py-1 bg-komgaPrimary hover:bg-purple-600 text-white rounded text-xs transition-colors">选择此目录</button>
                                                    <button type="button" onClick={() => setBrowsing(false)} className="px-2 py-1 text-gray-400 hover:text-white transition-colors">关闭</button>
                                                </div>
                                            </div>
                                            <div className="max-h-48 overflow-y-auto">
                                                {browseDrives.length > 0 && (
                                                    <div className="px-3 py-2 flex flex-wrap gap-1 border-b border-gray-700">
                                                        {browseDrives.map((drv: any) => (
                                                            <button key={drv.path} type="button" onClick={() => {
                                                                axios.get(`/api/browse-dirs?path=${encodeURIComponent(drv.path)}`)
                                                                    .then(res => { setBrowseDirs(res.data.dirs || []); setBrowseCurrent(res.data.current); setBrowseParent(res.data.parent); setBrowseDrives(res.data.drives || []); });
                                                            }}
                                                                className={`px-2 py-1 text-xs rounded transition-colors ${browseCurrent.startsWith(drv.path) || browseCurrent.startsWith(drv.name)
                                                                    ? 'bg-komgaPrimary text-white'
                                                                    : 'bg-gray-800 text-gray-400 hover:bg-gray-700 hover:text-white'
                                                                    }`}
                                                            >{drv.name}</button>
                                                        ))}
                                                    </div>
                                                )}
                                                {browseCurrent !== browseParent && (
                                                    <button type="button" onClick={() => {
                                                        axios.get(`/api/browse-dirs?path=${encodeURIComponent(browseParent)}`)
                                                            .then(res => { setBrowseDirs(res.data.dirs || []); setBrowseCurrent(res.data.current); setBrowseParent(res.data.parent); setBrowseDrives(res.data.drives || []); });
                                                    }} className="w-full text-left px-3 py-2 text-sm text-yellow-400 hover:bg-gray-800 transition-colors flex items-center">
                                                        ↑ ..
                                                    </button>
                                                )}
                                                {browseDirs.length === 0 ? (
                                                    <div className="px-3 py-3 text-xs text-gray-500 text-center">此目录下无子文件夹</div>
                                                ) : browseDirs.map((d: any) => (
                                                    <button key={d.path} type="button" onClick={() => {
                                                        axios.get(`/api/browse-dirs?path=${encodeURIComponent(d.path)}`)
                                                            .then(res => { setBrowseDirs(res.data.dirs || []); setBrowseCurrent(res.data.current); setBrowseParent(res.data.parent); setBrowseDrives(res.data.drives || []); });
                                                    }} className="w-full text-left px-3 py-2 text-sm text-gray-300 hover:bg-gray-800 hover:text-komgaPrimary transition-colors flex items-center">
                                                        <FolderOpen className="w-4 h-4 mr-2 text-komgaPrimary/60" />{d.name}
                                                    </button>
                                                ))}
                                            </div>
                                        </div>
                                    )}
                                </div>
                            </div>

                            <div className="mt-4 p-4 bg-gray-900 rounded-lg border border-gray-800 space-y-4">
                                <label className="flex items-center space-x-3 cursor-pointer">
                                    <input
                                        type="checkbox"
                                        checked={newLibAutoScan}
                                        onChange={e => setNewLibAutoScan(e.target.checked)}
                                        className="form-checkbox h-4 w-4 text-komgaPrimary bg-gray-800 border-gray-700 rounded focus:ring-komgaPrimary focus:ring-2"
                                    />
                                    <span className="text-sm font-medium text-gray-300">开启后台自动轮次扫描监控</span>
                                </label>
                                {newLibAutoScan && (
                                    <>
                                        <div>
                                            <label className="block text-sm font-medium text-gray-400 mb-1">循环触发扫描任务的间隔 (默认60分钟)</label>
                                            <input
                                                type="number"
                                                min="1"
                                                value={newLibScanInterval}
                                                onChange={e => setNewLibScanInterval(parseInt(e.target.value) || 60)}
                                                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-komgaPrimary"
                                            />
                                        </div>
                                        <div>
                                            <label className="block text-sm font-medium text-gray-400 mb-1">目标提取匹配类型 (英文逗号分隔)</label>
                                            <input
                                                type="text"
                                                value={newLibScanFormats}
                                                onChange={e => setNewLibScanFormats(e.target.value)}
                                                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-white text-sm focus:outline-none focus:ring-2 focus:ring-komgaPrimary"
                                            />
                                        </div>
                                    </>
                                )}
                            </div>

                            <div className="mt-8 flex justify-end space-x-3">
                                <button
                                    type="button"
                                    onClick={() => setShowAddModal(false)}
                                    className="px-4 py-2 text-sm font-medium text-gray-400 hover:text-white transition-colors"
                                >
                                    取消
                                </button>
                                <button
                                    type="submit"
                                    disabled={adding}
                                    className="px-6 py-2 bg-komgaPrimary hover:bg-purple-600 text-white text-sm font-medium rounded-lg shadow-lg flex items-center transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    {adding ? (
                                        <>
                                            <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                                            扫描入库中...
                                        </>
                                    ) : (
                                        "立即添加"
                                    )}
                                </button>
                            </div>
                        </form>
                    </div>
                </div>
            )}

            {isSearchModalOpen && (
                <div className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh] px-4">
                    <div
                        className="fixed inset-0 bg-black/60 backdrop-blur-sm"
                        onClick={() => setIsSearchModalOpen(false)}
                    />
                    <div className="relative w-full max-w-2xl bg-komgaSurface border border-gray-800 rounded-xl shadow-2xl flex flex-col max-h-[70vh] animate-in fade-in zoom-in-95 duration-200">
                        <div className="flex items-center px-4 border-b border-gray-800 shrink-0">
                            <Search className="w-5 h-5 text-gray-400" />
                            <input
                                autoFocus
                                type="text"
                                placeholder="输入关键字搜索..."
                                value={searchQuery}
                                onChange={(e) => setSearchQuery(e.target.value)}
                                onKeyDown={handleSearchKeyDown}
                                className="flex-1 bg-transparent border-none py-4 px-4 text-white focus:outline-none focus:ring-0 text-lg placeholder-gray-500"
                            />
                            {searchQuery && (
                                <button onClick={() => { setSearchQuery(""); setSearchResults([]); }} className="p-1 text-gray-500 hover:text-white rounded-md transition-colors">
                                    <X className="w-5 h-5" />
                                </button>
                            )}
                        </div>

                        <div className="flex items-center px-4 py-2 border-b border-gray-800 space-x-2 shrink-0 bg-gray-900/30">
                            <span className="text-xs text-gray-500 mr-2">范围:</span>
                            <button onClick={() => setSearchTarget("all")} className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'all' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}>全部</button>
                            <button onClick={() => setSearchTarget("series")} className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'series' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}>仅系列</button>
                            <button onClick={() => setSearchTarget("book")} className={`px-3 py-1 text-xs font-medium rounded-full transition-colors ${searchTarget === 'book' ? 'bg-komgaPrimary text-white' : 'bg-gray-800 text-gray-400 hover:text-white'}`}>仅册文件</button>
                        </div>

                        <div className="overflow-y-auto flex-1 p-2">
                            {searchResults.length > 0 && searchQuery.trim() !== "" ? (
                                searchResults.map((hit: any, index: number) => {
                                    const isSeries = hit.fields?.type === 'series' || hit.id.startsWith('s_');
                                    const coverPath = hit.fields?.cover_path;

                                    return (
                                        <div
                                            key={hit.id}
                                            onClick={() => handleSelectResult(hit)}
                                            onMouseEnter={() => setSelectedIndex(index)}
                                            className={`flex items-center gap-4 px-4 py-3 cursor-pointer rounded-lg transition-all ${index === selectedIndex ? 'bg-komgaPrimary/10 border-l-4 border-komgaPrimary shadow-md' : 'hover:bg-gray-800/50 border-l-4 border-transparent'}`}
                                        >
                                            <div className="w-12 h-18 sm:w-14 sm:h-20 bg-gray-900 rounded-md overflow-hidden flex-shrink-0 border border-gray-800 shadow-sm relative group-hover:border-komgaPrimary/30 transition-colors">
                                                {coverPath ? (
                                                    <img
                                                        src={`/api/thumbnails/${coverPath}`}
                                                        alt="preview"
                                                        className="w-full h-full object-cover transition-transform group-hover:scale-110"
                                                        onError={(e) => { (e.target as any).src = ''; (e.target as any).nextSibling.style.display = 'flex'; (e.target as any).style.display = 'none'; }}
                                                    />
                                                ) : null}
                                                <div className={`absolute inset-0 items-center justify-center bg-gray-800 flex ${coverPath ? 'hidden' : ''}`}>
                                                    <ImageIcon className="w-6 h-6 text-gray-700" />
                                                </div>
                                            </div>

                                            <div className="flex-1 min-w-0 flex flex-col justify-center">
                                                <div className="flex items-center space-x-2 mb-1">
                                                    {isSeries ? (
                                                        <span className="px-1.5 py-0.5 rounded bg-blue-500/20 text-blue-400 text-[10px] font-bold tracking-wider shrink-0 border border-blue-500/30 uppercase">系列</span>
                                                    ) : (
                                                        <span className="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-400 text-[10px] font-bold tracking-wider shrink-0 border border-emerald-500/30 uppercase">单册</span>
                                                    )}
                                                    <div className="text-base font-bold text-gray-100 truncate group-hover:text-komgaPrimary transition-colors">
                                                        {hit.fields?.title || hit.id}
                                                    </div>
                                                </div>
                                                <div className="text-xs text-gray-500 truncate flex items-center gap-2">
                                                    {isSeries ? (
                                                        <span>浏览整个系列内容</span>
                                                    ) : (
                                                        <>
                                                            <span className="text-komgaPrimary font-medium truncate max-w-[150px]">{hit.fields?.series_name || "未知系列"}</span>
                                                            <span className="text-gray-700">•</span>
                                                            <span>进入详情页阅读</span>
                                                        </>
                                                    )}
                                                </div>
                                            </div>
                                            <div className="hidden sm:flex flex-col items-end shrink-0 ml-2">
                                                <span className="text-[10px] text-gray-600 font-mono">SCORE</span>
                                                <span className={`text-xs font-bold ${hit.score > 0.5 ? 'text-komgaPrimary' : 'text-gray-500'}`}>{hit.score?.toFixed(2)}</span>
                                            </div>
                                        </div>
                                    );
                                })
                            ) : searchQuery.trim() !== "" ? (
                                <div className="py-14 text-center text-gray-500 text-sm">
                                    未找到符合条件的漫画
                                </div>
                            ) : (
                                <div className="py-8 text-center text-gray-600 text-sm flex flex-col items-center">
                                    <Search className="w-8 h-8 mb-3 opacity-20" />
                                    支持全局模糊检索、键盘上下方向键导航
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

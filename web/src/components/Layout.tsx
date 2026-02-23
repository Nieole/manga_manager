import { Outlet, Link, useParams, useNavigate, useLocation } from 'react-router-dom';
import { useState, useEffect } from 'react';
import axios from 'axios';
import { BookOpen, FolderOpen, Plus, X, Loader2, RefreshCw, Search } from 'lucide-react';

interface Library {
    id: string;
    name: string;
    path: string;
}

export default function Layout() {
    const [libraries, setLibraries] = useState<Library[]>([]);
    const [loading, setLoading] = useState(true);
    const [showAddModal, setShowAddModal] = useState(false);
    const [newLibName, setNewLibName] = useState("");
    const [newLibPath, setNewLibPath] = useState("");
    const [adding, setAdding] = useState(false);
    // 用于向所有 Outlet 子路由向下传递全局刷新信号的计数器
    const [refreshTrigger, setRefreshTrigger] = useState(0);

    const [searchQuery, setSearchQuery] = useState("");
    const [searchResults, setSearchResults] = useState<any[]>([]);

    const { libId } = useParams();
    const navigate = useNavigate();
    const location = useLocation();

    useEffect(() => {
        if (!searchQuery.trim()) {
            setSearchResults([]);
            return;
        }
        const timer = setTimeout(() => {
            axios.get(`/api/search?q=${encodeURIComponent(searchQuery)}`)
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
    }, [searchQuery]);

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
            if (event.data === "refresh") {
                console.log("Receive SSE refresh signal, reloading libraries...");
                fetchLibraries();
                // 收到后端推送后自增刷新信号以便子组件重新拉取元数据
                setRefreshTrigger(prev => prev + 1);
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

    const handleScanLibrary = async (e: React.MouseEvent, id: string) => {
        e.preventDefault();
        e.stopPropagation();
        try {
            await axios.post(`/api/libraries/${id}/scan`);
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
            // POST 只是将配置落库并派发给后台 Worker Pool，不必由于海量图库等待返回。
            await axios.post('/api/libraries', { name: newLibName, path: newLibPath });
            setShowAddModal(false);
            setNewLibName("");
            setNewLibPath("");
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
            <header className="bg-komgaSurface shadow-md sticky top-0 z-20 px-6 py-4 flex items-center justify-between border-b border-gray-800">
                <Link to="/" className="flex items-center space-x-3 w-64">
                    <BookOpen className="text-komgaPrimary h-8 w-8" />
                    <h1 className="text-2xl font-bold tracking-tight text-white hover:text-komgaPrimary transition">Manga Manager</h1>
                </Link>

                <div className="flex-1 max-w-xl relative">
                    <div className="relative group">
                        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-gray-500 group-focus-within:text-komgaPrimary transition-colors" />
                        <input
                            type="text"
                            placeholder="跨库模糊搜索漫画名称、连载系列..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            className="w-full bg-gray-900 border border-gray-800 rounded-full pl-10 pr-4 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 focus:border-transparent transition-all placeholder-gray-500"
                        />
                    </div>
                    {searchResults.length > 0 && searchQuery.trim() !== "" && (
                        <div className="absolute top-full mt-2 w-full bg-komgaSurface border border-gray-800 rounded-lg shadow-2xl overflow-hidden py-2 animate-in fade-in slide-in-from-top-2 duration-200">
                            {searchResults.map((hit: any) => (
                                <Link
                                    key={hit.id}
                                    to={`/reader/${hit.id}`}
                                    onClick={() => { setSearchQuery(""); setSearchResults([]); }}
                                    className="block px-4 py-3 hover:bg-gray-800 transition-colors"
                                >
                                    <div className="text-sm font-medium text-white truncate">{hit.fields?.title || hit.id}</div>
                                    <div className="text-xs text-komgaPrimary mt-1 truncate">{hit.fields?.series_name || "未知系列"} | 匹配度: {hit.score?.toFixed(2)}</div>
                                </Link>
                            ))}
                        </div>
                    )}
                </div>

                <div className="text-sm text-gray-400 w-64 text-right hidden lg:block">
                    Superfast & 100% Go Native
                </div>
            </header>

            <main className="flex-1 flex overflow-hidden">
                <aside className="w-64 bg-komgaSurface border-r border-gray-800 flex flex-col pt-6 hidden md:flex">
                    <div className="px-6 mb-4 flex items-center justify-between text-xs font-semibold text-gray-400 uppercase tracking-wider">
                        <span>Libraries</span>
                        <button
                            onClick={() => setShowAddModal(true)}
                            className="text-gray-400 hover:text-white transition-colors"
                            title="添加新资源库"
                        >
                            <Plus className="w-4 h-4" />
                        </button>
                    </div>
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
                                        onClick={(e) => handleScanLibrary(e, lib.id)}
                                        className="text-gray-500 hover:text-komgaPrimary opacity-0 group-hover:opacity-100 transition-opacity p-1"
                                        title="重新扫描库内的变动"
                                    >
                                        <RefreshCw className="w-4 h-4" />
                                    </button>
                                </Link>
                            ))
                        )}
                    </nav>
                </aside>

                <div className="flex-1 overflow-y-auto bg-komgaDark relative">
                    <Outlet context={{ refreshTrigger }} />
                </div>
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
                                    <label className="block text-sm font-medium text-gray-400 mb-1">绝对路径</label>
                                    <input
                                        type="text"
                                        required
                                        value={newLibPath}
                                        onChange={e => setNewLibPath(e.target.value)}
                                        placeholder="例如: /Users/nicoer/comic"
                                        className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2.5 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary focus:border-transparent transition-all"
                                    />
                                </div>
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
        </div>
    );
}

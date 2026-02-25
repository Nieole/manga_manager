import React, { useState, useEffect } from 'react';
import axios from 'axios';
import { Settings as SettingsIcon, Save, Server, Database, FolderOpen, HardDrive, AlertTriangle, RefreshCw, Image as ImageIcon, Download } from 'lucide-react';

interface Config {
    server: { port: number };
    database: { path: string };
    library: { paths: string[] };
    cache: { dir: string };
    scanner: {
        workers: number;
        thumbnail_format: string;
    };
}

const Settings: React.FC = () => {
    const [config, setConfig] = useState<Config | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [confirmModal, setConfirmModal] = useState<{
        isOpen: boolean;
        title: string;
        message: React.ReactNode;
        isDanger?: boolean;
        onConfirm: () => void;
    } | null>(null);
    const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

    const showToast = (text: string, type: 'success' | 'error' = 'success') => {
        setToastMsg({ text, type });
        setTimeout(() => setToastMsg(null), 3000);
    };

    useEffect(() => {
        fetchConfig();
    }, []);

    const fetchConfig = async () => {
        try {
            const res = await axios.get('/api/system/config');
            setConfig(res.data);
        } catch (error) {
            console.error("Failed to fetch system config:", error);
        } finally {
            setLoading(false);
        }
    };

    const handleSave = async () => {
        if (!config) return;
        setSaving(true);
        try {
            const res = await axios.post('/api/system/config', config);
            showToast(res.data.message || "配置已保存，请重启应用以生效", 'success');
        } catch (error) {
            console.error(error);
            showToast("保存失败", 'error');
        } finally {
            setSaving(false);
        }
    };

    const handleRebuildIndex = () => {
        setConfirmModal({
            isOpen: true,
            title: "全量重建搜索索引",
            message: "这将会彻底擦除并重建所有搜索分词缓存，可能导致暂时的搜索瘫痪。确定执行？",
            isDanger: true,
            onConfirm: async () => {
                setConfirmModal(null);
                try {
                    const res = await axios.post('/api/system/rebuild-index');
                    showToast(res.data.message || "重建指令已发出", 'success');
                } catch (error) {
                    console.error(error);
                    showToast("执行重建失败", 'error');
                }
            }
        });
    };

    const handleRebuildThumbnails = () => {
        setConfirmModal({
            isOpen: true,
            title: "全量重构封面图",
            message: "这将会清空全站缩略图并重新跑一遍全量提取，这会极大消耗 CPU 并引起磁盘 IO 风暴！确认执行？",
            isDanger: true,
            onConfirm: async () => {
                setConfirmModal(null);
                try {
                    const res = await axios.post('/api/system/rebuild-thumbnails');
                    showToast(res.data.message || "重抽指令已发出", 'success');
                } catch (error) {
                    console.error(error);
                    showToast("执行重构失败", 'error');
                }
            }
        });
    };

    const handleBatchScrape = () => {
        setConfirmModal({
            isOpen: true,
            title: "批量刮削元数据",
            message: (
                <div className="space-y-4">
                    <p className="text-sm text-gray-300">将按顺序对所有漫画系列发起元数据刮削，这可能需要较长时间。</p>
                    <div>
                        <label className="block text-gray-400 mb-2 text-sm">选择数据来源：</label>
                        <select
                            id="batch-scrape-provider"
                            className="w-full bg-gray-900 border border-gray-700 text-white rounded-lg px-3 py-2 outline-none focus:border-komgaPrimary transition"
                            defaultValue="bangumi"
                        >
                            <option value="bangumi">Bangumi (推荐)</option>
                            <option value="ollama">Ollama LLM</option>
                        </select>
                    </div>
                </div>
            ),
            isDanger: false,
            onConfirm: async () => {
                const selectElement = document.getElementById('batch-scrape-provider') as HTMLSelectElement;
                const provider = selectElement?.value || 'bangumi';
                setConfirmModal(null);
                try {
                    const res = await axios.post('/api/system/batch-scrape', { provider });
                    showToast(res.data.message || "批量刮削已后台异步启动", 'success');
                } catch (error) {
                    console.error(error);
                    showToast("执行批量刮削失败", 'error');
                }
            }
        });
    };

    if (loading) {
        return (
            <div className="flex h-full items-center justify-center">
                <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-komgaPrimary"></div>
            </div>
        );
    }

    if (!config) {
        return <div className="p-8 text-center text-gray-500">无法加载配置</div>;
    }

    return (
        <div className="p-8 max-w-4xl mx-auto pb-24">
            <div className="flex items-center space-x-3 mb-8">
                <SettingsIcon className="w-8 h-8 text-komgaPrimary" />
                <h1 className="text-2xl font-bold text-white tracking-tight">系统设定</h1>
            </div>

            <div className="grid gap-6">
                {/* 服务与网络 */}
                <div className="bg-komgaSurface border border-gray-800 rounded-xl p-6 shadow-sm">
                    <div className="flex items-center space-x-2 mb-4 text-komgaPrimary">
                        <Server className="w-5 h-5" />
                        <h2 className="text-lg font-semibold text-white">服务与网络</h2>
                    </div>
                    <div className="space-y-4">
                        <div>
                            <label className="block text-sm font-medium text-gray-400 mb-1">
                                服务端口 (Port)
                            </label>
                            <input
                                type="number"
                                value={config.server.port}
                                onChange={(e) => setConfig({ ...config, server: { ...config.server, port: parseInt(e.target.value) || 8080 } })}
                                className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                            />
                        </div>
                    </div>
                </div>

                {/* 存储引擎 */}
                <div className="bg-komgaSurface border border-gray-800 rounded-xl p-6 shadow-sm">
                    <div className="flex items-center space-x-2 mb-4 text-komgaPrimary">
                        <Database className="w-5 h-5" />
                        <h2 className="text-lg font-semibold text-white">持久化存储</h2>
                    </div>
                    <div className="space-y-4">
                        <div>
                            <label className="block text-sm font-medium text-gray-400 mb-1">
                                SQLite 数据库路径
                            </label>
                            <input
                                type="text"
                                value={config.database.path}
                                onChange={(e) => setConfig({ ...config, database: { ...config.database, path: e.target.value } })}
                                className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                            />
                        </div>
                    </div>
                </div>

                {/* 缓存机制 */}
                <div className="bg-komgaSurface border border-gray-800 rounded-xl p-6 shadow-sm">
                    <div className="flex items-center space-x-2 mb-4 text-komgaPrimary">
                        <HardDrive className="w-5 h-5" />
                        <h2 className="text-lg font-semibold text-white">缓存机制与生成策略</h2>
                    </div>
                    <div className="space-y-4">
                        <div>
                            <label className="block text-sm font-medium text-gray-400 mb-1">
                                已存在缩略图的物理预制基底
                            </label>
                            <input
                                type="text"
                                value={config.cache.dir}
                                onChange={(e) => setConfig({ ...config, cache: { ...config.cache, dir: e.target.value } })}
                                className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                            />
                        </div>
                        <div className="grid grid-cols-2 gap-4">
                            <div>
                                <label className="block text-sm font-medium text-gray-400 mb-1" title="置0则由程序动态以主机线程数翻倍挂起">
                                    解析器工作协程(Workers)
                                </label>
                                <input
                                    type="number"
                                    min="0"
                                    value={config.scanner.workers}
                                    onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, workers: parseInt(e.target.value) || 0 } })}
                                    className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                                />
                                <p className="text-xs text-gray-500 mt-1">留为 0 表示通过 CPU 数自动智能调度榨取。</p>
                            </div>
                            <div>
                                <label className="block text-sm font-medium text-gray-400 mb-1">
                                    生成图片压缩封包格式
                                </label>
                                <select
                                    value={config.scanner.thumbnail_format}
                                    onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, thumbnail_format: e.target.value } })}
                                    className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all appearance-none"
                                >
                                    <option value="webp">WebP (默认平衡优先)</option>
                                    <option value="avif">AVIF (次世代极致容量)</option>
                                    <option value="jpg">JPEG (老旧纯血兼容)</option>
                                </select>
                            </div>
                        </div>
                    </div>
                </div>

                {/* 已挂载库目录（只读预览，由首页管理） */}
                <div className="bg-komgaSurface border border-gray-800 rounded-xl p-6 shadow-sm opacity-70">
                    <div className="flex items-center space-x-2 mb-4 text-komgaPrimary">
                        <FolderOpen className="w-5 h-5" />
                        <h2 className="text-lg font-semibold text-white">已绑定的物理检索根节点</h2>
                    </div>
                    <div className="space-y-2">
                        {(!config.library.paths || config.library.paths.length === 0) && <p className="text-sm text-gray-500">尚无注册目录，请前往主页左侧边栏添加资源库。</p>}
                        {(config.library.paths || []).map((p, i) => (
                            <div key={i} className="text-sm text-gray-300 bg-gray-900 px-3 py-2 border border-gray-800 rounded-lg truncate">
                                {p}
                            </div>
                        ))}
                    </div>
                </div>

                {/* 高级与维护区 */}
                <div className="bg-red-900/10 border border-red-900/40 rounded-xl p-6 shadow-sm mt-4">
                    <div className="flex items-center space-x-2 mb-4 text-red-500">
                        <AlertTriangle className="w-5 h-5" />
                        <h2 className="text-lg font-semibold text-white">进阶危险维护操作</h2>
                    </div>
                    <p className="text-sm text-red-400 mb-6 max-w-2xl">
                        这些操作将直接越过保护对底层进行物理级数据结构撕洗。请确认您深知这些操作背后的代价。重建期间应用可能假死。
                    </p>
                    <div className="flex flex-col sm:flex-row space-y-4 sm:space-y-0 sm:space-x-4">
                        <button
                            onClick={handleRebuildIndex}
                            className="flex-1 flex items-center justify-center space-x-2 bg-red-500/10 hover:bg-red-500/20 border border-red-500/30 text-red-300 py-3 px-4 rounded-lg transition-colors group"
                        >
                            <RefreshCw className="w-4 h-4 group-hover:rotate-180 transition-transform duration-500" />
                            <span>全量重建搜索索引</span>
                        </button>

                        <button
                            onClick={handleRebuildThumbnails}
                            className="flex-1 flex items-center justify-center space-x-2 bg-red-500/10 hover:bg-red-500/20 border border-red-500/30 text-red-300 py-3 px-4 rounded-lg transition-colors group"
                        >
                            <ImageIcon className="w-4 h-4 group-hover:scale-110 transition-transform duration-300" />
                            <span>清空并重装所有封面图</span>
                        </button>
                        <button
                            onClick={handleBatchScrape}
                            className="flex-1 flex items-center justify-center space-x-2 bg-red-500/10 hover:bg-red-500/20 border border-red-500/30 text-red-300 py-3 px-4 rounded-lg transition-colors group"
                        >
                            <Download className="w-4 h-4 group-hover:translate-y-0.5 transition-transform duration-300" />
                            <span>批量 Bangumi 元数据刮削</span>
                        </button>
                    </div>
                </div>
            </div>

            {/* 底部悬浮保存动作栏 */}
            <div className="fixed bottom-0 left-0 right-0 p-4 bg-komgaDark/80 backdrop-blur-md border-t border-gray-800 flex justify-center z-10">
                <button
                    onClick={handleSave}
                    disabled={saving}
                    className="flex items-center space-x-2 bg-komgaPrimary hover:bg-komgaPrimary/90 text-white px-8 py-2.5 rounded-full font-medium shadow-lg shadow-komgaPrimary/20 transition-all disabled:opacity-50"
                >
                    {saving ? (
                        <div className="animate-spin rounded-full h-5 w-5 border-b-2 border-white"></div>
                    ) : (
                        <Save className="w-5 h-5" />
                    )}
                    <span>{saving ? '正在复写...' : '保存更改并提示重启'}</span>
                </button>
            </div>

            {/* Confirm Modal */}
            {confirmModal && confirmModal.isOpen && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
                    <div className="absolute inset-0 bg-black/60 backdrop-blur-sm" onClick={() => setConfirmModal(null)} />
                    <div className="relative bg-gray-800 border border-gray-700 rounded-xl shadow-2xl w-full max-w-md p-6 animate-in zoom-in-95 duration-200">
                        <h3 className="text-xl font-semibold text-white mb-4">{confirmModal.title}</h3>
                        <div className="text-gray-300 mb-8">{confirmModal.message}</div>
                        <div className="flex justify-end space-x-3">
                            <button
                                onClick={() => setConfirmModal(null)}
                                className="px-5 py-2.5 rounded-lg text-gray-400 font-medium hover:text-white hover:bg-white/10 transition-colors"
                            >
                                取消
                            </button>
                            <button
                                onClick={confirmModal.onConfirm}
                                className={`px-5 py-2.5 rounded-lg text-white font-medium transition-colors shadow-lg ${confirmModal.isDanger
                                        ? 'bg-red-500 hover:bg-red-600 shadow-red-500/20'
                                        : 'bg-komgaPrimary hover:bg-komgaPrimary/90 shadow-komgaPrimary/20'
                                    }`}
                            >
                                确认执行
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Toast 通知 */}
            {toastMsg && (
                <div className="fixed bottom-24 right-6 z-50 animate-in slide-in-from-bottom-5 fade-in duration-300">
                    <div className={`px-4 py-3 rounded-lg shadow-lg flex items-center gap-3 border ${toastMsg.type === 'success' ? 'bg-green-900 border-green-700 text-green-100' : 'bg-red-900 border-red-700 text-red-100'
                        }`}>
                        <span className="text-sm font-medium">{toastMsg.text}</span>
                        <button onClick={() => setToastMsg(null)} className="ml-2 text-white/50 hover:text-white">✕</button>
                    </div>
                </div>
            )}
        </div>
    );
};

export default Settings;

import React, { useState, useEffect } from 'react';
import axios from 'axios';
import { Settings as SettingsIcon, Save, Server, Database, FolderOpen, HardDrive } from 'lucide-react';

interface Config {
    server: { port: number };
    database: { path: string };
    library: { paths: string[] };
    cache: { dir: string };
}

const Settings: React.FC = () => {
    const [config, setConfig] = useState<Config | null>(null);
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);

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
            alert(res.data.message || "配置已保存，请重启应用以生效");
        } catch (error) {
            console.error(error);
            alert("保存失败");
        } finally {
            setSaving(false);
        }
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
                        <h2 className="text-lg font-semibold text-white">缓存机制</h2>
                    </div>
                    <div className="space-y-4">
                        <div>
                            <label className="block text-sm font-medium text-gray-400 mb-1">
                                封面与切片缓存路径
                            </label>
                            <input
                                type="text"
                                value={config.cache.dir}
                                onChange={(e) => setConfig({ ...config, cache: { ...config.cache, dir: e.target.value } })}
                                className="w-full bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                            />
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
                        {config.library.paths.length === 0 && <p className="text-sm text-gray-500">尚无注册目录，请前往主页左侧边栏添加资源库。</p>}
                        {config.library.paths.map((p, i) => (
                            <div key={i} className="text-sm text-gray-300 bg-gray-900 px-3 py-2 border border-gray-800 rounded-lg truncate">
                                {p}
                            </div>
                        ))}
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
        </div>
    );
};

export default Settings;

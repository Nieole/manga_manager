import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { FolderHeart, Plus, Trash2, ChevronRight, BookOpen, Search, X } from 'lucide-react';
import { ModalShell } from '../components/ui/ModalShell';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass, modalTextareaClass } from '../components/ui/modalStyles';

interface Collection {
    id: number;
    name: string;
    description: string;
    series_count: number;
    created_at: string;
}

interface CollectionSeriesItem {
    series_id: number;
    series_name: string;
    cover_path: { String: string; Valid: boolean };
    book_count: number;
}

export default function Collections() {
    const [collections, setCollections] = useState<Collection[]>([]);
    const [selected, setSelected] = useState<Collection | null>(null);
    const [seriesItems, setSeriesItems] = useState<CollectionSeriesItem[]>([]);
    const [showCreate, setShowCreate] = useState(false);
    const [newName, setNewName] = useState('');
    const [newDesc, setNewDesc] = useState('');
    const [loading, setLoading] = useState(true);
    const [pendingDeleteCollection, setPendingDeleteCollection] = useState<Collection | null>(null);
    const navigate = useNavigate();

    const fetchCollections = () => {
        axios.get('/api/collections/').then(res => {
            setCollections(res.data);
            setLoading(false);
        }).catch(() => setLoading(false));
    };

    useEffect(() => { fetchCollections(); }, []);

    const selectCollection = (c: Collection) => {
        setSelected(c);
        axios.get(`/api/collections/${c.id}/series`).then(res => {
            setSeriesItems(res.data || []);
        });
    };

    const handleCreate = () => {
        if (!newName.trim()) return;
        axios.post('/api/collections/', { name: newName, description: newDesc }).then(() => {
            setNewName('');
            setNewDesc('');
            setShowCreate(false);
            fetchCollections();
        });
    };

    const handleDelete = (id: number) => {
        axios.delete(`/api/collections/${id}`).then(() => {
            if (selected?.id === id) {
                setSelected(null);
                setSeriesItems([]);
            }
            fetchCollections();
        });
    };

    const handleRemoveSeries = (seriesId: number) => {
        if (!selected) return;
        axios.delete(`/api/collections/${selected.id}/series/${seriesId}`).then(() => {
            setSeriesItems(prev => prev.filter(s => s.series_id !== seriesId));
            fetchCollections();
        });
    };

    if (loading) {
        return (
            <div className="flex items-center justify-center h-full min-h-[60vh]">
                <div className="animate-spin rounded-full h-10 w-10 border-b-2 border-komgaPrimary"></div>
            </div>
        );
    }

    return (
        <div className="p-4 sm:p-8 max-w-6xl mx-auto">
            {/* 标题 + 新建按钮 */}
            <div className="flex items-center justify-between mb-6">
                <div className="flex items-center gap-3">
                    <FolderHeart className="w-7 h-7 text-komgaPrimary" />
                    <h1 className="text-2xl font-bold text-white tracking-tight">合集管理</h1>
                </div>
                <button
                    onClick={() => setShowCreate(true)}
                    className="flex items-center gap-2 px-4 py-2 bg-komgaPrimary hover:bg-purple-600 text-white rounded-lg transition-colors text-sm font-medium"
                >
                    <Plus className="w-4 h-4" />
                    新建合集
                </button>
            </div>

            {/* 新建合集弹窗 */}
            <ModalShell
                open={showCreate}
                onClose={() => setShowCreate(false)}
                title="新建合集"
                description="创建一个可复用的整理篮子，后续可以从资源库或系列页批量加入漫画。"
                icon={<FolderHeart className="h-5 w-5" />}
                size="compact"
                footer={
                    <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
                        <button onClick={() => setShowCreate(false)} className={modalGhostButtonClass}>取消</button>
                        <button onClick={handleCreate} className={modalPrimaryButtonClass}>创建</button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <input
                        value={newName}
                        onChange={e => setNewName(e.target.value)}
                        placeholder="合集名称"
                        className={modalInputClass}
                        autoFocus
                    />
                    <textarea
                        value={newDesc}
                        onChange={e => setNewDesc(e.target.value)}
                        placeholder="描述（可选）"
                        rows={4}
                        className={modalTextareaClass}
                    />
                </div>
            </ModalShell>

            <ConfirmDialog
                open={pendingDeleteCollection !== null}
                onClose={() => setPendingDeleteCollection(null)}
                onConfirm={() => {
                    if (!pendingDeleteCollection) return;
                    handleDelete(pendingDeleteCollection.id);
                    setPendingDeleteCollection(null);
                }}
                title="删除合集"
                description={`确定要删除合集「${pendingDeleteCollection?.name || ''}」吗？这个操作会移除合集本身，但不会删除原始漫画文件。`}
                confirmLabel="确认删除"
                tone="danger"
            />

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                {/* 左侧：合集列表 */}
                <div className="lg:col-span-1 space-y-2">
                    {collections.length === 0 ? (
                        <div className="text-center py-16 text-gray-600">
                            <FolderHeart className="w-12 h-12 mx-auto mb-3 opacity-50" />
                            <p className="text-sm">还没有合集</p>
                            <p className="text-xs mt-1">点击右上角"新建合集"开始整理你的漫画</p>
                        </div>
                    ) : (
                        collections.map(c => (
                            <div
                                key={c.id}
                                onClick={() => selectCollection(c)}
                                className={`group flex items-center justify-between p-4 rounded-xl border cursor-pointer transition-all duration-200 ${selected?.id === c.id
                                        ? 'bg-komgaPrimary/10 border-komgaPrimary/40 shadow-lg shadow-komgaPrimary/5'
                                        : 'bg-komgaSurface border-gray-800 hover:border-gray-700 hover:bg-gray-900'
                                    }`}
                            >
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2">
                                        <FolderHeart className={`w-4 h-4 shrink-0 ${selected?.id === c.id ? 'text-komgaPrimary' : 'text-gray-600'}`} />
                                        <p className="font-medium text-white truncate">{c.name}</p>
                                    </div>
                                    <p className="text-xs text-gray-500 mt-1 ml-6">{c.series_count} 个系列</p>
                                </div>
                                <div className="flex items-center gap-1.5 shrink-0">
                                    <button
                                        onClick={e => { e.stopPropagation(); setPendingDeleteCollection(c); }}
                                        className="p-1.5 rounded-lg text-gray-600 hover:text-red-400 hover:bg-red-900/20 transition opacity-0 group-hover:opacity-100"
                                    >
                                        <Trash2 className="w-3.5 h-3.5" />
                                    </button>
                                    <ChevronRight className={`w-4 h-4 transition-colors ${selected?.id === c.id ? 'text-komgaPrimary' : 'text-gray-700'}`} />
                                </div>
                            </div>
                        ))
                    )}
                </div>

                {/* 右侧：选中合集的系列 */}
                <div className="lg:col-span-2">
                    {selected ? (
                        <div>
                            <div className="flex items-center justify-between mb-4">
                                <div>
                                    <h2 className="text-lg font-semibold text-white">{selected.name}</h2>
                                    {selected.description && <p className="text-xs text-gray-500 mt-1">{selected.description}</p>}
                                </div>
                                <span className="text-xs text-gray-500 bg-gray-900 px-3 py-1 rounded-full">{seriesItems.length} 个系列</span>
                            </div>

                            {seriesItems.length === 0 ? (
                                <div className="text-center py-20 text-gray-600">
                                    <BookOpen className="w-10 h-10 mx-auto mb-3 opacity-40" />
                                    <p className="text-sm">合集暂无系列</p>
                                    <p className="text-xs mt-1">在资源库中将系列添加到此合集</p>
                                </div>
                            ) : (
                                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                                    {seriesItems.map(item => {
                                        const coverUrl = item.cover_path?.Valid ? `/api/thumbnails/${item.cover_path.String}` : '';
                                        return (
                                            <div key={item.series_id} className="group relative cursor-pointer" onClick={() => navigate(`/series/${item.series_id}`)}>
                                                <div className="aspect-[2/3] rounded-xl overflow-hidden bg-gray-900 border border-gray-800 group-hover:border-komgaPrimary/40 transition-all shadow-lg">
                                                    {coverUrl ? (
                                                        <img src={coverUrl} alt={item.series_name} className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                                                    ) : (
                                                        <div className="w-full h-full flex items-center justify-center text-gray-700"><BookOpen className="w-8 h-8" /></div>
                                                    )}
                                                    {/* 移除按钮 */}
                                                    <button
                                                        onClick={e => { e.stopPropagation(); handleRemoveSeries(item.series_id); }}
                                                        className="absolute top-2 right-2 p-1.5 rounded-full bg-black/70 text-white/60 hover:text-red-400 hover:bg-red-900/80 opacity-0 group-hover:opacity-100 transition-all"
                                                    >
                                                        <X className="w-3 h-3" />
                                                    </button>
                                                </div>
                                                <p className="text-xs text-gray-300 mt-2 truncate group-hover:text-komgaPrimary transition-colors">{item.series_name}</p>
                                                <p className="text-[10px] text-gray-600">{item.book_count} 册</p>
                                            </div>
                                        );
                                    })}
                                </div>
                            )}
                        </div>
                    ) : (
                        <div className="flex items-center justify-center h-full min-h-[40vh] text-gray-600">
                            <div className="text-center">
                                <Search className="w-10 h-10 mx-auto mb-3 opacity-30" />
                                <p className="text-sm">选择左侧合集查看内容</p>
                            </div>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

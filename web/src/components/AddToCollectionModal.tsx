import { useState, useEffect } from 'react';
import axios from 'axios';
import { X, FolderHeart, Loader2 } from 'lucide-react';

interface Collection {
    id: number;
    name: string;
    description: string;
    series_count: number;
}

interface Props {
    seriesIds: number[];
    onClose: () => void;
    onSuccess: () => void;
}

export default function AddToCollectionModal({ seriesIds, onClose, onSuccess }: Props) {
    const [collections, setCollections] = useState<Collection[]>([]);
    const [loading, setLoading] = useState(true);
    const [addingTo, setAddingTo] = useState<number | null>(null);

    useEffect(() => {
        axios.get('/api/collections/')
            .then(res => setCollections(res.data || []))
            .catch(err => console.error(err))
            .finally(() => setLoading(false));
    }, []);

    const handleAddToCollection = (collectionId: number) => {
        setAddingTo(collectionId);
        axios.post(`/api/collections/${collectionId}/series`, { series_ids: seriesIds })
            .then(() => {
                onSuccess();
                setTimeout(() => onClose(), 500); // 稍微延迟关闭以显示反馈
            })
            .catch(err => console.error(err))
            .finally(() => setAddingTo(null));
    };

    return (
        <div className="fixed inset-0 bg-black/60 z-[100] flex items-center justify-center p-4 backdrop-blur-sm" onClick={onClose}>
            <div className="bg-komgaSurface border border-gray-800 rounded-2xl p-6 w-full max-w-md shadow-2xl" onClick={e => e.stopPropagation()}>
                <div className="flex items-center justify-between mb-6">
                    <h3 className="text-lg font-semibold text-white flex items-center gap-2">
                        <FolderHeart className="w-5 h-5 text-komgaPrimary" />
                        添加到合集
                    </h3>
                    <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
                        <X className="w-5 h-5" />
                    </button>
                </div>

                {loading ? (
                    <div className="flex justify-center py-8">
                        <Loader2 className="w-8 h-8 animate-spin text-komgaPrimary" />
                    </div>
                ) : collections.length === 0 ? (
                    <div className="text-center py-8 text-gray-500">
                        <p>还没有创建任何合集</p>
                        <p className="text-xs mt-2">请先在侧边栏的"合集"页面创建一个合集</p>
                    </div>
                ) : (
                    <div className="space-y-2 max-h-[60vh] overflow-y-auto pr-2 custom-scrollbar">
                        {collections.map(c => (
                            <button
                                key={c.id}
                                disabled={addingTo !== null}
                                onClick={() => handleAddToCollection(c.id)}
                                className="w-full flex items-center justify-between p-3 rounded-xl border border-gray-800 bg-gray-900/50 hover:bg-gray-800 hover:border-gray-700 transition-all text-left group disabled:opacity-50"
                            >
                                <div>
                                    <h4 className="font-medium text-gray-200 group-hover:text-white">{c.name}</h4>
                                    <p className="text-xs text-gray-500 mt-1">{c.series_count} 个系列</p>
                                </div>
                                {addingTo === c.id ? (
                                    <Loader2 className="w-4 h-4 animate-spin text-komgaPrimary" />
                                ) : (
                                    <div className="text-komgaPrimary opacity-0 group-hover:opacity-100 transition-opacity text-sm">
                                        添加
                                    </div>
                                )}
                            </button>
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

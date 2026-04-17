import { useState, useEffect } from 'react';
import axios from 'axios';
import { FolderHeart, Loader2 } from 'lucide-react';
import { ModalShell } from './ui/ModalShell';
import { modalPrimaryButtonClass, modalSectionClass } from './ui/modalStyles';

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
        <ModalShell
            open
            onClose={onClose}
            title="添加到合集"
            description={`将已选择的 ${seriesIds.length} 个系列归档到一个已有合集。`}
            icon={<FolderHeart className="h-5 w-5" />}
            size="compact"
            zIndexClassName="z-[100]"
        >
            <div className="space-y-4">
                {loading ? (
                    <div className={`${modalSectionClass} flex justify-center py-10`}>
                        <Loader2 className="w-8 h-8 animate-spin text-komgaPrimary" />
                    </div>
                ) : collections.length === 0 ? (
                    <div className={`${modalSectionClass} text-center py-10 text-gray-400`}>
                        <p className="text-sm font-medium text-gray-200">还没有创建任何合集</p>
                        <p className="mt-2 text-xs text-gray-500">请先在侧边栏的“合集”页面创建一个合集</p>
                    </div>
                ) : (
                    <div className={`${modalSectionClass} max-h-[58vh] space-y-2 overflow-y-auto pr-1 custom-scrollbar`}>
                        {collections.map(c => (
                            <button
                                key={c.id}
                                disabled={addingTo !== null}
                                onClick={() => handleAddToCollection(c.id)}
                                className="group flex w-full items-center justify-between rounded-2xl border border-gray-800 bg-gray-900/60 px-4 py-3.5 text-left transition-all hover:border-gray-700 hover:bg-gray-800/80 disabled:opacity-50"
                            >
                                <div>
                                    <h4 className="font-medium text-gray-100 transition-colors group-hover:text-white">{c.name}</h4>
                                    <p className="mt-1 text-xs text-gray-500">{c.series_count} 个系列</p>
                                </div>
                                {addingTo === c.id ? (
                                    <Loader2 className="w-4 h-4 animate-spin text-komgaPrimary" />
                                ) : (
                                    <div className={`${modalPrimaryButtonClass} px-3 py-2 text-xs opacity-0 transition-opacity group-hover:opacity-100`}>
                                        添加
                                    </div>
                                )}
                            </button>
                        ))}
                    </div>
                )}
            </div>
        </ModalShell>
    );
}

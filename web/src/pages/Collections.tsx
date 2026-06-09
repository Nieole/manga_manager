/**
 * 业务说明：本文件是业务实现，属于项目源码的一部分，负责支撑漫画管理器在资料库、阅读器、扫描、元数据或系统设置中的具体业务能力。
 * 它与相邻模块共同组成前后端业务链路，修改时需要结合调用方理解数据流和用户可见行为。
 * 维护时应关注输入输出契约、错误处理、状态同步和与既有业务语义的一致性。
 */

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import axios from 'axios';
import { FolderHeart, Plus, Trash2, ChevronRight, BookOpen, Search, X, Pencil, SlidersHorizontal, Camera, AlertTriangle } from 'lucide-react';
import { ModalShell } from '../components/ui/ModalShell';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { modalGhostButtonClass, modalInputClass, modalPrimaryButtonClass, modalTextareaClass } from '../components/ui/modalStyles';
import { useI18n } from '../i18n/LocaleProvider';

interface Collection {
    view_id: string;
    kind: 'collection' | 'smart';
    id: number;
    numeric_id: number;
    name: string;
    description: string;
    series_count: number;
    source_type: string;
    source_review_id?: number;
    created_at: string;
    library_id?: number;
    activeTag?: string | null;
    activeAuthor?: string | null;
    activeStatus?: string | null;
    activeLetter?: string | null;
    readState?: string | null;
    minRating?: number | null;
    maxRating?: number | null;
    minProgress?: number | null;
    maxProgress?: number | null;
    addedWithinDays?: number | null;
    sortByField?: string;
    sortDir?: string;
    pageSize?: number;
}

interface CollectionSeriesItem {
    series_id: number;
    series_name: string;
    cover_path: { String: string; Valid: boolean };
    book_count: number;
}

interface SmartCollectionSeriesItem {
    id: number;
    name: string;
    title?: { String: string; Valid: boolean };
    cover_path?: { String: string; Valid: boolean };
    actual_book_count?: number;
    book_count?: number;
}

interface SmartCollectionSeriesResponse {
    items: SmartCollectionSeriesItem[];
    total: number;
    filter: Collection;
}

interface SmartCollectionSnapshotPreview {
    items: SmartCollectionSeriesItem[];
    total: number;
    preview_limit: number;
    snapshot_limit: number;
    snapshot_count: number;
    truncated: boolean;
    name_conflict: boolean;
}

export default function Collections() {
    const { t } = useI18n();
    const [collections, setCollections] = useState<Collection[]>([]);
    const [selected, setSelected] = useState<Collection | null>(null);
    const [seriesItems, setSeriesItems] = useState<CollectionSeriesItem[]>([]);
    const [showCreate, setShowCreate] = useState(false);
    const [newName, setNewName] = useState('');
    const [newDesc, setNewDesc] = useState('');
    const [showEdit, setShowEdit] = useState(false);
    const [editName, setEditName] = useState('');
    const [editDesc, setEditDesc] = useState('');
    const [smartName, setSmartName] = useState('');
    const [smartTag, setSmartTag] = useState('');
    const [smartAuthor, setSmartAuthor] = useState('');
    const [smartStatus, setSmartStatus] = useState('');
    const [smartLetter, setSmartLetter] = useState('');
    const [smartReadState, setSmartReadState] = useState('');
    const [smartMinRating, setSmartMinRating] = useState('');
    const [smartMaxRating, setSmartMaxRating] = useState('');
    const [smartMinProgress, setSmartMinProgress] = useState('');
    const [smartMaxProgress, setSmartMaxProgress] = useState('');
    const [smartAddedWithinDays, setSmartAddedWithinDays] = useState('');
    const [smartSortBy, setSmartSortBy] = useState('name');
    const [smartSortDir, setSmartSortDir] = useState('asc');
    const [smartPageSize, setSmartPageSize] = useState(30);
    const [showSmartEdit, setShowSmartEdit] = useState(false);
    const [showSnapshot, setShowSnapshot] = useState(false);
    const [snapshotName, setSnapshotName] = useState('');
    const [snapshotDesc, setSnapshotDesc] = useState('');
    const [snapshotPreview, setSnapshotPreview] = useState<SmartCollectionSnapshotPreview | null>(null);
    const [snapshotPreviewLoading, setSnapshotPreviewLoading] = useState(false);
    const [loading, setLoading] = useState(true);
    const [pendingDeleteCollection, setPendingDeleteCollection] = useState<Collection | null>(null);
    const [pendingDeleteSmart, setPendingDeleteSmart] = useState<Collection | null>(null);
    const [kindTab, setKindTab] = useState<'all' | 'manual' | 'smart'>('all');
    const navigate = useNavigate();

    const fetchCollections = () => {
        axios.get('/api/collection-views').then(res => {
            setCollections((res.data || []).map((item: Collection) => ({
                ...item,
                id: item.numeric_id,
            })));
            setLoading(false);
        }).catch(() => setLoading(false));
    };

    useEffect(() => { fetchCollections(); }, []);

    const selectCollection = (c: Collection) => {
        setSelected(c);
        const request = c.kind === 'smart'
            ? axios.get<SmartCollectionSeriesResponse>(`/api/collection-views/smart/${c.numeric_id}/series`)
            : axios.get(`/api/collections/${c.numeric_id}/series`);
        request.then(res => {
            if (c.kind === 'smart') {
                const payload = res.data as SmartCollectionSeriesResponse;
                setSeriesItems((payload.items || []).map((item) => ({
                    series_id: item.id,
                    series_name: item.title?.Valid ? item.title.String : item.name,
                    cover_path: item.cover_path || { String: '', Valid: false },
                    book_count: item.actual_book_count ?? item.book_count ?? 0,
                })));
                setSelected((current) => current?.view_id === c.view_id ? {
                    ...current,
                    ...payload.filter,
                    id: Number(payload.filter.id),
                    numeric_id: Number(payload.filter.id),
                } as Collection : current);
                return;
            }
            setSeriesItems(res.data || []);
        });
    };

    const openSmartEdit = () => {
        if (!selected || selected.kind !== 'smart') return;
        setSmartName(selected.name);
        setSmartTag(selected.activeTag || '');
        setSmartAuthor(selected.activeAuthor || '');
        setSmartStatus(selected.activeStatus || '');
        setSmartLetter(selected.activeLetter || '');
        setSmartReadState(selected.readState || '');
        setSmartMinRating(selected.minRating != null ? String(selected.minRating) : '');
        setSmartMaxRating(selected.maxRating != null ? String(selected.maxRating) : '');
        setSmartMinProgress(selected.minProgress != null ? String(selected.minProgress) : '');
        setSmartMaxProgress(selected.maxProgress != null ? String(selected.maxProgress) : '');
        setSmartAddedWithinDays(selected.addedWithinDays != null ? String(selected.addedWithinDays) : '');
        setSmartSortBy(selected.sortByField || 'name');
        setSmartSortDir(selected.sortDir || 'asc');
        setSmartPageSize(selected.pageSize || 30);
        setShowSmartEdit(true);
    };

    const submitSmartEdit = () => {
        if (!selected || selected.kind !== 'smart' || !smartName.trim()) return;
        axios.put(`/api/smart-filters/${selected.numeric_id}`, {
            name: smartName,
            activeTag: smartTag || null,
            activeAuthor: smartAuthor || null,
            activeStatus: smartStatus || null,
            activeLetter: smartLetter || null,
            readState: smartReadState || null,
            minRating: smartMinRating === '' ? null : Number(smartMinRating),
            maxRating: smartMaxRating === '' ? null : Number(smartMaxRating),
            minProgress: smartMinProgress === '' ? null : Number(smartMinProgress),
            maxProgress: smartMaxProgress === '' ? null : Number(smartMaxProgress),
            addedWithinDays: smartAddedWithinDays === '' ? null : Number(smartAddedWithinDays),
            sortByField: smartSortBy,
            sortDir: smartSortDir,
            pageSize: smartPageSize,
        }).then((res) => {
            setShowSmartEdit(false);
            const updated = {
                ...selected,
                ...res.data,
                name: res.data.name,
                numeric_id: Number(res.data.id),
                id: Number(res.data.id),
            };
            setSelected(updated);
            fetchCollections();
            selectCollection(updated);
        });
    };

    const deleteSmart = (item: Collection) => {
        axios.delete(`/api/smart-filters/${item.numeric_id}`).then(() => {
            if (selected?.view_id === item.view_id) {
                setSelected(null);
                setSeriesItems([]);
            }
            fetchCollections();
        });
    };

    const openSnapshot = () => {
        if (!selected || selected.kind !== 'smart') return;
        setSnapshotName(selected.name);
        setSnapshotDesc('');
        setSnapshotPreview(null);
        setShowSnapshot(true);
    };

    useEffect(() => {
        if (!showSnapshot || !selected || selected.kind !== 'smart') return;
        const timer = window.setTimeout(() => {
            setSnapshotPreviewLoading(true);
            axios.get<SmartCollectionSnapshotPreview>(`/api/collection-views/smart/${selected.numeric_id}/snapshot-preview`, {
                params: {
                    name: snapshotName,
                    description: snapshotDesc,
                    preview_limit: 8,
                },
            }).then((res) => {
                setSnapshotPreview(res.data);
            }).finally(() => {
                setSnapshotPreviewLoading(false);
            });
        }, 220);
        return () => window.clearTimeout(timer);
    }, [selected, showSnapshot, snapshotName, snapshotDesc]);

    const submitSnapshot = () => {
        if (!selected || selected.kind !== 'smart' || !snapshotName.trim()) return;
        if (snapshotPreview && snapshotPreview.snapshot_count <= 0) return;
        axios.post(`/api/collection-views/smart/${selected.numeric_id}/snapshot`, {
            name: snapshotName,
            description: snapshotDesc,
        }).then(() => {
            setShowSnapshot(false);
            fetchCollections();
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

    const handleEditSubmit = () => {
        if (!selected || !editName.trim()) return;
        if (selected.kind !== 'collection') return;
        axios.put(`/api/collections/${selected.numeric_id}`, { name: editName, description: editDesc }).then(() => {
            setShowEdit(false);
            // Update selected locally
            setSelected({ ...selected, name: editName, description: editDesc });
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
        if (!selected || selected.kind !== 'collection') return;
        axios.delete(`/api/collections/${selected.numeric_id}/series/${seriesId}`).then(() => {
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
                    <h1 className="text-2xl font-bold text-white tracking-tight">{t('collections.title')}</h1>
                </div>
                <button
                    onClick={() => setShowCreate(true)}
                    className="flex items-center gap-2 px-4 py-2 bg-komgaPrimary hover:bg-komgaPrimaryHover text-white rounded-lg transition-colors text-sm font-medium"
                >
                    <Plus className="w-4 h-4" />
                    {t('collections.create')}
                </button>
            </div>

            {/* 新建合集弹窗 */}
            <ModalShell
                open={showCreate}
                onClose={() => setShowCreate(false)}
                title={t('collections.createTitle')}
                description={t('collections.createDescription')}
                icon={<FolderHeart className="h-5 w-5" />}
                size="compact"
                footer={
                    <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
                        <button onClick={() => setShowCreate(false)} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
                        <button onClick={handleCreate} className={modalPrimaryButtonClass}>{t('common.create')}</button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <input
                        value={newName}
                        onChange={e => setNewName(e.target.value)}
                        placeholder={t('collections.namePlaceholder')}
                        className={modalInputClass}
                        autoFocus
                    />
                    <textarea
                        value={newDesc}
                        onChange={e => setNewDesc(e.target.value)}
                        placeholder={t('collections.descriptionPlaceholder')}
                        rows={4}
                        className={modalTextareaClass}
                    />
                </div>
            </ModalShell>

            {/* 编辑合集弹窗 */}
            <ModalShell
                open={showEdit}
                onClose={() => setShowEdit(false)}
                title={t('collections.editTitle')}
                description={t('collections.editDescription')}
                icon={<Pencil className="h-5 w-5" />}
                size="compact"
                footer={
                    <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
                        <button onClick={() => setShowEdit(false)} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
                        <button onClick={handleEditSubmit} className={modalPrimaryButtonClass}>{t('collections.editSubmit')}</button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <input
                        value={editName}
                        onChange={e => setEditName(e.target.value)}
                        placeholder={t('collections.namePlaceholder')}
                        className={modalInputClass}
                        autoFocus
                    />
                    <textarea
                        value={editDesc}
                        onChange={e => setEditDesc(e.target.value)}
                        placeholder={t('collections.descriptionPlaceholder')}
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
                title={t('collections.deleteTitle')}
                description={t('collections.deleteDescription', { name: pendingDeleteCollection?.name || '' })}
                confirmLabel={t('collections.confirmDelete')}
                tone="danger"
            />

            <ConfirmDialog
                open={pendingDeleteSmart !== null}
                onClose={() => setPendingDeleteSmart(null)}
                onConfirm={() => {
                    if (!pendingDeleteSmart) return;
                    deleteSmart(pendingDeleteSmart);
                    setPendingDeleteSmart(null);
                }}
                title={t('collections.deleteSmartTitle')}
                description={t('collections.deleteSmartDescription', { name: pendingDeleteSmart?.name || '' })}
                confirmLabel={t('collections.confirmDelete')}
                tone="danger"
            />

            <ModalShell
                open={showSmartEdit}
                onClose={() => setShowSmartEdit(false)}
                title={t('collections.editSmartTitle')}
                description={t('collections.editSmartDescription')}
                icon={<SlidersHorizontal className="h-5 w-5" />}
                size="standard"
                footer={
                    <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
                        <button onClick={() => setShowSmartEdit(false)} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
                        <button onClick={submitSmartEdit} className={modalPrimaryButtonClass}>{t('collections.editSubmit')}</button>
                    </div>
                }
            >
                <div className="grid gap-4 sm:grid-cols-2">
                    <input value={smartName} onChange={e => setSmartName(e.target.value)} placeholder={t('collections.namePlaceholder')} className={modalInputClass} />
                    <input value={smartTag} onChange={e => setSmartTag(e.target.value)} placeholder={t('collections.smartTagPlaceholder')} className={modalInputClass} />
                    <input value={smartAuthor} onChange={e => setSmartAuthor(e.target.value)} placeholder={t('collections.smartAuthorPlaceholder')} className={modalInputClass} />
                    <input value={smartStatus} onChange={e => setSmartStatus(e.target.value)} placeholder={t('collections.smartStatusPlaceholder')} className={modalInputClass} />
                    <input value={smartLetter} onChange={e => setSmartLetter(e.target.value.toUpperCase())} placeholder={t('collections.smartLetterPlaceholder')} className={modalInputClass} />
                    <select value={smartReadState} onChange={e => setSmartReadState(e.target.value)} className={modalInputClass}>
                        <option value="">{t('collections.readState.any')}</option>
                        <option value="unread">{t('collections.readState.unread')}</option>
                        <option value="reading">{t('collections.readState.reading')}</option>
                        <option value="completed">{t('collections.readState.completed')}</option>
                    </select>
                    <input type="number" min="0" max="10" step="0.1" value={smartMinRating} onChange={e => setSmartMinRating(e.target.value)} placeholder={t('collections.minRatingPlaceholder')} className={modalInputClass} />
                    <input type="number" min="0" max="10" step="0.1" value={smartMaxRating} onChange={e => setSmartMaxRating(e.target.value)} placeholder={t('collections.maxRatingPlaceholder')} className={modalInputClass} />
                    <input type="number" min="0" max="100" step="1" value={smartMinProgress} onChange={e => setSmartMinProgress(e.target.value)} placeholder={t('collections.minProgressPlaceholder')} className={modalInputClass} />
                    <input type="number" min="0" max="100" step="1" value={smartMaxProgress} onChange={e => setSmartMaxProgress(e.target.value)} placeholder={t('collections.maxProgressPlaceholder')} className={modalInputClass} />
                    <input type="number" min="1" max="3650" step="1" value={smartAddedWithinDays} onChange={e => setSmartAddedWithinDays(e.target.value)} placeholder={t('collections.addedWithinDaysPlaceholder')} className={modalInputClass} />
                    <select value={smartSortBy} onChange={e => setSmartSortBy(e.target.value)} className={modalInputClass}>
                        {['name', 'created', 'updated', 'rating', 'volumes', 'books', 'pages', 'read', 'favorite'].map((field) => <option key={field} value={field}>{t(`home.toolbar.sort.${field}`)}</option>)}
                    </select>
                    <select value={smartSortDir} onChange={e => setSmartSortDir(e.target.value)} className={modalInputClass}>
                        <option value="asc">{t('home.smartFilters.dir.asc')}</option>
                        <option value="desc">{t('home.smartFilters.dir.desc')}</option>
                    </select>
                    <select value={smartPageSize} onChange={e => setSmartPageSize(Number(e.target.value))} className={modalInputClass}>
                        {[30, 50, 100].map((size) => <option key={size} value={size}>{size}</option>)}
                    </select>
                </div>
            </ModalShell>

            <ModalShell
                open={showSnapshot}
                onClose={() => setShowSnapshot(false)}
                title={t('collections.snapshotTitle')}
                description={t('collections.snapshotDescription')}
                icon={<Camera className="h-5 w-5" />}
                size="standard"
                footer={
                    <div className="flex flex-col-reverse justify-end gap-3 sm:flex-row">
                        <button onClick={() => setShowSnapshot(false)} className={modalGhostButtonClass}>{t('modal.cancel')}</button>
                        <button
                            onClick={submitSnapshot}
                            disabled={!snapshotName.trim() || snapshotPreviewLoading || (snapshotPreview?.snapshot_count ?? 1) <= 0}
                            className={`${modalPrimaryButtonClass} disabled:cursor-not-allowed disabled:opacity-50`}
                        >
                            {t('collections.snapshotSubmit')}
                        </button>
                    </div>
                }
            >
                <div className="space-y-4">
                    <input value={snapshotName} onChange={e => setSnapshotName(e.target.value)} placeholder={t('collections.namePlaceholder')} className={modalInputClass} />
                    <textarea value={snapshotDesc} onChange={e => setSnapshotDesc(e.target.value)} placeholder={t('collections.descriptionPlaceholder')} rows={3} className={modalTextareaClass} />
                    <div className="rounded-2xl border border-gray-800 bg-gray-950/45 p-4">
                        <div className="grid gap-3 sm:grid-cols-3">
                            <div>
                                <p className="text-[11px] uppercase text-gray-500">{t('collections.snapshotPreview.total')}</p>
                                <p className="mt-1 text-lg font-semibold text-white">{snapshotPreviewLoading ? '...' : snapshotPreview?.total ?? '-'}</p>
                            </div>
                            <div>
                                <p className="text-[11px] uppercase text-gray-500">{t('collections.snapshotPreview.create')}</p>
                                <p className="mt-1 text-lg font-semibold text-white">{snapshotPreviewLoading ? '...' : snapshotPreview?.snapshot_count ?? '-'}</p>
                            </div>
                            <div>
                                <p className="text-[11px] uppercase text-gray-500">{t('collections.snapshotPreview.limit')}</p>
                                <p className="mt-1 text-lg font-semibold text-white">{snapshotPreview?.snapshot_limit ?? 1000}</p>
                            </div>
                        </div>
                        {snapshotPreview?.name_conflict && (
                            <div className="mt-4 flex gap-2 rounded-xl border border-amber-500/25 bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-500">
                                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                <span>{t('collections.snapshotPreview.nameConflict')}</span>
                            </div>
                        )}
                        {snapshotPreview?.truncated && (
                            <div className="mt-3 flex gap-2 rounded-xl border border-cyan-500/25 bg-cyan-500/10 px-3 py-2 text-xs text-cyan-100">
                                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                                <span>{t('collections.snapshotPreview.truncated', { count: snapshotPreview.snapshot_limit })}</span>
                            </div>
                        )}
                    </div>
                    <div className="space-y-2">
                        <div className="flex items-center justify-between">
                            <p className="text-xs font-medium text-gray-300">{t('collections.snapshotPreview.sample')}</p>
                            <p className="text-[11px] text-gray-500">{snapshotPreviewLoading ? t('common.loading') : t('common.seriesCount', { count: snapshotPreview?.items.length ?? 0 })}</p>
                        </div>
                        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                            {(snapshotPreview?.items || []).map((item) => {
                                const coverUrl = item.cover_path?.Valid ? `/api/thumbnails/${item.cover_path.String}` : '';
                                return (
                                    <div key={item.id} className="min-w-0 rounded-xl border border-gray-800 bg-gray-950/40 p-2">
                                        <div className="aspect-2/3 overflow-hidden rounded-lg bg-gray-900">
                                            {coverUrl ? (
                                                <img src={coverUrl} alt={item.name} className="h-full w-full object-cover" />
                                            ) : (
                                                <div className="flex h-full w-full items-center justify-center text-gray-700"><BookOpen className="h-6 w-6" /></div>
                                            )}
                                        </div>
                                        <p className="mt-2 truncate text-xs text-gray-300">{item.title?.Valid ? item.title.String : item.name}</p>
                                    </div>
                                );
                            })}
                        </div>
                        {!snapshotPreviewLoading && snapshotPreview?.total === 0 && (
                            <p className="rounded-xl border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-200">{t('collections.snapshotPreview.empty')}</p>
                        )}
                    </div>
                </div>
            </ModalShell>

            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                {/* 左侧：合集列表 */}
                <div className="lg:col-span-1 space-y-3">
                    <div className="flex gap-1 rounded-xl border border-gray-800 bg-gray-950/60 p-1">
                        {(['all', 'manual', 'smart'] as const).map((key) => {
                            const count = key === 'all'
                                ? collections.length
                                : key === 'manual'
                                    ? collections.filter((c) => c.kind === 'collection').length
                                    : collections.filter((c) => c.kind === 'smart').length;
                            const isActive = kindTab === key;
                            return (
                                <button
                                    key={key}
                                    type="button"
                                    onClick={() => setKindTab(key)}
                                    className={`flex-1 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                                        isActive ? 'bg-komgaPrimary text-white' : 'text-gray-400 hover:bg-gray-800/60 hover:text-white'
                                    }`}
                                >
                                    {t(`collections.kindTab.${key}`)}
                                    <span className={`ml-1.5 text-[10px] ${isActive ? 'text-white/80' : 'text-gray-500'}`}>{count}</span>
                                </button>
                            );
                        })}
                    </div>
                    {(() => {
                        const filtered = collections.filter((c) =>
                            kindTab === 'all'
                                ? true
                                : kindTab === 'manual'
                                    ? c.kind === 'collection'
                                    : c.kind === 'smart',
                        );
                        if (filtered.length === 0) {
                            return (
                                <div className="text-center py-16 text-gray-600">
                                    <FolderHeart className="w-12 h-12 mx-auto mb-3 opacity-50" />
                                    <p className="text-sm">{t('collections.empty')}</p>
                                    <p className="text-xs mt-1">{t('collections.emptyHint')}</p>
                                </div>
                            );
                        }
                        return filtered.map(c => (
                            <div
                                key={c.view_id}
                                onClick={() => selectCollection(c)}
                                className={`group flex items-center justify-between p-4 rounded-xl border cursor-pointer transition-all duration-200 ${selected?.view_id === c.view_id
                                        ? 'bg-komgaPrimary/10 border-komgaPrimary/40 shadow-lg shadow-komgaPrimary/5'
                                        : 'bg-komgaSurface border-gray-800 hover:border-gray-700 hover:bg-gray-900'
                                    }`}
                            >
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2">
                                        {c.kind === 'smart' ? (
                                            <SlidersHorizontal className={`w-4 h-4 shrink-0 ${selected?.view_id === c.view_id ? 'text-cyan-300' : 'text-gray-600'}`} />
                                        ) : (
                                            <FolderHeart className={`w-4 h-4 shrink-0 ${selected?.view_id === c.view_id ? 'text-komgaPrimary' : 'text-gray-600'}`} />
                                        )}
                                        <p className="font-medium text-white truncate">{c.name}</p>
                                    </div>
                                    <div className="mt-1 ml-6 flex flex-wrap items-center gap-2">
                                        <p className="text-xs text-gray-500">{t('common.seriesCount', { count: c.series_count })}</p>
                                        <span className="rounded-full border border-white/10 bg-white/5 px-2 py-0.5 text-[10px] text-gray-400">{t(`collections.source.${c.source_type || 'manual'}`)}</span>
                                    </div>
                                    {c.kind === 'smart' && <SmartFilterChips collection={c} t={t} />}
                                </div>
                                <div className="flex items-center gap-1.5 shrink-0">
                                    {c.kind === 'collection' && (
                                        <button
                                            onClick={e => { e.stopPropagation(); setPendingDeleteCollection(c); }}
                                            className="p-1.5 rounded-lg text-gray-600 hover:text-red-400 hover:bg-red-900/20 transition opacity-0 group-hover:opacity-100"
                                        >
                                            <Trash2 className="w-3.5 h-3.5" />
                                        </button>
                                    )}
                                    {c.kind === 'smart' && (
                                        <button
                                            onClick={e => { e.stopPropagation(); setPendingDeleteSmart(c); }}
                                            className="p-1.5 rounded-lg text-gray-600 hover:text-red-400 hover:bg-red-900/20 transition opacity-0 group-hover:opacity-100"
                                        >
                                            <Trash2 className="w-3.5 h-3.5" />
                                        </button>
                                    )}
                                    <ChevronRight className={`w-4 h-4 transition-colors ${selected?.view_id === c.view_id ? 'text-komgaPrimary' : 'text-gray-700'}`} />
                                </div>
                            </div>
                        ));
                    })()}
                </div>

                {/* 右侧：选中合集的系列 */}
                <div className="lg:col-span-2">
                    {selected ? (
                        <div>
                            <div className="flex items-center justify-between mb-4">
                                <div className="flex items-start justify-between w-full">
                                    <div>
                                        <div className="flex items-center gap-2">
                                            <h2 className="text-lg font-semibold text-white">{selected.name}</h2>
                                            <span className="rounded-full border border-white/10 bg-white/5 px-2 py-0.5 text-[10px] text-gray-400">{t(`collections.source.${selected.source_type || 'manual'}`)}</span>
                                            {selected.kind === 'collection' && (
                                                <button
                                                    onClick={() => {
                                                        setEditName(selected.name);
                                                        setEditDesc(selected.description);
                                                        setShowEdit(true);
                                                    }}
                                                    className="p-1 rounded-md text-gray-500 hover:text-white hover:bg-gray-800 transition-colors"
                                                    title={t('common.edit')}
                                                >
                                                    <Pencil className="w-3.5 h-3.5" />
                                                </button>
                                            )}
                                            {selected.kind === 'smart' && (
                                                <>
                                                    <button onClick={openSmartEdit} className="p-1 rounded-md text-gray-500 hover:text-white hover:bg-gray-800 transition-colors" title={t('common.edit')}>
                                                        <Pencil className="w-3.5 h-3.5" />
                                                    </button>
                                                    <button onClick={openSnapshot} className="p-1 rounded-md text-gray-500 hover:text-white hover:bg-gray-800 transition-colors" title={t('collections.snapshot')}>
                                                        <Camera className="w-3.5 h-3.5" />
                                                    </button>
                                                </>
                                            )}
                                        </div>
                                        {selected.description && <p className="text-xs text-gray-500 mt-1">{selected.description}</p>}
                                    </div>
                                    <span className="text-xs text-gray-500 bg-gray-900 px-3 py-1 rounded-full">{t('common.seriesCount', { count: seriesItems.length })}</span>
                                </div>
                            </div>

                            {seriesItems.length === 0 ? (
                                <div className="text-center py-20 text-gray-600">
                                    <BookOpen className="w-10 h-10 mx-auto mb-3 opacity-40" />
                                    <p className="text-sm">{t('collections.noSeries')}</p>
                                    <p className="text-xs mt-1">{t('collections.noSeriesHint')}</p>
                                </div>
                            ) : (
                                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                                    {seriesItems.map(item => {
                                        const coverUrl = item.cover_path?.Valid ? `/api/thumbnails/${item.cover_path.String}` : '';
                                        return (
                                            <div key={item.series_id} className="group relative cursor-pointer" onClick={() => navigate(`/series/${item.series_id}`)}>
                                                <div className="aspect-2/3 rounded-xl overflow-hidden bg-gray-900 border border-gray-800 group-hover:border-komgaPrimary/40 transition-all shadow-lg">
                                                    {coverUrl ? (
                                                        <img src={coverUrl} alt={item.series_name} className="w-full h-full object-cover group-hover:scale-105 transition-transform duration-500" />
                                                    ) : (
                                                        <div className="w-full h-full flex items-center justify-center text-gray-700"><BookOpen className="w-8 h-8" /></div>
                                                    )}
                                                    {/* 移除按钮 */}
                                                    {selected.kind === 'collection' && (
                                                        <button
                                                            onClick={e => { e.stopPropagation(); handleRemoveSeries(item.series_id); }}
                                                            className="absolute top-2 right-2 p-1.5 rounded-full bg-black/70 text-white/60 hover:text-red-400 hover:bg-red-900/80 opacity-0 group-hover:opacity-100 transition-all"
                                                        >
                                                            <X className="w-3 h-3" />
                                                        </button>
                                                    )}
                                                </div>
                                                <p className="text-xs text-gray-300 mt-2 truncate group-hover:text-komgaPrimary transition-colors">{item.series_name}</p>
                                                <p className="text-[10px] text-gray-600">{t('common.books', { count: item.book_count })}</p>
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
                                <p className="text-sm">{t('collections.pickLeft')}</p>
                            </div>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

function SmartFilterChips({ collection, t }: { collection: Collection; t: (key: string, vars?: Record<string, unknown>) => string }) {
    const chips: { key: string; label: string }[] = [];
    if (collection.activeTag) chips.push({ key: 'tag', label: `#${collection.activeTag}` });
    if (collection.activeAuthor) chips.push({ key: 'author', label: `@${collection.activeAuthor}` });
    if (collection.activeStatus) chips.push({ key: 'status', label: t(`collections.smartChip.status.${collection.activeStatus}`) });
    if (collection.activeLetter) chips.push({ key: 'letter', label: collection.activeLetter });
    if (collection.readState) chips.push({ key: 'readState', label: t(`collections.smartChip.read.${collection.readState}`) });
    if (collection.minRating != null || collection.maxRating != null) {
        const lo = collection.minRating != null ? collection.minRating : '';
        const hi = collection.maxRating != null ? collection.maxRating : '';
        chips.push({ key: 'rating', label: `★ ${lo}${lo !== '' || hi !== '' ? '–' : ''}${hi}` });
    }
    if (collection.minProgress != null || collection.maxProgress != null) {
        const lo = collection.minProgress != null ? `${collection.minProgress}%` : '';
        const hi = collection.maxProgress != null ? `${collection.maxProgress}%` : '';
        chips.push({ key: 'progress', label: `${t('collections.smartChip.progress')} ${lo}${lo !== '' || hi !== '' ? '–' : ''}${hi}` });
    }
    if (collection.addedWithinDays != null) {
        chips.push({ key: 'addedDays', label: t('collections.smartChip.addedWithinDays', { days: collection.addedWithinDays }) });
    }
    if (collection.sortByField) {
        const dir = collection.sortDir === 'desc' ? '↓' : '↑';
        chips.push({ key: 'sort', label: `${t(`collections.smartChip.sort.${collection.sortByField}`)} ${dir}` });
    }
    if (chips.length === 0) {
        return (
            <div className="mt-1 ml-6 flex flex-wrap items-center gap-1">
                <span className="rounded-full border border-white/5 bg-white/2 px-2 py-0.5 text-[10px] text-gray-600">{t('collections.smartChip.noFilter')}</span>
            </div>
        );
    }
    return (
        <div className="mt-1 ml-6 flex flex-wrap items-center gap-1">
            {chips.map((chip) => (
                <span key={chip.key} className="rounded-full border border-cyan-500/20 bg-cyan-500/10 px-2 py-0.5 text-[10px] text-cyan-200/90">
                    {chip.label}
                </span>
            ))}
        </div>
    );
}

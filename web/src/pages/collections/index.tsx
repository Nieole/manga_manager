/**
 * 业务说明：本文件是前端合集页面的编排入口，负责手工合集与智能合集的加载、选中、增删改、
 * 智能合集编辑与快照固化等交互状态与数据流；具体展示拆分到同目录的列表/详情/弹窗子组件。
 * 维护时应关注状态与各子组件的受控同步、操作后刷新，以及手工/智能合集的行为差异。
 */

import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { apiClient } from '../../api/client';
import { FolderHeart, Plus } from 'lucide-react';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';
import { useI18n } from '../../i18n/LocaleProvider';
import type {
  Collection,
  CollectionSeriesItem,
  SmartCollectionSeriesResponse,
  SmartCollectionSnapshotPreview,
} from './types';
import { CollectionListPanel } from './CollectionListPanel';
import { CollectionDetailPanel } from './CollectionDetailPanel';
import { CreateCollectionModal, EditCollectionModal } from './CollectionFormModals';
import { SmartEditModal } from './SmartEditModal';
import { SnapshotModal } from './SnapshotModal';

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
    apiClient.get('/api/collection-views').then(res => {
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
      ? apiClient.get<SmartCollectionSeriesResponse>(`/api/collection-views/smart/${c.numeric_id}/series`)
      : apiClient.get(`/api/collections/${c.numeric_id}/series`);
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
    apiClient.put(`/api/smart-filters/${selected.numeric_id}`, {
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
    apiClient.delete(`/api/smart-filters/${item.numeric_id}`).then(() => {
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
      apiClient.get<SmartCollectionSnapshotPreview>(`/api/collection-views/smart/${selected.numeric_id}/snapshot-preview`, {
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
    apiClient.post(`/api/collection-views/smart/${selected.numeric_id}/snapshot`, {
      name: snapshotName,
      description: snapshotDesc,
    }).then(() => {
      setShowSnapshot(false);
      fetchCollections();
    });
  };

  const handleCreate = () => {
    if (!newName.trim()) return;
    apiClient.post('/api/collections/', { name: newName, description: newDesc }).then(() => {
      setNewName('');
      setNewDesc('');
      setShowCreate(false);
      fetchCollections();
    });
  };

  const handleEditSubmit = () => {
    if (!selected || !editName.trim()) return;
    if (selected.kind !== 'collection') return;
    apiClient.put(`/api/collections/${selected.numeric_id}`, { name: editName, description: editDesc }).then(() => {
      setShowEdit(false);
      // Update selected locally
      setSelected({ ...selected, name: editName, description: editDesc });
      fetchCollections();
    });
  };

  const handleDelete = (id: number) => {
    apiClient.delete(`/api/collections/${id}`).then(() => {
      if (selected?.id === id) {
        setSelected(null);
        setSeriesItems([]);
      }
      fetchCollections();
    });
  };

  const handleRemoveSeries = (seriesId: number) => {
    if (!selected || selected.kind !== 'collection') return;
    apiClient.delete(`/api/collections/${selected.numeric_id}/series/${seriesId}`).then(() => {
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

      <CreateCollectionModal
        open={showCreate}
        onClose={() => setShowCreate(false)}
        name={newName}
        onNameChange={setNewName}
        desc={newDesc}
        onDescChange={setNewDesc}
        onSubmit={handleCreate}
        t={t}
      />

      <EditCollectionModal
        open={showEdit}
        onClose={() => setShowEdit(false)}
        name={editName}
        onNameChange={setEditName}
        desc={editDesc}
        onDescChange={setEditDesc}
        onSubmit={handleEditSubmit}
        t={t}
      />

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

      <SmartEditModal
        open={showSmartEdit}
        onClose={() => setShowSmartEdit(false)}
        onSubmit={submitSmartEdit}
        values={{
          name: smartName,
          tag: smartTag,
          author: smartAuthor,
          status: smartStatus,
          letter: smartLetter,
          readState: smartReadState,
          minRating: smartMinRating,
          maxRating: smartMaxRating,
          minProgress: smartMinProgress,
          maxProgress: smartMaxProgress,
          addedWithinDays: smartAddedWithinDays,
          sortBy: smartSortBy,
          sortDir: smartSortDir,
          pageSize: smartPageSize,
        }}
        set={{
          setName: setSmartName,
          setTag: setSmartTag,
          setAuthor: setSmartAuthor,
          setStatus: setSmartStatus,
          setLetter: setSmartLetter,
          setReadState: setSmartReadState,
          setMinRating: setSmartMinRating,
          setMaxRating: setSmartMaxRating,
          setMinProgress: setSmartMinProgress,
          setMaxProgress: setSmartMaxProgress,
          setAddedWithinDays: setSmartAddedWithinDays,
          setSortBy: setSmartSortBy,
          setSortDir: setSmartSortDir,
          setPageSize: setSmartPageSize,
        }}
        t={t}
      />

      <SnapshotModal
        open={showSnapshot}
        onClose={() => setShowSnapshot(false)}
        onSubmit={submitSnapshot}
        name={snapshotName}
        onNameChange={setSnapshotName}
        desc={snapshotDesc}
        onDescChange={setSnapshotDesc}
        preview={snapshotPreview}
        previewLoading={snapshotPreviewLoading}
        t={t}
      />

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <CollectionListPanel
          collections={collections}
          kindTab={kindTab}
          onKindTabChange={setKindTab}
          selected={selected}
          onSelect={selectCollection}
          onRequestDeleteCollection={setPendingDeleteCollection}
          onRequestDeleteSmart={setPendingDeleteSmart}
          t={t}
        />

        <CollectionDetailPanel
          selected={selected}
          seriesItems={seriesItems}
          onEditCollection={() => {
            if (!selected) return;
            setEditName(selected.name);
            setEditDesc(selected.description);
            setShowEdit(true);
          }}
          onOpenSmartEdit={openSmartEdit}
          onOpenSnapshot={openSnapshot}
          onRemoveSeries={handleRemoveSeries}
          onOpenSeries={(seriesId) => navigate(`/series/${seriesId}`)}
          t={t}
        />
      </div>
    </div>
  );
}

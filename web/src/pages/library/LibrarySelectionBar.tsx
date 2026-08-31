import { CheckCircle2, FolderHeart, Heart, Loader2, RefreshCw, Send, Tags } from 'lucide-react';
import { SelectionBar, type SelectionBarAction } from '../../components/ui/SelectionBar';
import { useI18n } from '../../i18n/LocaleProvider';

interface LibrarySelectionBarProps {
  visible: boolean;
  count: number;
  currentPageSelectedCount: number;
  bulkProgressUpdating: 'read' | 'unread' | null;
  externalReady: boolean;
  startingTransfer: boolean;
  onMarkFavorite: () => void;
  onUnmarkFavorite: () => void;
  onAddToCollection: () => void;
  onBulkEdit: () => void;
  onMarkRead: () => void;
  onMarkUnread: () => void;
  onTransfer: () => void;
  // canManage 为假时只留下标为已读/未读：那两条走每用户的进度端点，其余（收藏、加入合集、
  // 批量编辑、传输）在后端都是管理员专属。
  canManage: boolean;
}

export function LibrarySelectionBar({
  visible,
  count,
  currentPageSelectedCount,
  bulkProgressUpdating,
  externalReady,
  startingTransfer,
  onMarkFavorite,
  onUnmarkFavorite,
  onAddToCollection,
  onBulkEdit,
  onMarkRead,
  onMarkUnread,
  onTransfer,
  canManage,
}: LibrarySelectionBarProps) {
  const { t } = useI18n();
  const countLabel = (
    <>
      {t('home.selection.selectedCount', { count })}
      {currentPageSelectedCount > 0
        ? ` · ${t('home.selection.currentPageCount', { count: currentPageSelectedCount })}`
        : ''}
    </>
  );

  const adminActions: SelectionBarAction[] = [
    {
      key: 'fav',
      variant: 'danger',
      icon: <Heart className="w-4 h-4 fill-current" />,
      label: t('home.selection.markFavorite'),
      onClick: onMarkFavorite,
    },
    {
      key: 'unfav',
      variant: 'default',
      label: t('home.selection.removeFavorite'),
      onClick: onUnmarkFavorite,
    },
    {
      key: 'collection',
      variant: 'primary',
      icon: <FolderHeart className="w-4 h-4" />,
      label: t('home.selection.addToCollection'),
      onClick: onAddToCollection,
    },
    {
      key: 'bulk-edit',
      variant: 'primary',
      icon: <Tags className="w-4 h-4" />,
      label: t('home.selection.bulkEdit'),
      onClick: onBulkEdit,
    },
  ];

  const everyoneActions: SelectionBarAction[] = [
    {
      key: 'read',
      variant: 'success',
      icon: bulkProgressUpdating === 'read' ? <Loader2 className="w-4 h-4 animate-spin" /> : <CheckCircle2 className="w-4 h-4" />,
      label: bulkProgressUpdating === 'read' ? t('home.selection.updatingReadState') : t('home.selection.markRead'),
      onClick: onMarkRead,
      disabled: bulkProgressUpdating !== null,
    },
    {
      key: 'unread',
      variant: 'warning',
      icon: bulkProgressUpdating === 'unread' ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />,
      label: bulkProgressUpdating === 'unread' ? t('home.selection.updatingReadState') : t('home.selection.markUnread'),
      onClick: onMarkUnread,
      disabled: bulkProgressUpdating !== null,
    },
  ];

  const transferAction: SelectionBarAction[] = [
    {
      key: 'transfer',
      variant: 'info',
      icon: <Send className="w-4 h-4" />,
      label: startingTransfer ? t('home.transfer.submitting') : t('home.transfer.action'),
      onClick: onTransfer,
      disabled: startingTransfer || !externalReady,
    },
  ];

  const actions = canManage ? [...adminActions, ...everyoneActions, ...transferAction] : everyoneActions;

  return <SelectionBar visible={visible} count={count} countLabel={countLabel} actions={actions} />;
}

import { CheckCircle2, FolderHeart, Heart, Loader2, RefreshCw, Send } from 'lucide-react';
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
  onMarkRead: () => void;
  onMarkUnread: () => void;
  onTransfer: () => void;
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
  onMarkRead,
  onMarkUnread,
  onTransfer,
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

  const actions: SelectionBarAction[] = [
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
    {
      key: 'transfer',
      variant: 'info',
      icon: <Send className="w-4 h-4" />,
      label: startingTransfer ? t('home.transfer.submitting') : t('home.transfer.action'),
      onClick: onTransfer,
      disabled: startingTransfer || !externalReady,
    },
  ];

  return <SelectionBar visible={visible} count={count} countLabel={countLabel} actions={actions} />;
}

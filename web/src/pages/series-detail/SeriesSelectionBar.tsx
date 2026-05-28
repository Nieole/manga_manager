import { CheckCircle2, MinusCircle } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { SelectionBar } from '../../components/ui/SelectionBar';

interface SeriesSelectionBarProps {
  visible: boolean;
  selectedCount: number;
  allSelected: boolean;
  onSelectAllOrNone: () => void;
  onMarkRead: () => void;
  onMarkUnread: () => void;
  busy: boolean;
}

export function SeriesSelectionBar({
  visible,
  selectedCount,
  allSelected,
  onSelectAllOrNone,
  onMarkRead,
  onMarkUnread,
  busy,
}: SeriesSelectionBarProps) {
  const { t } = useI18n();
  return (
    <SelectionBar
      visible={visible}
      count={selectedCount}
      countLabel={t('series.selection.selectedCount', { count: selectedCount })}
      actions={[
        {
          key: 'select-all',
          label: allSelected ? t('series.selection.unselectAll') : t('series.selection.selectAll'),
          onClick: onSelectAllOrNone,
          variant: 'primary',
        },
        {
          key: 'mark-read',
          label: t('series.selection.markRead'),
          icon: <CheckCircle2 className="w-4 h-4" />,
          onClick: onMarkRead,
          variant: 'success',
          disabled: busy,
        },
        {
          key: 'mark-unread',
          label: t('series.selection.markUnread'),
          icon: <MinusCircle className="w-4 h-4" />,
          onClick: onMarkUnread,
          disabled: busy,
        },
      ]}
    />
  );
}

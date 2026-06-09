/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

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

/**
 * 业务说明：本文件是前端合集页面的智能合集筛选条件标签（chips）展示组件，把智能合集持久化的
 * 标签/作者/状态/评分/进度/时间/排序等筛选参数渲染为紧凑的可视芯片。
 * 维护时应与智能合集编辑表单的字段保持一致。
 */

import type { Collection, TFunc } from './types';
import { buildBoundedRangeLabel } from './rangeLabel';

export function SmartFilterChips({ collection, t }: { collection: Collection; t: TFunc }) {
  const chips: { key: string; label: string }[] = [];
  if (collection.activeTag) chips.push({ key: 'tag', label: `#${collection.activeTag}` });
  if (collection.activeAuthor) chips.push({ key: 'author', label: `@${collection.activeAuthor}` });
  if (collection.activeStatus) chips.push({ key: 'status', label: t(`collections.smartChip.status.${collection.activeStatus}`) });
  if (collection.activeLetter) chips.push({ key: 'letter', label: collection.activeLetter });
  if (collection.readState) chips.push({ key: 'readState', label: t(`collections.smartChip.read.${collection.readState}`) });
  if (collection.minRating != null || collection.maxRating != null) {
    chips.push({ key: 'rating', label: `★ ${buildBoundedRangeLabel(collection.minRating, collection.maxRating)}` });
  }
  if (collection.minProgress != null || collection.maxProgress != null) {
    chips.push({ key: 'progress', label: `${t('collections.smartChip.progress')} ${buildBoundedRangeLabel(collection.minProgress, collection.maxProgress, '%')}` });
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

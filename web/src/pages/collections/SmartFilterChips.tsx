/**
 * 本文件把智能书架的筛选定义渲染成合集页左栏的芯片，让用户在列表里一眼分清各个书架。
 * 排序不是筛选条件，默认的「名称正序」不占芯片；左栏窄，超出的条件折叠进「+N 项筛选」。
 */

import { normalizeSeriesStatus } from '../../i18n/status';
import { buildBoundedRangeLabel } from './rangeLabel';
import type { SmartFilter, TFunc } from './types';

// 左栏一行放得下的芯片数；再多就折叠，折叠掉的条件挂在 title 上仍看得到。
const MAX_VISIBLE_CHIPS = 3;

const chipClass = 'rounded-full border border-cyan-500/20 bg-cyan-500/10 px-2 py-0.5 text-[10px] text-cyan-200/90';
const mutedChipClass = 'rounded-full border border-white/5 bg-white/2 px-2 py-0.5 text-[10px] text-gray-600';

const LITERAL_UNKNOWN_STATUS = new Set(['unknown', '未知', '']);

// 状态取值来自刮削源，别名（publishing、已完结…）与自定义值都可能出现：
// 认得出的归一到 status.* 词条，认不出的原样显示——不谎报成「未知」。
function statusChipLabel(value: string, t: TFunc): string {
  const code = normalizeSeriesStatus(value);
  if (code === 'unknown' && !LITERAL_UNKNOWN_STATUS.has(value.trim().toLowerCase())) {
    return value;
  }
  return t(`status.${code}`);
}

function buildFilterChips(filter: SmartFilter, t: TFunc): { key: string; label: string }[] {
  const chips: { key: string; label: string }[] = [];
  if (filter.activeTag) chips.push({ key: 'tag', label: `#${filter.activeTag}` });
  if (filter.activeAuthor) chips.push({ key: 'author', label: `@${filter.activeAuthor}` });
  if (filter.activeStatus) chips.push({ key: 'status', label: statusChipLabel(filter.activeStatus, t) });
  if (filter.activeLetter) chips.push({ key: 'letter', label: filter.activeLetter });
  if (filter.readState) chips.push({ key: 'readState', label: t(`collections.smartChip.read.${filter.readState}`) });
  if (filter.minRating != null || filter.maxRating != null) {
    chips.push({ key: 'rating', label: `★ ${buildBoundedRangeLabel(filter.minRating, filter.maxRating)}` });
  }
  if (filter.minProgress != null || filter.maxProgress != null) {
    chips.push({ key: 'progress', label: `${t('collections.smartChip.progress')} ${buildBoundedRangeLabel(filter.minProgress, filter.maxProgress, '%')}` });
  }
  if (filter.addedWithinDays != null) {
    chips.push({ key: 'addedDays', label: t('collections.smartChip.addedWithinDays', { days: filter.addedWithinDays }) });
  }
  return chips;
}

// 排序说的是「怎么排」而不是「装了谁」，默认值区分不出书架，不值得占一个芯片。
function buildSortLabel(filter: SmartFilter, t: TFunc): string | null {
  const field = filter.sortByField || 'name';
  const desc = filter.sortDir === 'desc';
  if (field === 'name' && !desc) return null;
  return `${t(`collections.smartChip.sort.${field}`)} ${desc ? '↓' : '↑'}`;
}

export function SmartFilterChips({ filter, t }: { filter?: SmartFilter | null; t: TFunc }) {
  const chips = filter ? buildFilterChips(filter, t) : [];
  const sortLabel = filter ? buildSortLabel(filter, t) : null;
  const visible = chips.slice(0, MAX_VISIBLE_CHIPS);
  const folded = chips.slice(MAX_VISIBLE_CHIPS);
  return (
    <div className="mt-1 ml-6 flex flex-wrap items-center gap-1">
      {chips.length === 0 && <span className={mutedChipClass}>{t('collections.smartChip.noFilter')}</span>}
      {visible.map((chip) => (
        <span key={chip.key} className={chipClass}>{chip.label}</span>
      ))}
      {folded.length > 0 && (
        <span className={chipClass} title={folded.map((chip) => chip.label).join(' · ')}>
          {t('collections.smartChip.more', { count: folded.length })}
        </span>
      )}
      {sortLabel && <span className={mutedChipClass}>{sortLabel}</span>}
    </div>
  );
}

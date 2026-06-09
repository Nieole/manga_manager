/**
 * 业务说明：本文件是业务实现，属于前端国际化资源层，负责维护中文、英文等界面文案和业务状态描述。
 * 它把后端状态、前端操作和领域术语转换为用户可理解的本地化文本。
 * 维护时应保证 key 稳定、占位符一致、业务术语统一，并避免修改造成页面缺文案。
 */

export type SeriesStatusCode = 'ongoing' | 'completed' | 'hiatus' | 'cancelled' | 'unknown';

const statusAliases: Record<string, SeriesStatusCode> = {
  ongoing: 'ongoing',
  publishing: 'ongoing',
  serializing: 'ongoing',
  completed: 'completed',
  complete: 'completed',
  finished: 'completed',
  hiatus: 'hiatus',
  paused: 'hiatus',
  cancelled: 'cancelled',
  canceled: 'cancelled',
  dropped: 'cancelled',
  unknown: 'unknown',
  '连载中': 'ongoing',
  '已完结': 'completed',
  '休刊中': 'hiatus',
  '已放弃': 'cancelled',
  '已取消': 'cancelled',
  '有生之年': 'hiatus',
  '未知': 'unknown',
  '': 'unknown',
};

export function normalizeSeriesStatus(value?: string | null): SeriesStatusCode {
  const normalized = String(value ?? '').trim().toLowerCase();
  return statusAliases[normalized] ?? statusAliases[String(value ?? '').trim()] ?? 'unknown';
}

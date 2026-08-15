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

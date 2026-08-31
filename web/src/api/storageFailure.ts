/**
 * 本文件把后端「读不到书的字节」的失败分类翻成用户照着能做事的一句话。
 *
 * 分类、状态码与载荷字段由 internal/api/storage_diagnosis.go 定义，类型经 cmd/tsgen 生成到
 * generated.ts。这里只做两件事：从一次失败的请求里认出这种响应，以及按 reason 选词条。
 * 认不出时返回 null，调用方回退到自己原有的错误文案——旧端点仍只发 { error }。
 */

import { isAxiosError } from './client';
import type { StorageFailureResponse } from './generated';

export type StorageFailureReason =
  | 'storage_offline'
  | 'file_missing'
  | 'path_unreadable'
  | 'archive_unreadable';

type Translate = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

const REASON_KEYS: Record<StorageFailureReason, string> = {
  storage_offline: 'storage.error.offline',
  file_missing: 'storage.error.fileMissing',
  path_unreadable: 'storage.error.pathUnreadable',
  archive_unreadable: 'storage.error.archiveUnreadable',
};

function isStorageFailureBody(data: unknown): data is StorageFailureResponse {
  if (!data || typeof data !== 'object') return false;
  const reason = (data as { reason?: unknown }).reason;
  return typeof reason === 'string' && reason in REASON_KEYS;
}

/** storageFailureFromError 从一次失败的请求里取出存储失败分类；不是这一类则返回 null。 */
export function storageFailureFromError(error: unknown): StorageFailureResponse | null {
  if (!isAxiosError(error)) return null;
  const data: unknown = error.response?.data;
  return isStorageFailureBody(data) ? data : null;
}

/**
 * storageFailureMessage 按分类给出提示。
 *
 * 存储离线是唯一需要指名道姓的一类——用户要知道该把哪块盘插回去，所以缺了资料库名时
 * 退到不带名字的说法，而不是印一个空引号。
 */
export function storageFailureMessage(failure: StorageFailureResponse, t: Translate): string {
  if (failure.reason === 'storage_offline') {
    return failure.library_name
      ? t('storage.error.offline', { name: failure.library_name, path: failure.library_path })
      : t('storage.error.offlineUnnamed');
  }
  const key = REASON_KEYS[failure.reason as StorageFailureReason];
  return key ? t(key, { path: failure.path }) : failure.error;
}

/** storageFailureMessageFromError 是上面两步的合流：不是存储失败就返回 null。 */
export function storageFailureMessageFromError(error: unknown, t: Translate): string | null {
  const failure = storageFailureFromError(error);
  return failure ? storageFailureMessage(failure, t) : null;
}

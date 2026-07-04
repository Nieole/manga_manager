/**
 * 业务说明：本文件是业务实现，属于前端国际化资源层，负责维护中文、英文等界面文案和业务状态描述。
 * 它把后端状态、前端操作和领域术语转换为用户可理解的本地化文本。
 * 维护时应保证 key 稳定、占位符一致、业务术语统一，并避免修改造成页面缺文案。
 */

type Translator = (key: string, params?: Record<string, string | number | boolean | null | undefined>, defaultValue?: string) => string;

export interface TaskWithParams {
  type: string;
  params?: Record<string, string>;
}

export function formatKOReaderIndexLabel(params: Record<string, string> | undefined, t: Translator) {
  if (params?.match_mode === 'file_path') {
    return params.path_ignore_extension === 'true'
      ? t('task.koreader.pathIndexIgnoreExtension')
      : t('task.koreader.pathIndex');
  }
  return t('task.koreader.binaryHashIndex');
}

export function getTaskTypeLabel(task: TaskWithParams, t: Translator) {
  switch (task.type) {
    case 'rebuild_book_hashes':
      return t('task.type.rebuild_book_hashes', { label: formatKOReaderIndexLabel(task.params, t) });
    default:
      return t(`task.type.${task.type}`);
  }
}

export function getTaskActionHint(task: TaskWithParams, t: Translator) {
  switch (task.type) {
    case 'rebuild_book_hashes':
      return t('task.hint.rebuild_book_hashes', { label: formatKOReaderIndexLabel(task.params, t) });
    case 'reconcile_koreader_progress':
      return t('task.hint.reconcile_koreader_progress', { label: formatKOReaderIndexLabel(task.params, t) });
    case 'refresh_koreader_matching':
      return t('task.hint.refresh_koreader_matching', { label: formatKOReaderIndexLabel(task.params, t) });
    case 'scan_library':
    case 'scan_external_library':
    case 'rebuild_index':
    case 'rebuild_thumbnails':
    case 'cleanup_thumbnails':
    case 'rebuild_file_identities':
    case 'scrape':
    case 'ai_grouping':
    case 'transfer_external_library':
      return t(`task.hint.${task.type}`);
    default:
      return t('task.hint.default');
  }
}

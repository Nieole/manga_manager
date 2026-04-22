type Translator = (key: string, params?: Record<string, string | number | boolean | null | undefined>) => string;

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
    case 'scrape':
    case 'ai_grouping':
    case 'transfer_external_library':
      return t(`task.hint.${task.type}`);
    default:
      return t('task.hint.default');
  }
}

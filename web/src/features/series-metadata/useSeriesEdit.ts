import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient, getApiErrorMessage, isAxiosError } from '../../api/client';
import { useLibraryInvalidation } from '../../session/ApplicationSessionProvider';
import type { Author, MetaTag, Series, SeriesLink } from './types';
import type { SeriesEditForm, SeriesFormField, SeriesFormValue } from './draft';
export type { SeriesEditForm, SeriesFormField, SeriesFormValue } from './draft';

interface UseSeriesEditParams {
  seriesId: string | undefined;
  series: Series | null;
  tags: MetaTag[];
  authors: Author[];
  links: SeriesLink[];
  metadataVersion: string | null;
  reload: (requireFresh?: boolean) => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesEdit({ seriesId, series, tags, authors, links, metadataVersion, reload, showToast, t }: UseSeriesEditParams) {
  const invalidateLibrary = useLibraryInvalidation();
  const activeScope = useRef({ seriesId });
  if (activeScope.current.seriesId !== seriesId) activeScope.current = { seriesId };
  const scope = activeScope.current;
  useEffect(() => {
    activeScope.current = scope;
    return () => { if (activeScope.current === scope) activeScope.current = { seriesId: undefined }; };
  }, [scope]);
  const [isEditing, setIsEditing] = useState(false);
  const [editForm, setEditForm] = useState<SeriesEditForm>({});
  const [lockedFields, setLockedFields] = useState<Set<string>>(new Set());
  const [allTags, setAllTags] = useState<MetaTag[]>([]);
  const [allAuthors, setAllAuthors] = useState<Author[]>([]);
  // hasConflict：最近一次保存被服务端以「编辑期间有人改过」为由拒绝。置上后表单原样留着，
  // 用户确认后再点一次保存即以自己这份为准。
  const [hasConflict, setHasConflict] = useState(false);

  // 表单里这份内容属于哪个系列。只能记在 ref 里：effect 读状态拿到的是上一轮渲染的闭包值。
  const formSeriesIdRef = useRef<number | null>(null);
  // 表单这份内容是从哪个版本的服务端数据长出来的，保存时带回去做冲突检测。
  // 它必须与表单内容同进同出：编辑期间的后台刷新不重置表单，也就不能把基线偷偷换成新版本——
  // 换了就等于自动认领别人的改动，静默覆盖原样复发。
  const formVersionRef = useRef<string | null>(null);

  // 表单重置：非编辑态跟随服务端最新值，编辑态只在换了系列时才跟。
  // 两条判据缺一不可——只看 seriesId，任一后台任务完成引发的静默重取都会换掉 series/tags/
  // authors/links 的引用（内容可以一模一样），把用户没保存的输入顶掉；只看 isEditing，
  // 编辑期间切到别的系列就会把上一个系列的内容留在框里，保存时打到新系列头上。
  useEffect(() => {
    if (!series) return;
    if (isEditing && formSeriesIdRef.current === series.id) return;
    formSeriesIdRef.current = series.id;
    formVersionRef.current = metadataVersion;
    setHasConflict(false);
    setLockedFields(new Set(series.locked_fields?.Valid && series.locked_fields.String ? series.locked_fields.String.split(',') : []));
    setEditForm({
      id: series.id,
      title: series.title?.String ?? '',
      summary: series.summary?.String ?? '',
      publisher: series.publisher?.String ?? '',
      status: series.status?.String ?? '',
      rating: series.rating?.Float64 ?? 0,
      language: series.language?.String ?? '',
      tagsInput: tags.map((tag) => tag.name),
      authorsInput: authors.map((author) => ({ name: author.name, role: author.role })),
      linksInput: links.map((link) => ({ name: link.name, url: link.url })),
    });
  }, [series, tags, authors, links, metadataVersion, isEditing]);

  // 进入编辑时再加载全量 tags / authors
  useEffect(() => {
    if (!isEditing) return;
    Promise.all([
      apiClient.get<MetaTag[]>('/api/tags/all').catch(() => ({ data: [] as MetaTag[] })),
      apiClient.get<Author[]>('/api/authors/all').catch(() => ({ data: [] as Author[] })),
    ]).then(([tagsRes, authorsRes]) => {
      setAllTags(Array.isArray(tagsRes.data) ? tagsRes.data : []);
      setAllAuthors(Array.isArray(authorsRes.data) ? authorsRes.data : []);
    });
  }, [isEditing]);

  const toggleLock = useCallback((field: string) => {
    setLockedFields((prev) => {
      const next = new Set(prev);
      if (next.has(field)) next.delete(field);
      else next.add(field);
      return next;
    });
  }, []);

  const onFormChange = useCallback((field: SeriesFormField, value: SeriesFormValue) => {
    setEditForm((prev) => {
      const next: SeriesEditForm = { ...prev };
      if (field === 'rating') {
        next.rating = Number(value);
      } else if (field === 'tagsInput' && Array.isArray(value)) {
        next.tagsInput = value as string[];
      } else if (field === 'authorsInput' && Array.isArray(value)) {
        next.authorsInput = value as { name: string; role: string }[];
      } else if (field === 'linksInput' && Array.isArray(value)) {
        next.linksInput = value as { name: string; url: string }[];
      } else {
        const stringValue = String(value);
        if (field === 'title' || field === 'summary' || field === 'publisher' || field === 'status' || field === 'language') {
          next[field] = stringValue;
        }
      }
      return next;
    });
    setLockedFields((prev) => {
      const next = new Set(prev);
      const lockField = field === 'tagsInput' ? 'tags' : field === 'authorsInput' ? 'authors' : field;
      next.add(lockField);
      return next;
    });
  }, []);

  const save = useCallback(async () => {
    if (!series || !seriesId || series.id !== Number(seriesId)) return;
    // 保存必须携带草稿的版本；没有基线时不能发起无保护的全量覆盖。
    const expectedVersion = formVersionRef.current;
    if (!expectedVersion) {
      showToast(t('series.toast.saveNoVersion'), 'error');
      return;
    }
    try {
      await apiClient.put(`/api/series/info/${seriesId}`, {
        title: editForm.title,
        summary: editForm.summary,
        publisher: editForm.publisher,
        status: editForm.status,
        rating: editForm.rating,
        language: editForm.language,
        locked_fields: Array.from(lockedFields).join(','),
        tags: editForm.tagsInput,
        authors: editForm.authorsInput,
        links: editForm.linksInput,
        expected_version: expectedVersion,
      });
      try {
        if (activeScope.current !== scope) return;
        setHasConflict(false);
        setIsEditing(false);
        try {
          await reload(true);
        } catch {
          if (activeScope.current === scope) showToast(t('series.toast.savedRefreshFailed'), 'error');
        }
      } finally {
        // Publish even after refresh failure or a target switch, but never supersede our awaited refresh.
        invalidateLibrary({ areas: ['catalog', 'collections', 'organize', 'stats', 'reading-lists'] });
      }
    } catch (err) {
      if (activeScope.current !== scope) return;
      // 409：编辑期间服务端被别的途径写过（刮削应用了提案、另一个标签页保存、另一个用户改了）。
      // 弹窗与用户敲进去的内容一律不动——保存没成功，输入就不该消失；把服务端给的最新版本记为
      // 新基线，用户看过提示后再点一次保存即以自己这份为准。
      if (isAxiosError(err) && err.response?.status === 409) {
        const current = err.response?.data?.current_version;
        if (typeof current === 'string' && current) formVersionRef.current = current;
        setHasConflict(true);
        showToast(t('series.toast.saveConflict'), 'error');
        return;
      }
      console.error('Failed to update metadata', err);
      showToast(`${t('series.toast.saveFailed')}: ${getApiErrorMessage(err, t('series.toast.saveFailed'))}`, 'error');
    }
  }, [series, seriesId, editForm, lockedFields, reload, showToast, t, scope, invalidateLibrary]);

  return {
    isEditing,
    setIsEditing,
    editForm,
    lockedFields,
    allTags,
    allAuthors,
    hasConflict,
    toggleLock,
    onFormChange,
    save,
  };
}

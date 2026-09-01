import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient } from '../../../api/client';
import { getApiErrorMessage, isAxiosError } from '../../../api/client';
import type { Author, MetaTag, Series, SeriesLink } from '../types';


export type SeriesEditForm = Partial<Series> & {
  tagsInput?: string[];
  authorsInput?: { name: string; role: string }[];
  linksInput?: { name: string; url: string }[];
};

export type SeriesFormField = 'title' | 'summary' | 'publisher' | 'status' | 'rating' | 'language' | 'tagsInput' | 'authorsInput' | 'linksInput';
export type SeriesFormValue = string | number | string[] | { name: string; role: string }[] | { name: string; url: string }[];

interface UseSeriesEditParams {
  seriesId: string | undefined;
  series: Series | null;
  tags: MetaTag[];
  authors: Author[];
  links: SeriesLink[];
  metadataVersion: string | null;
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesEdit({ seriesId, series, tags, authors, links, metadataVersion, reload, showToast, t }: UseSeriesEditParams) {
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
      title: series.title,
      summary: series.summary,
      publisher: series.publisher,
      status: series.status,
      rating: series.rating,
      language: series.language,
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
        next.rating = { Float64: Number(value), Valid: Number(value) > 0 };
      } else if (field === 'tagsInput' && Array.isArray(value)) {
        next.tagsInput = value as string[];
      } else if (field === 'authorsInput' && Array.isArray(value)) {
        next.authorsInput = value as { name: string; role: string }[];
      } else if (field === 'linksInput' && Array.isArray(value)) {
        next.linksInput = value as { name: string; url: string }[];
      } else {
        const stringValue = String(value);
        if (field === 'title' || field === 'summary' || field === 'publisher' || field === 'status' || field === 'language') {
          next[field] = { String: stringValue, Valid: stringValue.trim() !== '' };
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
    if (!series || !seriesId) return;
    // 没拿到版本就不保存。带空串发出去，服务端只能当成「调用方不参与并发控制」而跳过校验，
    // 「有人改过了」的提示再也不会出现——用户以为自己受着保护，后写的却在静默覆盖先写的。
    const expectedVersion = formVersionRef.current;
    if (!expectedVersion) {
      showToast(t('series.toast.saveNoVersion'), 'error');
      return;
    }
    try {
      await apiClient.put(`/api/series/info/${seriesId}`, {
        title: editForm.title?.String || '',
        summary: editForm.summary?.String || '',
        publisher: editForm.publisher?.String || '',
        status: editForm.status?.String || '',
        rating: editForm.rating?.Float64 || 0,
        language: editForm.language?.String || '',
        locked_fields: Array.from(lockedFields).join(','),
        tags: editForm.tagsInput || [],
        authors: editForm.authorsInput || [],
        links: editForm.linksInput || [],
        expected_version: expectedVersion,
      });
      setHasConflict(false);
      await reload();
      setIsEditing(false);
    } catch (err) {
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
  }, [series, seriesId, editForm, lockedFields, reload, showToast, t]);

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

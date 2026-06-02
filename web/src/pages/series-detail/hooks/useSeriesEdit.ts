import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
import type { Author, MetaTag, Series, SeriesLink } from '../types';

function getApiErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError(error)) return error.response?.data?.error || error.message || fallback;
  if (error instanceof Error) return error.message;
  return fallback;
}

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
  reload: () => Promise<void>;
  showToast: (message: string, level: 'success' | 'error') => void;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export function useSeriesEdit({ seriesId, series, tags, authors, links, reload, showToast, t }: UseSeriesEditParams) {
  const [isEditing, setIsEditing] = useState(false);
  const [editForm, setEditForm] = useState<SeriesEditForm>({});
  const [lockedFields, setLockedFields] = useState<Set<string>>(new Set());
  const [allTags, setAllTags] = useState<MetaTag[]>([]);
  const [allAuthors, setAllAuthors] = useState<Author[]>([]);

  // 当 series / tags / authors / links 变更时重置表单
  useEffect(() => {
    if (!series) return;
     
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
  }, [series, tags, authors, links]);

  // 进入编辑时再加载全量 tags / authors
  useEffect(() => {
    if (!isEditing) return;
    Promise.all([
      axios.get<MetaTag[]>('/api/tags/all').catch(() => ({ data: [] as MetaTag[] })),
      axios.get<Author[]>('/api/authors/all').catch(() => ({ data: [] as Author[] })),
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
    try {
      await axios.put(`/api/series/info/${seriesId}`, {
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
      });
      await reload();
      setIsEditing(false);
    } catch (err) {
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
    toggleLock,
    onFormChange,
    save,
  };
}

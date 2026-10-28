import { useEffect, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import { useToast } from '../../components/ToastProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { useLibraryInvalidation, useLibraryRefresh } from '../../session/ApplicationSessionProvider';
import { useLatestRequest } from '../../hooks/useLatestRequest';
import { DEFAULT_SCAN_FORMATS, DEFAULT_SCAN_INTERVAL } from './constants';
import { libraryFormPayload, useLibraryForm, type LibraryFormValues } from './useLibraryForm';
import { useDirectoryBrowser } from './useDirectoryBrowser';
import type { Library } from './types';

const EMPTY: LibraryFormValues = { name: '', path: '', scanMode: 'none', koreaderSyncEnabled: true, scanInterval: DEFAULT_SCAN_INTERVAL, scanFormats: DEFAULT_SCAN_FORMATS };
interface SaveResult { saved: boolean; warnings?: string[]; initial_scan?: { admitted: boolean; run_id?: number; status?: string } }

export function useLibraryManagement(isAdmin: boolean, onRelocation: (id: string, path: string) => void) {
  const { t } = useI18n();
  const { showToast } = useToast();
  const invalidate = useLibraryInvalidation();
  const refresh = useLibraryRefresh();
  const navigation = useLatestRequest();
  const [libraries, setLibraries] = useState<Library[]>([]);
  const [loading, setLoading] = useState(true);
  const [librariesError, setLibrariesError] = useState(false);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showEditModal, setShowEditModal] = useState(false);
  const [editLibId, setEditLibId] = useState('');
  const [supportedScanFormats, setSupportedScanFormats] = useState(DEFAULT_SCAN_FORMATS);
  const [recentLibraryPaths, setRecentLibraryPaths] = useState<string[]>(() => {
    try { const saved: unknown = JSON.parse(localStorage.getItem('manga_manager_recent_library_paths') ?? '[]'); return Array.isArray(saved) ? saved.filter((v): v is string => typeof v === 'string') : []; } catch { return []; }
  });
  const addForm = useLibraryForm(EMPTY);
  const editForm = useLibraryForm(EMPTY);
  const directory = useDirectoryBrowser();

  const fetchLibraries = async () => {
    setLoading(true); setLibrariesError(false);
    let failed = false;
    await navigation.run(() => apiClient.get<Library[]>('/api/libraries'), {
      onSuccess: result => setLibraries(result.data),
      onError: () => { failed = true; setLibrariesError(true); },
      onSettled: () => setLoading(false),
    });
    return !failed;
  };
  useEffect(() => {
    void fetchLibraries();
    return () => navigation.invalidate();
  // Navigation refresh does not reset forms or the directory browser.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refresh, navigation]);
  useEffect(() => {
    if (isAdmin) void apiClient.get('/api/system/capabilities').then(response => {
      const formats = response.data?.default_scan_formats;
      if (formats) { setSupportedScanFormats(formats); addForm.setScanFormats(formats); editForm.setScanFormats(formats); }
    }).catch(() => {});
  // Defaults are loaded once per authenticated role; user drafts are preserved afterwards.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdmin]);

  const openEditLibraryModal = (id: string | number) => {
    const target = libraries.find(lib => String(lib.id) === String(id));
    if (!target) return;
    setEditLibId(String(target.id));
    editForm.reset({name: target.name || '', path: target.path || '', scanMode: target.scan_mode || 'none', koreaderSyncEnabled: target.koreader_sync_enabled ?? true, scanInterval: target.scan_interval || DEFAULT_SCAN_INTERVAL, scanFormats: target.scan_formats || supportedScanFormats});
    setShowEditModal(true);
  };
  const rememberPath = (path: string) => {
    const clean = path.trim(); if (!clean) return;
    const next = [clean, ...recentLibraryPaths.filter(value => value !== clean)].slice(0, 5);
    setRecentLibraryPaths(next);
    try { localStorage.setItem('manga_manager_recent_library_paths', JSON.stringify(next)); } catch { /* The save remains committed if local preferences cannot be written. */ }
  };
  const errorMessage = (error: unknown, fallback: string) => {
    if (!isAxiosError(error)) return fallback;
    if (error.response?.data?.code === 'library_unavailable') return t('librarySave.unavailable');
    const issues = error.response?.data?.validation?.issues;
    return Array.isArray(issues) && issues.length ? issues.map((issue: {field: string; message: string}) => `${issue.field}: ${issue.message}`).join('\n') : error.response?.data?.error || fallback;
  };
  const afterSave = async (result: SaveResult, path: string) => {
    rememberPath(path); invalidate();
    const refreshed = await fetchLibraries();
    if (!refreshed) showToast(t('librarySave.refreshFailed'), 'error');
    else if (result.warnings?.length) showToast(t('librarySave.savedWarning'), 'error');
  };
  const handleAddLibrary = async (event: React.FormEvent) => {
    event.preventDefault(); addForm.setSubmitting(true);
    try {
      const response = await apiClient.post<SaveResult>('/api/libraries', libraryFormPayload(addForm.values));
      if (!response.data.saved) throw new Error('Library was not saved');
      setShowAddModal(false); addForm.reset(EMPTY);
      await afterSave(response.data, addForm.values.path);
    } catch (error) { showToast(errorMessage(error, t('layout.toast.addLibraryFailed')), 'error'); }
    finally { addForm.setSubmitting(false); }
  };
  const handleEditLibrarySubmit = async (event: React.FormEvent) => {
    event.preventDefault(); editForm.setSubmitting(true);
    try {
      const response = await apiClient.put<SaveResult>(`/api/libraries/${editLibId}`, libraryFormPayload(editForm.values));
      if (!response.data.saved) throw new Error('Library was not saved');
      setShowEditModal(false); await afterSave(response.data, editForm.values.path);
    } catch (error) {
      if (isAxiosError(error) && error.response?.data?.code === 'relocation_required') { setShowEditModal(false); onRelocation(editLibId, editForm.values.path); showToast(t('relocation.editRedirect')); }
      else showToast(isAxiosError(error) && error.response?.data?.code === 'relocation_protected' ? t('relocation.protection.explanation') : errorMessage(error, t('layout.toast.editLibraryFailed')), 'error');
    } finally { editForm.setSubmitting(false); }
  };
  return { libraries, loading, librariesError, fetchLibraries, showAddModal, setShowAddModal, showEditModal, setShowEditModal, addForm, editForm, supportedScanFormats, recentLibraryPaths, openEditLibraryModal, handleAddLibrary, handleEditLibrarySubmit, ...directory };
}

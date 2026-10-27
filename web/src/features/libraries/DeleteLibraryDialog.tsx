import { useEffect, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import { useLatestRequest } from '../../hooks/useLatestRequest';
import { useI18n } from '../../i18n/LocaleProvider';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';

interface LibraryTarget { id: string | number; name: string }
interface Conflict {
  run_id?: number;
  library_ids?: number[];
  library_id?: number;
  scope?: string;
  status?: string;
  reason?: string;
}
interface DeleteResponse { deleted?: boolean; status?: string; warnings?: string[] }
interface DeleteFailure { code?: string; conflicts?: Conflict[]; warnings?: string[] }

export function DeleteLibraryDialog({ library, onClose, onDeleted }: {
  library: LibraryTarget | null;
  onClose: () => void;
  onDeleted: (library: LibraryTarget, warnings: string[]) => void;
}) {
  const { t } = useI18n();
  const request = useLatestRequest();
  const [deleting, setDeleting] = useState(false);
  const [conflicts, setConflicts] = useState<Conflict[]>([]);
  const [failure, setFailure] = useState('');
  const [restoreFailed, setRestoreFailed] = useState(false);
  useEffect(() => {
    setDeleting(false);
    setConflicts([]);
    setFailure('');
    setRestoreFailed(false);
    return () => request.invalidate();
  }, [library?.id, request]);

  const confirm = () => {
    if (!library) return;
    setDeleting(true);
    setFailure('');
    setConflicts([]);
    setRestoreFailed(false);
    void request.run(() => apiClient.delete<DeleteResponse>(`/api/libraries/${library.id}`), {
      onSuccess: response => {
        if (response.data.deleted || response.data.status === 'deleted') onDeleted(library, response.data.warnings ?? []);
        else setFailure('layout.toast.deleteFailed');
      },
      onError: error => {
        const data = isAxiosError<DeleteFailure>(error) ? error.response?.data : undefined;
        setFailure(data?.code === 'library_busy' ? 'libraryDelete.busy' : 'layout.toast.deleteFailed');
        setConflicts(Array.isArray(data?.conflicts) ? data.conflicts : []);
        setRestoreFailed(data?.warnings?.includes('watcher_restore_failed') ?? false);
      },
      onSettled: () => setDeleting(false),
    });
  };
  const conflictText = (conflict: Conflict) => {
    if (conflict.reason === 'wait_for_reads') return t('libraryDelete.readers');
    if (conflict.reason === 'exclusive_access') return t('libraryDelete.reserved');
    if (conflict.reason === 'wait_for_exit') return t('libraryDelete.finishing', { id: conflict.run_id ?? '' });
    const shared = conflict.scope === 'system' || conflict.library_ids?.some(id => id !== Number(library?.id));
    if (shared) return t('libraryDelete.sharedWork', { id: conflict.run_id ?? '' });
    return t(conflict.status === 'queued' ? 'libraryDelete.queuedWork' : 'libraryDelete.localWork', { id: conflict.run_id ?? '' });
  };
  return <ConfirmDialog open={library !== null} onClose={onClose} onConfirm={confirm} loading={deleting}
    title={t('layout.libraryActions.confirmDeleteTitle')}
    description={t('layout.libraryActions.confirmDeleteDescription', { name: library?.name ?? '' })}
    confirmLabel={t('layout.libraryActions.confirmDelete')} tone="danger">
    {failure && <div role="alert" className="text-sm text-amber-200">
      <p>{t(failure)}</p>
      {conflicts.length > 0 && <ul className="mt-2 list-disc pl-4">{conflicts.map((conflict, index) => <li key={`${conflict.run_id ?? 0}:${index}`}>{conflictText(conflict)}</li>)}</ul>}
      {restoreFailed && <p className="mt-2">{t('libraryDelete.restoreFailed')}</p>}
    </div>}
  </ConfirmDialog>;
}

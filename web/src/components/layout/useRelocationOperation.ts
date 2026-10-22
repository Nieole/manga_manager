import { useEffect, useRef } from 'react';
import { apiClient } from '../../api/client';
import type { RelocationOperation, RunSnapshot } from '../../api/generated';

interface Options {
  libraryID: number;
  operationID?: number;
  status?: string;
  onOperation: (operation: RelocationOperation) => boolean;
  loadProgress?: (operation: RelocationOperation) => Promise<RunSnapshot | null>;
  onProgress?: (run: RunSnapshot | null) => void;
  onCompleted?: () => void;
  onError: () => void;
}

const pending = (status: string) => ['pending', 'prepared', 'paths_committed'].includes(status);

export function useRelocationOperation({ libraryID, operationID, status, ...callbacks }: Options) {
  const latest = useRef(callbacks);
  useEffect(() => { latest.current = callbacks; });

  useEffect(() => {
    if (!operationID || (status && !pending(status))) return;
    let active = true;
    let timer: number | undefined;
    const refresh = async () => {
      try {
        const { data } = await apiClient.get<RelocationOperation>(`/api/relocation-operations/${operationID}`);
        if (!active || !latest.current.onOperation(data)) return;
        if (pending(data.status)) {
          try {
            if (latest.current.loadProgress) {
              const progress = await latest.current.loadProgress(data);
              if (active) latest.current.onProgress?.(progress);
            }
          } catch { /* Persisted results remain available without live progress. */ }
          if (active) timer = window.setTimeout(refresh, 1000);
        } else if (data.status === 'completed') latest.current.onCompleted?.();
      } catch { if (active) latest.current.onError(); }
    };
    void refresh();
    return () => { active = false; window.clearTimeout(timer); };
  }, [libraryID, operationID, status]);
}

import { useCallback } from 'react';
import { apiClient, getApiErrorMessage, isAxiosError } from '../../api/client';
import { useLibraryInvalidation } from '../../session/ApplicationSessionProvider';

export interface SelectionEdit {
  seriesIds: number[];
  addTags: string[];
  removeTags: string[];
  status?: string;
  publisher?: string;
}

interface SelectionResponse {
  updated?: number;
  committed?: boolean;
  committed_series_ids?: number[];
  refresh_failed_series_ids?: number[];
}

export interface SelectionOutcome {
  committed: boolean;
  updated: number;
  refreshFailed: boolean;
  laterError?: string;
}

/** Selection edits declare only effects justified by a committed response. */
export function useBulkSeriesEdit() {
  const invalidateLibrary = useLibraryInvalidation();
  return useCallback(async (input: SelectionEdit): Promise<SelectionOutcome> => {
    let data: SelectionResponse;
    let laterError: string | undefined;
    try {
      const response = await apiClient.post<SelectionResponse>('/api/series/bulk-edit', {
        series_ids: input.seriesIds,
        add_tags: input.addTags,
        remove_tags: input.removeTags,
        status: input.status,
        publisher: input.publisher,
      });
      data = response.data;
    } catch (err) {
      const response = isAxiosError<SelectionResponse>(err) ? err.response?.data : undefined;
      if (!response || (!response.committed && !response.committed_series_ids?.length)) throw err;
      data = response;
      laterError = getApiErrorMessage(err, '');
    }
    const knownIDs = data.committed_series_ids;
    const updated = Array.isArray(knownIDs) ? knownIDs.length : (data.updated ?? 0);
    // Responses without an explicit commit flag retain compatibility with older servers.
    const committed = data.committed === true || !!knownIDs?.length || (data.committed === undefined && updated > 0);
    if (committed) invalidateLibrary({ areas: ['catalog', 'collections', 'organize', 'stats', 'reading-lists'] });
    return { committed, updated, refreshFailed: !!data.refresh_failed_series_ids?.length, laterError };
  }, [invalidateLibrary]);
}

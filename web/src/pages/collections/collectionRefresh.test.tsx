/** @vitest-environment jsdom */
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import { apiClient } from '../../api/client';
import { useCollectionSeries } from './useCollectionSeries';
import type { Collection } from './types';
const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; });
it('refreshes the loaded smart shelf without changing its continuation page size', async () => {
  const requests: Array<{ limit: number; offset: number }> = [];
  apiClient.defaults.adapter = async config => {
    const limit = Number(config.params?.limit ?? 2); const offset = Number(config.params?.offset ?? 0);
    requests.push({ limit, offset });
    const data = { items: Array.from({ length: 5 }, (_, index) => ({ id: index + 1, name: `Series ${index + 1}`, actual_book_count: 1 })).slice(offset, offset + limit), total: 5, limit };
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const collection: Collection = { id: 1, numeric_id: 1, view_id: 'smart:1', kind: 'smart', name: 'Smart shelf', description: '', series_count: 5, source_type: 'smart', created_at: '' };
  const onFilter = () => {};
  const view = renderHook(() => useCollectionSeries(onFilter));
  act(() => view.result.current.open(collection));
  await waitFor(() => expect(view.result.current.items).toHaveLength(2));
  act(() => view.result.current.loadMore());
  await waitFor(() => expect(view.result.current.items).toHaveLength(4));
  act(() => view.result.current.refresh(collection));
  await waitFor(() => expect(view.result.current.loading).toBe(false));
  expect(view.result.current.items).toHaveLength(4);
  expect(requests.at(-1)).toEqual({ limit: 4, offset: 0 });
  act(() => view.result.current.loadMore());
  await waitFor(() => expect(view.result.current.items).toHaveLength(5));
  expect(requests.at(-1)).toEqual({ limit: 2, offset: 4 });
});

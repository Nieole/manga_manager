/** @vitest-environment jsdom */
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { apiClient } from '../../../api/client';
import { ApplicationSessionProvider } from '../../../session/ApplicationSessionProvider';
import { EMPTY_ADVANCED_FILTERS } from './libraryFilterParams';
import { useLibrarySeries } from './useLibrarySeries';

const COMPLETED_FILTERS = { ...EMPTY_ADVANCED_FILTERS, readState: 'completed' };

class Source {
  static CLOSED = 2;
  readyState = 0;
  close() {}
}

afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function Catalogue({ refresh = 0 }: { refresh?: number }) {
  const [selection, setSelection] = useState('');
  const view = useLibrarySeries({
    libId: 'reader-cache', page: 1, pageSize: 10, activeTag: null, activeAuthor: null,
    activeStatus: null, activeLetter: null, advanced: COMPLETED_FILTERS,
    sortByField: 'name', sortDir: 'asc', refreshTrigger: refresh, enabled: true,
  });
  return <><output>{view.allSeries.map(item => item.name).join(',')}</output><input aria-label="selection" value={selection} onChange={event => setSelection(event.target.value)} /></>;
}

it('does not reuse another reader response for the same catalogue query', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', Source);
  let name = 'reader one';
  vi.spyOn(apiClient, 'get').mockImplementation(async () => ({ data: { items: [{ id: 1, name }], total: 1 } }));
  const view = render(<ApplicationSessionProvider identity="one:regular" isAdmin={false}><Catalogue /></ApplicationSessionProvider>);
  await act(async () => { await Promise.resolve(); });
  expect(screen.getByText('reader one')).toBeTruthy();
  name = 'reader two';
  view.rerender(<ApplicationSessionProvider identity="two:regular" isAdmin={false}><Catalogue /></ApplicationSessionProvider>);
  await act(async () => { await Promise.resolve(); });
  expect(screen.getByText('reader two')).toBeTruthy();
});

it('refreshes completed-series facts within the dedup window while keeping selection', async () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', Source);
  let name = 'old completed series';
  vi.spyOn(apiClient, 'get').mockImplementation(async () => ({ data: { items: [{ id: 1, name }], total: 1 } }));
  const view = render(<ApplicationSessionProvider identity="refresh:regular" isAdmin={false}><Catalogue /></ApplicationSessionProvider>);
  await act(async () => { await Promise.resolve(); });
  fireEvent.change(screen.getByLabelText('selection'), { target: { value: 'keep selected' } });
  name = 'new completed series';
  view.rerender(<ApplicationSessionProvider identity="refresh:regular" isAdmin={false}><Catalogue refresh={1} /></ApplicationSessionProvider>);
  await act(async () => { await Promise.resolve(); });
  expect(screen.getByText('new completed series')).toBeTruthy();
  expect((screen.getByLabelText('selection') as HTMLInputElement).value).toBe('keep selected');
  const params = new URLSearchParams(vi.mocked(apiClient.get).mock.calls.at(-1)?.[0].split('?')[1]);
  expect(params.get('readState')).toBe('completed');
});

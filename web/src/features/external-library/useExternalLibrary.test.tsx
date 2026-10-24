/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { apiClient } from '../../api/client';
import { ApplicationSessionProvider, useLibraryRefresh } from '../../session/ApplicationSessionProvider';
import { useExternalLibrary } from './useExternalLibrary';
import type { PropsWithChildren } from 'react';
class Source {
  static CLOSED = 2;
  static current: Source;
  onmessage: ((event: { data: string }) => void) | null = null;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() { Source.current = this; }
  close() {}
}
const adapter = apiClient.defaults.adapter;
const ids = [7];
const onError = vi.fn();
function wrapper({ children }: PropsWithChildren) { return <ApplicationSessionProvider identity="1:admin" isAdmin>{children}</ApplicationSessionProvider>; }
function setup() {
  vi.stubGlobal('EventSource', Source);
  return renderHook(({ id }: { id: string }) => ({
    external: useExternalLibrary({ libId: id, allSeriesIds: ids, onError }),
    refresh: useLibraryRefresh({ libraryId: id }),
  }), { initialProps: { id: '1' }, wrapper });
}
afterEach(() => { cleanup(); apiClient.defaults.adapter = adapter; vi.unstubAllGlobals(); localStorage.clear(); onError.mockClear(); });
it('discards a scan start response after switching library and keeps device input', async () => {
  let finish: (() => void) | undefined;
  apiClient.defaults.adapter = async config => {
    await new Promise<void>(resolve => { finish = resolve; });
    return { data: { session: { session_id: 'old', library_id: 1, status: 'scanning' }, task_key: 'old' }, status: 200, statusText: 'OK', headers: {}, config };
  };
  const view = setup();
  act(() => view.result.current.external.setExternalPath('/device/input'));
  let start: Promise<void>;
  act(() => { start = view.result.current.external.startExternalLibraryScan(); });
  await waitFor(() => expect(finish).toBeDefined());
  view.rerender({ id: '2' });
  await act(async () => { finish?.(); await start; });
  expect(view.result.current.external.externalSession).toBeNull();
  expect(view.result.current.external.externalPath).toBe('/device/input');
});
it('uses the session to refresh coverage and catalog after transfer termination', async () => {
  const requests: string[] = [];
  apiClient.defaults.adapter = async config => {
    const url = config.url ?? ''; requests.push(url);
    const session = { session_id: 's1', library_id: 1, status: 'ready', external_path: '/device' };
    const data = config.method === 'post' ? url.endsWith('/transfer') ? { task_key: 'transfer' } : { session, task_key: 'scan' }
      : url.endsWith('/series') ? [{ series_id: 7, external_match_count: 4, external_total_count: 4, external_sync_status: 'complete' }] : session;
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const view = setup();
  act(() => view.result.current.external.setExternalPath('/device'));
  await act(async () => view.result.current.external.startExternalLibraryScan());
  await act(async () => view.result.current.external.startExternalTransfer([7], () => {}));
  const before = requests.length;
  await act(async () => Source.current.onmessage?.({ data: 'run_snapshot:' + JSON.stringify({ sequence: 1, prev: 0, run: { type: 'transfer_external_library', status: 'cancelled', params: { session_id: 's1' } } }) }));
  expect(requests.slice(before).some(url => url.endsWith('/s1'))).toBe(true);
  expect(view.result.current.refresh).toBe(1);
  expect(view.result.current.external.externalTransferTaskKey).toBeNull();
  expect(view.result.current.external.externalSeriesMap[7]?.external_sync_status).toBe('complete');
});

it('does not apply old-library coverage over a newly opened session', async () => {
  let finishOldCoverage: (() => void) | undefined;
  apiClient.defaults.adapter = async config => {
    const url = config.url ?? '';
    const library = url.includes('/libraries/1/') ? 1 : 2;
    let data: unknown = { session: { session_id: `s${library}`, library_id: library, status: 'ready' }, task_key: 'scan' };
    if (url.endsWith('/series')) {
      if (library === 1) await new Promise<void>(resolve => { finishOldCoverage = resolve; });
      data = [{ series_id: 7, external_match_count: library, external_total_count: 4, external_sync_status: 'partial' }];
    }
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const view = setup();
  act(() => view.result.current.external.setExternalPath('/device'));
  await act(async () => view.result.current.external.startExternalLibraryScan());
  await waitFor(() => expect(finishOldCoverage).toBeDefined());
  view.rerender({ id: '2' });
  await act(async () => view.result.current.external.startExternalLibraryScan());
  await waitFor(() => expect(view.result.current.external.externalSeriesMap[7]?.external_match_count).toBe(2));
  await act(async () => finishOldCoverage?.());
  expect(view.result.current.external.externalSession?.session_id).toBe('s2');
  expect(view.result.current.external.externalSeriesMap[7]?.external_match_count).toBe(2);
});
it('recovers a missed transfer terminal after reconnect without a snapshot DOM bridge', async () => {
  const requests: string[] = [];
  apiClient.defaults.adapter = async config => {
    const url = config.url ?? ''; requests.push(url);
    const session = { session_id: 's1', library_id: 1, status: 'ready' };
    const data = config.method === 'post' ? url.endsWith('/transfer') ? { task_key: 'transfer' } : { session, task_key: 'scan' }
      : url.startsWith('/api/system/tasks?') ? [{ run_id: 8, task_id: 3, scope_id: 1, type: 'transfer_external_library', status: 'failed', params: { session_id: 's1' } }]
        : url.endsWith('/series') ? [] : session;
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  const view = setup();
  act(() => Source.current.onopen?.());
  act(() => view.result.current.external.setExternalPath('/device'));
  await act(async () => view.result.current.external.startExternalLibraryScan());
  await act(async () => view.result.current.external.startExternalTransfer([7], () => {}));
  const before = requests.length;
  await act(async () => Source.current.onopen?.());
  expect(view.result.current.external.externalTransferTaskKey).toBeNull();
  expect(view.result.current.refresh).toBe(1);
  expect(requests.slice(before).filter(url => url.endsWith('/s1'))).toHaveLength(1);
});

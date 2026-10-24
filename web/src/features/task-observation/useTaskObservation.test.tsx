/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { apiClient } from '../../api/client';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { ToastProvider } from '../../components/ToastProvider';
import { ApplicationSessionProvider } from '../../session/ApplicationSessionProvider';
import { useTaskObservation } from './useTaskObservation';
import { useState } from 'react';
import type { RunSnapshot } from '../../api/generated';

class Source {
  static CLOSED = 2;
  static instances: Source[] = [];
  readyState = 0;
  closed = false;
  onmessage: ((event: { data: string }) => void) | null = null;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() { Source.instances.push(this); }
  close() { this.closed = true; }
}
const run: RunSnapshot = { run_id: 9, task_id: 4, type: 'scan_library', scope: 'library', status: 'running', current: 1, total: 10 };
const original = apiClient.defaults.adapter;
afterEach(() => { cleanup(); apiClient.defaults.adapter = original; vi.unstubAllGlobals(); Source.instances = []; });
function Probe() {
  const [query, setQuery] = useState('');
  const observation = useTaskObservation({ status: 'ALL', scope: 'ALL', type: 'ALL', scopeId: '', query }, 0);
  return <>
    <output data-testid="live">{JSON.stringify(observation.live)}</output>
    <output data-testid="history">{JSON.stringify(observation.history)}</output>
    <output data-testid="tasks">{JSON.stringify(observation.tasks)}</output>
    <button onClick={() => observation.toggleTask(4)}>expand</button>
    <button onClick={() => void observation.runTaskAction(run, 'pause')}>pause</button>
    <button onClick={() => setQuery('chosen')}>filter</button>
  </>;
}
function start(identity = '1:admin') {
  vi.stubGlobal('EventSource', Source);
  return render(<LocaleProvider><ToastProvider><ApplicationSessionProvider identity={identity} isAdmin><Probe /></ApplicationSessionProvider></ToastProvider></LocaleProvider>);
}
function push(frame: object) { act(() => Source.instances.at(-1)?.onmessage?.({ data: 'run_live:' + JSON.stringify(frame) })); }
function setup() {
  const requests: string[] = [];
  let status = 'running';
  apiClient.defaults.adapter = async config => {
    const url = config.url ?? '';
    requests.push(`${config.method}:${url}`);
    const data = url.includes('/live') ? { active: 6, queued: 3, slots: 9, paused: false, paused_all: false, runs: [run] }
      : url.includes('/summary') ? [{ task_id: 4, type: 'scan_library', scope: 'library', disabled: false, fail_streak: 0, last_run: run }]
        : url.includes('task_id=4') ? [{ ...run, status }] : {};
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  return { requests, complete() { status = 'completed'; } };
}
it('recovers expanded history and backend totals on gaps/reconnect while preserving filters', async () => {
  const { requests, complete } = setup();
  start();
  await waitFor(() => expect(screen.getByTestId('live').textContent).toContain('"active":6'));
  fireEvent.click(screen.getByText('filter'));
  await waitFor(() => expect(requests.some(url => url.includes('q=chosen'))).toBe(true));
  fireEvent.click(screen.getByText('expand'));
  await waitFor(() => expect(screen.getByTestId('history').textContent).toContain('"status":"running"'));
  act(() => Source.instances[0].onopen?.());
  push({ sequence: 11, prev: 0, run, live: { active: 7, queued: 4, slots: 11, paused: false, paused_all: false } });
  expect(screen.getByTestId('live').textContent).toContain('"active":7');
  const count = requests.length;
  push({ sequence: 100, prev: 11, run });
  expect(requests).toHaveLength(count);
  complete();
  push({ sequence: 103, prev: 102 });
  await waitFor(() => expect(screen.getByTestId('history').textContent).toContain('"status":"completed"'));
  expect(requests.slice(count)).toContain('get:/api/system/tasks/summary?limit=50&q=chosen');
  const afterGap = requests.length;
  await act(async () => Source.instances[0].onopen?.());
  await waitFor(() => expect(requests.length).toBe(afterGap + 3));
  expect(Source.instances).toHaveLength(1);
});
it('does not issue follow-up requests when an old control completes after leaving its scope', async () => {
  const { requests } = setup();
  const immediate = apiClient.defaults.adapter;
  let finish: (() => void) | undefined;
  apiClient.defaults.adapter = async config => {
    if (config.method === 'post') {
      requests.push(`post:${config.url}`);
      await new Promise<void>(resolve => { finish = resolve; });
      return { data: {}, status: 200, statusText: 'OK', headers: {}, config };
    }
    if (typeof immediate !== 'function') throw new Error('test adapter missing');
    return immediate(config);
  };
  const view = start();
  await waitFor(() => expect(screen.getByTestId('live').textContent).toContain('"active":6'));
  fireEvent.click(screen.getByText('pause'));
  await waitFor(() => expect(requests).toContain('post:/api/system/runs/9/pause'));
  view.unmount();
  const count = requests.length;
  await act(async () => finish?.());
  expect(requests).toHaveLength(count);
});

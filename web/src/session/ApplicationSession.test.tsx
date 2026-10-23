/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ApplicationSessionProvider, useLibraryRefresh, useSessionRunPush } from './ApplicationSessionProvider';

class Source {
  static CLOSED = 2;
  static instances: Source[] = [];
  readyState = 0;
  closed = false;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onopen: (() => void) | null = null;
  constructor() { Source.instances.push(this); }
  close() { this.closed = true; }
}
afterEach(() => { cleanup(); vi.unstubAllGlobals(); Source.instances = []; });
function Consumer() {
  const refresh = useLibraryRefresh();
  const [count, setCount] = React.useState(0);
  useSessionRunPush(() => setCount(n => n + 1));
  return <><output>{refresh}:{count}</output><input aria-label="filter" /></>;
}
import * as React from 'react';
it('shares one connection, decodes refresh once, and preserves consumer local input', () => {
  vi.stubGlobal('EventSource', Source);
  render(<ApplicationSessionProvider identity="1:regular" isAdmin={false}><Consumer /><Consumer /></ApplicationSessionProvider>);
  expect(Source.instances).toHaveLength(1);
  fireEvent.change(screen.getAllByLabelText('filter')[0], { target: { value: 'keep' } });
  act(() => Source.instances[0].onmessage?.({ data: 'refresh' }));
  expect(screen.getAllByText('1:0')).toHaveLength(2);
  expect((screen.getAllByLabelText('filter')[0] as HTMLInputElement).value).toBe('keep');
});
it('replaces sensitive projections on identity/role change and rejects old connection callbacks', () => {
  vi.stubGlobal('EventSource', Source);
  const bridge = vi.fn();
  window.addEventListener('manga-manager:run-push', bridge);
  const view = render(<ApplicationSessionProvider identity="1:admin" isAdmin><Consumer /></ApplicationSessionProvider>);
  const old = Source.instances[0];
  const payload = 'run_live:{"sequence":7,"prev":3}';
  act(() => old.onmessage?.({ data: payload }));
  expect(screen.getByText('0:1')).toBeTruthy();
  expect(bridge).toHaveBeenCalledTimes(1);
  view.rerender(<ApplicationSessionProvider identity="1:regular" isAdmin={false}><Consumer /></ApplicationSessionProvider>);
  expect(old.closed).toBe(true);
  expect(screen.getByText('0:0')).toBeTruthy();
  act(() => {
    old.onmessage?.({ data: payload });
    Source.instances[1].onmessage?.({ data: payload });
    Source.instances[1].onmessage?.({ data: 'refresh' });
  });
  expect(bridge).toHaveBeenCalledTimes(1);
  expect(screen.getByText('1:0')).toBeTruthy();
  view.rerender(<ApplicationSessionProvider identity="2:admin" isAdmin><Consumer /></ApplicationSessionProvider>);
  expect(Source.instances[1].closed).toBe(true);
  expect(screen.getByText('0:0')).toBeTruthy();
  window.removeEventListener('manga-manager:run-push', bridge);
});

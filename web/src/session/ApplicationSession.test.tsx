/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ApplicationSessionProvider, useLibraryRefresh, useSessionRunPush, useSessionRecovery, useLibraryInvalidation } from './ApplicationSessionProvider';

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
  const view = render(<ApplicationSessionProvider identity="1:admin" isAdmin><Consumer /></ApplicationSessionProvider>);
  const old = Source.instances[0];
  const payload = 'run_live:{"sequence":7,"prev":3}';
  act(() => old.onmessage?.({ data: payload }));
  expect(screen.getByText('0:1')).toBeTruthy();
  view.rerender(<ApplicationSessionProvider identity="1:regular" isAdmin={false}><Consumer /></ApplicationSessionProvider>);
  expect(old.closed).toBe(true);
  expect(screen.getByText('0:0')).toBeTruthy();
  act(() => {
    old.onmessage?.({ data: payload });
    Source.instances[1].onmessage?.({ data: payload });
    Source.instances[1].onmessage?.({ data: 'refresh' });
  });
  expect(screen.getByText('1:0')).toBeTruthy();
  view.rerender(<ApplicationSessionProvider identity="2:admin" isAdmin><Consumer /></ApplicationSessionProvider>);
  expect(Source.instances[1].closed).toBe(true);
  expect(screen.getByText('0:0')).toBeTruthy();
});

it('reports gaps once at the shared session and resets continuity after recovery', () => {
  vi.stubGlobal('EventSource', Source);
  const recover = vi.fn();
  function Observer() { useSessionRecovery(recover); return null; }
  render(<ApplicationSessionProvider identity="1:admin" isAdmin><Observer /></ApplicationSessionProvider>);
  const source = Source.instances[0];
  act(() => source.onopen?.());
  act(() => source.onmessage?.({ data: 'run_live:{"sequence":11,"prev":0}' }));
  act(() => source.onmessage?.({ data: 'run_live:{"sequence":99,"prev":11}' }));
  expect(recover).not.toHaveBeenCalled();
  act(() => source.onmessage?.({ data: 'run_live:{"sequence":102,"prev":101}' }));
  expect(recover).toHaveBeenCalledTimes(1);
  act(() => source.onopen?.());
  expect(recover).toHaveBeenCalledTimes(2);
  act(() => source.onmessage?.({ data: 'run_live:{"sequence":5,"prev":0}' }));
  expect(recover).toHaveBeenCalledTimes(2);
});

it('notifies recovery when a closed connection is replaced', () => {
  vi.useFakeTimers();
  vi.stubGlobal('EventSource', Source);
  const recover = vi.fn();
  function Observer() { useSessionRecovery(recover); return null; }
  try {
    render(<ApplicationSessionProvider identity="1:admin" isAdmin><Observer /></ApplicationSessionProvider>);
    act(() => {
      Source.instances[0].onopen?.();
      Source.instances[0].readyState = Source.CLOSED;
      Source.instances[0].onerror?.();
      vi.advanceTimersByTime(1000);
      Source.instances[1].onopen?.();
    });
    expect(Source.instances).toHaveLength(2);
    expect(Source.instances[0].closed).toBe(true);
    expect(recover).toHaveBeenCalledTimes(1);
  } finally { vi.useRealTimers(); }
});

it('invalidates the affected library without resetting unrelated view input', () => {
  vi.stubGlobal('EventSource', Source);
  function View({ id }: { id: string }) {
    const version = useLibraryRefresh({ libraryId: id });
    const invalidate = useLibraryInvalidation();
    return <><output data-testid={id}>{version}</output><input aria-label={id} /><button onClick={() => invalidate({ libraryId: '1' })}>change{id}</button></>;
  }
  render(<ApplicationSessionProvider identity="1:regular" isAdmin={false}><View id="1" /><View id="2" /></ApplicationSessionProvider>);
  fireEvent.change(screen.getByLabelText('1'), { target: { value: 'draft' } });
  fireEvent.click(screen.getByText('change1'));
  expect(screen.getByTestId('1').textContent).toBe('1');
  expect(screen.getByTestId('2').textContent).toBe('0');
  expect((screen.getByLabelText('1') as HTMLInputElement).value).toBe('draft');
});

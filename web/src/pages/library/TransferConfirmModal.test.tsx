/** @vitest-environment jsdom */
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages } from '../../i18n/locales/en-US';
import { TransferConfirmModal } from './TransferConfirmModal';

afterEach(cleanup);

it('explains cancellation latency and preserves completed-file expectations', () => {
  render(<LocaleProvider initialLocale="en-US" initialMessages={messages} fallbackMessages={messages}>
    <TransferConfirmModal open onClose={vi.fn()} selectedCount={2} externalPath="E:/device" summary={{ total: 2, matched: 0, missing: 2 }} submitting={false} onConfirm={vi.fn()} />
  </LocaleProvider>);
  expect(screen.getByText('Cancel stops further copying once the current read or write returns. Files already transferred are kept.')).toBeTruthy();
  expect(screen.getByText('Pause keeps the current copy for this run. Resume checks the source and destination again; changed files stop safely. A server restart does not resume this transfer.')).toBeTruthy();
});

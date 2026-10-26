/** @vitest-environment jsdom */
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { apiClient } from '../api/client';
import { LocaleProvider } from '../i18n/LocaleProvider';
import { messages } from '../i18n/locales/zh-CN';
import { ToastProvider } from '../components/ToastProvider';
import MetadataReviews from './MetadataReviews';

const originalAdapter = apiClient.defaults.adapter;
afterEach(() => {
  cleanup();
  apiClient.defaults.adapter = originalAdapter;
  vi.unstubAllGlobals();
});

it('shows conflicts, no changes and faults separately after a partial-apply request', async () => {
  vi.stubGlobal('IntersectionObserver', class { observe() {} disconnect() {} unobserve() {} });
  let submitted = false;
  const item = (id: number) => ({ id, series_id: id, provider: 'bangumi', library_id: 1, library_name: 'Library', series_name: `Series ${id}`, fields: [{ id, field_name: 'title', current: 'old', proposed: 'new' }], confidence: 0.9, field_count: 1, locked_field_count: 0 });
  apiClient.defaults.adapter = async config => {
    let data: unknown;
    if (config.method === 'post') {
      expect(JSON.parse(String(config.data))).toEqual({ review_ids: [1, 2, 3], mode: 'fill_empty' });
      submitted = true;
      data = { success: false, conflict: [1], skipped: [2], failed: [3] };
    } else {
      data = { items: submitted ? [item(2), item(3)] : [item(1), item(2), item(3)], total: submitted ? 2 : 3, limit: 30, offset: 0 };
    }
    return { data, status: 200, statusText: 'OK', headers: {}, config };
  };
  render(<MemoryRouter><LocaleProvider initialLocale="zh-CN" initialMessages={messages} fallbackMessages={messages}><ToastProvider><MetadataReviews embedded /></ToastProvider></LocaleProvider></MemoryRouter>);
  await screen.findAllByText('Series 1');
  screen.getAllByTitle(messages['reviewInbox.markApply']).forEach(mark => fireEvent.click(mark));
  fireEvent.click(screen.getByText(messages['reviewInbox.applyMarked']));
  const text = messages['reviewInbox.toast.markedApplied']
    .replace('{{applied}}', '0').replace('{{rejected}}', '0').replace('{{partial}}', '0')
    .replace('{{skipped}}', '1').replace('{{conflict}}', '1').replace('{{failed}}', '1');
  expect(await screen.findByText(text)).toBeTruthy();
  await screen.findAllByText('Series 2');
  expect(screen.queryAllByText('Series 1')).toHaveLength(0);
});

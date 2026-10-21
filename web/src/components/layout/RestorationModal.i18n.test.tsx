// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as chinese } from '../../i18n/locales/zh-CN';
import { messages as english } from '../../i18n/locales/en-US';
import { RestorationModal } from './RestorationModal';

vi.mock('../../api/client', () => ({
  apiClient: { defaults: { headers: { common: {} } }, get: vi.fn((url: string) => Promise.resolve({ data: url.includes('details') ? { items: [], total: 0 } : {
    protection: { library_id: 1, operation_id: 3, scan_mode: 'interval', scan_interval: 17, scan_formats: 'cbz' },
    accepted: { id: 2, anomaly_count: 7 },
    check: { id: 4, purpose: 'restoration', protection_id: 3, library_id: 1, library_root: '/books', status: 'checked', anomaly_count: 5, blocker_count: 2 },
  } })), post: vi.fn() }, isAxiosError: () => false,
}));
afterEach(cleanup);

it.each(['zh-CN', 'en-US'] as const)('renders original settings and independent historical/current counts in %s without literal placeholders', async (locale) => {
  const { baseElement } = render(<LocaleProvider initialLocale={locale} initialMessages={locale === 'zh-CN' ? chinese : english} fallbackMessages={chinese}><RestorationModal library={{ id: 1, name: 'Books', path: '/books' }} onClose={vi.fn()} /></LocaleProvider>);
  await screen.findByText(locale === 'zh-CN' ? '原自动处理设置：定时刷新；间隔 17 分钟；格式 cbz' : 'Original automatic settings: Scheduled (Timer-based); interval 17 minutes; formats cbz');
  expect(screen.getByText(locale === 'zh-CN' ? '迁移时接受的历史文件异常：7 项' : 'Historical file anomalies accepted during relocation: 7')).toBeTruthy();
  expect(screen.getByText(locale === 'zh-CN' ? '当前文件异常 5 项，阻断项 2 项' : 'Current file anomalies: 5; blockers: 2')).toBeTruthy();
  expect(baseElement.textContent).not.toMatch(/\{(?:mode|interval|formats|count|anomalies|blockers)\}/);
});

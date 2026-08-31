/**
 * @vitest-environment jsdom
 *
 * 守整理工作台上「后端会拒的动作不摆给普通用户」：健康报告本身读得到，但刮削、重扫、重建
 * 身份索引、KOReader 对账与去重移除在后端一律要管理员，摆着的按钮点下去只换回一句泛化的
 * 失败提示。管理员那侧必须原样保留（反向判据）。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import { LocaleProvider } from '../i18n/LocaleProvider';
import { ToastProvider } from '../components/ToastProvider';
import { messages as zhCN } from '../i18n/locales/zh-CN';
import Organize from './Organize';

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), isAdmin: { value: false } }));

vi.mock('../api/client', () => ({
  apiClient: {
    get: mocks.get,
    post: mocks.post,
    defaults: { headers: { common: {} as Record<string, string> } },
  },
  isAxiosError: () => false,
  isCancel: () => false,
  getApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}));

// 只替掉「当前登录的是谁」，页面自己的判定照跑。
vi.mock('../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: mocks.isAdmin.value }) }));

const ISSUE = {
  type: 'missing_metadata',
  severity: 'warn',
  library_id: 1,
  library_name: '主库',
  series_id: 7,
  series_name: '缺元数据的系列',
  last_task_key: 'scrape_series_7',
};

function renderOrganize(isAdmin: boolean) {
  mocks.isAdmin.value = isAdmin;
  mocks.get.mockImplementation((url: string) => {
    if (url.startsWith('/api/libraries')) return Promise.resolve({ data: [{ id: 1, name: '主库' }] });
    if (url.startsWith('/api/health/report')) {
      return Promise.resolve({ data: { summary: [{ type: 'missing_metadata', severity: 'warn', count: 1 }], issues: [ISSUE], limit: 80 } });
    }
    return Promise.resolve({ data: {} });
  });
  return render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter>
          <Organize />
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('整理工作台的管理员动作', () => {
  it('普通用户看得到问题，但看不到任何修复动作', async () => {
    renderOrganize(false);
    expect(await screen.findByText('缺元数据的系列')).toBeTruthy();

    expect(screen.queryByText(zhCN['organize.action.scrape'] as string)).toBeNull();
    expect(screen.queryByText(zhCN['organize.identity.rebuild'] as string)).toBeNull();
    expect(screen.queryByText(zhCN['organize.openSourceTask'] as string)).toBeNull();
    expect(screen.queryByText(zhCN['dedup.title'] as string)).toBeNull();
    // 反向判据：只读的入口照旧留着。
    expect(screen.queryByText(zhCN['organize.openSeries'] as string)).toBeTruthy();
  });

  it('管理员一切照旧：修复、来源任务与去重面板都在', async () => {
    renderOrganize(true);
    expect(await screen.findByText('缺元数据的系列')).toBeTruthy();

    expect(screen.queryByText(zhCN['organize.action.scrape'] as string)).toBeTruthy();
    expect(screen.queryByText(zhCN['organize.identity.rebuild'] as string)).toBeTruthy();
    expect(screen.queryByText(zhCN['organize.openSourceTask'] as string)).toBeTruthy();
    expect(screen.queryByText(zhCN['dedup.title'] as string)).toBeTruthy();
  });
});

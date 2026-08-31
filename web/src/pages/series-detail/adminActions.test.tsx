/**
 * @vitest-environment jsdom
 *
 * 守系列详情页「后端会拒的动作不摆给普通用户」：编辑元数据、刮削、重扫、打开目录、写回
 * ComicInfo、加入合集、关系增删改、提案采纳/驳回、失败任务重试在后端一律要管理员，摆着的
 * 按钮点下去只换回一句泛化的失败提示。阅读进度与短评是每用户操作，必须原样留着（反向判据），
 * 管理员那侧也必须一切照旧。判据渲染真的页面与真的 AuthProvider，只替掉 HTTP。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider, useAuth } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import { ToastProvider } from '../../components/ToastProvider';
import { __resetScrapeProvidersCacheForTest } from '../../hooks/useScrapeProviders';
import type { SeriesContextResponse } from './types';
import SeriesDetailPage from './index';

const CONTEXT: SeriesContextResponse = {
  series: {
    id: 1,
    name: '测试系列',
    library_id: 1,
    path: '/lib/1',
    book_count: 1,
    locked_fields: { String: '', Valid: false },
  },
  books: [{ id: 11, name: '书11', library_id: 1, volume: '', page_count: 10, last_read_page: { Valid: false, Int64: 0 } }],
  tags: [],
  authors: [],
  links: [],
  relations: [{ id: 5, target_series_id: 2, target_series_name: '续作系列', relation_type: 'sequel' }],
  metadata_review: {
    reviews: [
      {
        id: 9,
        series_id: 1,
        provider: 'bangumi',
        source_url: '',
        source_id: 0,
        source_query: '',
        summary: '一条待审提案',
        confidence: 0.9,
        status: 'pending',
        raw_payload: '',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        fields: [
          { name: 'title', label: '标题', current: '旧', proposed: '新', confidence: 0.9, locked: false, source: 'bangumi', source_url: '' },
        ],
      },
    ],
    provenance: [],
  },
  failed_tasks: [
    { key: 'scrape_series_1', type: 'scrape', message: '刮削失败', retryable: true, updated_at: '2026-01-01T00:00:00Z' },
  ],
};

// 探针消费真的 useAuth，只用来把「鉴权已落地」变成一条可等待的判据：普通用户那侧全是
// 「找不到」，不等这一步的话，鉴权还没回来时用例就跑完了，任何实现都能通过。
function AuthProbe() {
  const { loading, isAdmin } = useAuth();
  return <span data-testid="probe">{loading ? '加载中' : isAdmin ? '管理员' : '普通用户'}</span>;
}

function mockApi(role: 'admin' | 'regular') {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/auth/status')) {
      return Promise.resolve({
        data: {
          setup_required: false,
          authenticated: true,
          csrf_token: 'csrf',
          user: { id: 1, username: role, role, display_name: role, must_change_password: false },
        },
      });
    }
    if (url.endsWith('/context')) return Promise.resolve({ data: CONTEXT });
    if (url.endsWith('/review')) return Promise.resolve({ data: { exists: false, rating: null, review: '' } });
    return Promise.resolve({ data: [] });
  }) as never);
  vi.spyOn(apiClient, 'post').mockResolvedValue({ data: {} } as never);
  vi.spyOn(apiClient, 'put').mockResolvedValue({ data: {} } as never);
}

async function renderPage(role: 'admin' | 'regular') {
  mockApi(role);
  render(
    <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
      <ToastProvider>
        <MemoryRouter initialEntries={['/series/1']}>
          <AuthProvider>
            <AuthProbe />
            <Routes>
              <Route path="/series/:seriesId" element={<SeriesDetailPage />} />
            </Routes>
          </AuthProvider>
        </MemoryRouter>
      </ToastProvider>
    </LocaleProvider>,
  );
  await screen.findByText(role === 'admin' ? '管理员' : '普通用户');
  await screen.findByRole('button', { name: '进入批量选择' });
}

function action(name: string) {
  return screen.queryByRole('button', { name });
}

// 书卡的更多操作是悬浮菜单，写回 ComicInfo 与上传封面都藏在里面，要先点开才看得到。
function openBookMenu() {
  fireEvent.click(screen.getByRole('button', { name: '更多操作' }));
}

// 侧栏挂着关系、提案与失败任务三块管理动作，同样要先点角标打开；角标默认落在失败任务页，
// 要看哪一块就再点哪个页签。页签只在侧栏内找——角标文案里也带着「失败任务」四个字。
function openSidePanel(tab: string) {
  fireEvent.click(screen.getByRole('button', { name: /项待审/ }));
  panelTab(tab);
}

function panelTab(name: string) {
  fireEvent.click(within(screen.getByRole('complementary')).getByRole('button', { name: new RegExp(name) }));
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  window.localStorage.clear();
  __resetScrapeProvidersCacheForTest();
});

describe('系列详情页的管理员动作', () => {
  it('普通用户看不到快捷动作条上的任何管理动作', async () => {
    await renderPage('regular');

    expect(action('编辑元数据')).toBeNull();
    expect(action('添加到合集')).toBeNull();
    expect(action('写入 ComicInfo 到所有归档')).toBeNull();
    expect(action('打开系统目录')).toBeNull();
    expect(action('重新扫描该系列')).toBeNull();
    expect(action('刮削元数据')).toBeNull();
    // 反向判据：只读的导出照旧留着，动作条不是整条消失。
    expect(action('导出本系列 ComicInfo.zip')).toBeTruthy();
  });

  it('普通用户的书卡菜单里没有写回归档与上传封面', async () => {
    await renderPage('regular');
    openBookMenu();

    expect(action('写入 ComicInfo 到文件')).toBeNull();
    expect(action('上传封面')).toBeNull();
    // 反向判据：只读的导出、下载与复制识别码照旧。
    expect(action('导出 ComicInfo.xml')).toBeTruthy();
    expect(action('下载原文件')).toBeTruthy();
    expect(action('复制识别码')).toBeTruthy();
  });

  it('普通用户的侧栏看得到关系、提案与失败任务，但点不了任何一处', async () => {
    await renderPage('regular');
    openSidePanel('系列关系');

    expect(action('删除关联')).toBeNull();
    expect(action('添加关联')).toBeNull();
    expect(screen.queryByPlaceholderText('搜索要关联的系列')).toBeNull();
    // 反向判据：关系本身仍是可读的跳转入口。
    expect(screen.getByRole('link', { name: /续作系列/ })).toBeTruthy();

    panelTab('元数据审核');
    expect(action('应用')).toBeNull();
    expect(action('拒绝')).toBeNull();
    expect(screen.getByText('一条待审提案')).toBeTruthy();

    panelTab('失败任务');
    expect(action('重试')).toBeNull();
    expect(screen.getByText('刮削失败')).toBeTruthy();
  });

  it('普通用户的每用户操作不受影响：阅读进度与短评照旧', async () => {
    await renderPage('regular');

    // 快捷标记已读走 /api/books/bulk-progress，后端按每用户写操作放行。
    expect(action('快速标记为已读')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '进入批量选择' }));
    fireEvent.click(screen.getByRole('link', { name: /书11/ }));
    expect(action('标为已读')).toBeTruthy();
    expect(action('标为未读')).toBeTruthy();

    openSidePanel('短评');
    expect(await screen.findByPlaceholderText('写下你对这个系列的想法……')).toBeTruthy();
  });

  it('管理员一切照旧：快捷动作、书卡菜单与侧栏动作全在', async () => {
    await renderPage('admin');

    expect(action('编辑元数据')).toBeTruthy();
    expect(action('添加到合集')).toBeTruthy();
    expect(action('写入 ComicInfo 到所有归档')).toBeTruthy();
    expect(action('打开系统目录')).toBeTruthy();
    expect(action('重新扫描该系列')).toBeTruthy();
    expect(action('刮削元数据')).toBeTruthy();

    openBookMenu();
    expect(action('写入 ComicInfo 到文件')).toBeTruthy();
    expect(action('上传封面')).toBeTruthy();

    openSidePanel('系列关系');
    expect(action('删除关联')).toBeTruthy();
    expect(action('添加关联')).toBeTruthy();
    panelTab('元数据审核');
    expect(action('应用')).toBeTruthy();
    expect(action('拒绝')).toBeTruthy();
    panelTab('失败任务');
    expect(action('重试')).toBeTruthy();
  });
});

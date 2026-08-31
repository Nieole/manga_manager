/**
 * @vitest-environment jsdom
 *
 * 守「合集页左栏能区分各个智能书架」：芯片必须从 /api/collection-views 的响应渲染出实际筛选条件，
 * 且任何取值都不能把原始词条键漏给用户（t() 缺 key 时会回退成键本身）。
 */

import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { BrowserRouter } from 'react-router-dom';

import { apiClient } from '../../api/client';
import { AuthProvider } from '../../auth/AuthProvider';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as enUS } from '../../i18n/locales/en-US';
import { messages as zhCN } from '../../i18n/locales/zh-CN';
import Collections from './index';
import { SmartFilterChips } from './SmartFilterChips';
import { SMART_SORT_FIELDS, type SmartFilter, type TFunc } from './types';

// 用词条表直接拼出 t：与 LocaleProvider 同样「缺 key 就回退成 key」，缺词条会原样暴露在断言里。
function catalogT(messages: Record<string, string>): TFunc {
  return (key, vars) => (messages[key] ?? key).replace(/\{\{\s*([^}]+?)\s*\}\}/g, (_, name: string) => String(vars?.[name.trim()] ?? ''));
}

const zh = catalogT(zhCN);

const BASE_FILTER: SmartFilter = {
  id: 7,
  library_id: 1,
  name: '书架',
  activeTag: null,
  activeAuthor: null,
  activeStatus: null,
  activeLetter: null,
  readState: null,
  minRating: null,
  maxRating: null,
  minProgress: null,
  maxProgress: null,
  addedWithinDays: null,
  sortByField: 'name',
  sortDir: 'asc',
  pageSize: 30,
};

function renderChips(filter: Partial<SmartFilter>, t: TFunc = zh) {
  return render(<SmartFilterChips filter={{ ...BASE_FILTER, ...filter }} t={t} />).container;
}

// 原始词条键长成 `collections.smartChip.sort.volumes` 这样，一旦漏到界面上就是这条断言要拦的。
function expectNoRawKey(container: HTMLElement) {
  expect(container.textContent).not.toMatch(/smartChip|status\.|collections\./);
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('SmartFilterChips 把筛选条件显示成人话', () => {
  const cases: Array<[string, Partial<SmartFilter>, string]> = [
    ['标签', { activeTag: 'Action' }, '#Action'],
    ['作者', { activeAuthor: 'ONE' }, '@ONE'],
    ['连载状态', { activeStatus: 'ongoing' }, '连载中'],
    ['连载状态的别名也认得', { activeStatus: 'publishing' }, '连载中'],
    ['首字母', { activeLetter: 'A' }, 'A'],
    ['阅读状态', { readState: 'reading' }, '阅读中'],
    ['评分区间', { minRating: 8, maxRating: 10 }, '★ 8–10'],
    ['单边进度区间', { minProgress: 50 }, '进度 ≥50%'],
    ['加入天数', { addedWithinDays: 30 }, '近 30 天添加'],
    ['非默认排序', { sortByField: 'rating', sortDir: 'desc' }, '评分 ↓'],
  ];
  for (const [name, filter, expected] of cases) {
    it(name, () => {
      const container = renderChips(filter);
      expect(screen.getByText(expected)).toBeTruthy();
      expectNoRawKey(container);
    });
  }

  // 认不出的状态值原样显示：宁可显示用户自己填的字符串，也不能谎报成「未知」。
  it('认不出的状态值原样显示', () => {
    const container = renderChips({ activeStatus: 'Zzz' });
    expect(screen.getByText('Zzz')).toBeTruthy();
    expectNoRawKey(container);
  });

  it('九个排序取值在两个语言包里都有词条', () => {
    for (const [localeName, messages] of [['zh-CN', zhCN], ['en-US', enUS]] as const) {
      for (const field of SMART_SORT_FIELDS) {
        const key = `collections.smartChip.sort.${field}`;
        expect(messages[key], `${localeName} 缺词条 ${key}`).toBeTruthy();
        const container = renderChips({ sortByField: field, sortDir: 'desc' }, catalogT(messages));
        expect(container.textContent, `${localeName} 的 ${field} 把原始键显示给了用户`).not.toContain(key);
        cleanup();
      }
    }
  });
});

describe('SmartFilterChips 的空态与折叠', () => {
  it('没有任何筛选条件的智能书架显示「无筛选条件」', () => {
    const container = renderChips({});
    expect(screen.getByText('无筛选条件')).toBeTruthy();
    expectNoRawKey(container);
  });

  // 排序不是筛选条件：默认的「名称 ↑」不该占一个芯片，否则「无筛选条件」永远显示不出来。
  it('默认排序不占一个芯片', () => {
    renderChips({});
    expect(screen.queryByText('名称 ↑')).toBeNull();
  });

  it('拿不到筛选定义时按无条件处理', () => {
    render(<SmartFilterChips filter={null} t={zh} />);
    expect(screen.getByText('无筛选条件')).toBeTruthy();
  });

  it('条件多于三项时折叠成「+N 项筛选」', () => {
    renderChips({ activeTag: 'Action', activeAuthor: 'ONE', activeStatus: 'ongoing', activeLetter: 'A', readState: 'reading' });
    expect(screen.getByText('#Action')).toBeTruthy();
    expect(screen.getByText('+2 项筛选')).toBeTruthy();
    // 折叠掉的条件挂在 title 上，鼠标停一下仍看得到。
    expect(screen.getByText('+2 项筛选').getAttribute('title')).toContain('阅读中');
    expect(screen.queryByText('阅读中')).toBeNull();
  });
});

const MANUAL_VIEW = {
  view_id: 'collection:3',
  numeric_id: 3,
  kind: 'collection',
  name: '手工精选',
  description: '',
  series_count: 1,
  source_type: 'manual',
  sort_order: 0,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

// 与后端 /api/collection-views 实际返回的形状一致：智能书架的筛选条件挂在 smart_filter 下。
const SMART_VIEW = {
  view_id: 'smart:7',
  numeric_id: 7,
  kind: 'smart',
  name: '高分动作在读',
  description: 'tag=Action    read=reading rating>=8.0',
  library_id: 1,
  library_name: 'Library A',
  series_count: 2,
  source_type: 'smart_filter',
  sort_order: 0,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  smart_filter: { ...BASE_FILTER, id: 7, name: '高分动作在读', activeTag: 'Action', readState: 'reading', minRating: 8 },
};

async function renderCollectionsPage() {
  vi.spyOn(apiClient, 'get').mockImplementation(((url: string) => {
    if (url.startsWith('/api/auth/status')) {
      return Promise.resolve({
        data: {
          setup_required: false,
          authenticated: true,
          csrf_token: 'csrf',
          user: { id: 1, username: 'admin', role: 'admin', display_name: 'admin', must_change_password: false },
        },
      });
    }
    if (url === '/api/collection-views') return Promise.resolve({ data: [MANUAL_VIEW, SMART_VIEW] });
    return Promise.resolve({ data: [] });
  }) as never);
  render(
    <BrowserRouter>
      <LocaleProvider initialLocale="zh-CN" initialMessages={zhCN} fallbackMessages={zhCN}>
        <AuthProvider>
          <Collections />
        </AuthProvider>
      </LocaleProvider>
    </BrowserRouter>,
  );
  return (await screen.findByText('高分动作在读')).closest('div.group') as HTMLElement;
}

describe('合集页左栏从 /api/collection-views 的响应渲染芯片', () => {
  it('智能书架列出它实际的筛选条件', async () => {
    const row = await renderCollectionsPage();
    expect(within(row).getByText('#Action')).toBeTruthy();
    expect(within(row).getByText('阅读中')).toBeTruthy();
    expect(within(row).getByText('★ ≥8')).toBeTruthy();
    expect(within(row).queryByText('无筛选条件')).toBeNull();
    expectNoRawKey(row);
  });

  it('手工合集不显示芯片', async () => {
    await renderCollectionsPage();
    const row = (await screen.findByText('手工精选')).closest('div.group') as HTMLElement;
    expect(within(row).queryByText('无筛选条件')).toBeNull();
    expect(within(row).queryByText('#Action')).toBeNull();
  });
});

/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「编辑框里的内容归用户，其余时候归服务端」。
 * 后台任务一完成就会静默重取系列上下文并换掉 series/tags/authors/links 的引用；
 * 破了的话弹窗还开着、用户刚敲的简介与标签已被服务端版本悄悄顶掉，输入白丢。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { apiClient } from '../../../api/client';
import type { SeriesContextResponse } from '../types';
import { useSeriesContext } from './useSeriesContext';
import { useSeriesEdit } from './useSeriesEdit';

// 服务端此刻的简介，用例可在两次取数之间改它，模拟刮削应用提案后值真的变了。
let serverSummary = new Map<number, string>();

function contextOf(seriesId: number): SeriesContextResponse {
  return {
    series: {
      id: seriesId,
      name: `系列${seriesId}`,
      library_id: 1,
      path: `/lib/${seriesId}`,
      title: { String: `系列${seriesId}标题`, Valid: true },
      summary: { String: serverSummary.get(seriesId) ?? `系列${seriesId}简介`, Valid: true },
      book_count: 1,
      locked_fields: { String: '', Valid: false },
    },
    books: [],
    tags: [{ id: seriesId, name: `标签${seriesId}` }],
    authors: [{ id: seriesId, name: `作者${seriesId}`, role: 'author' }],
    links: [],
  };
}

// 每次取数都新建一份响应对象：内容可以一模一样，引用一定是新的——这正是触发表单重置的东西。
function mockGet() {
  vi.spyOn(apiClient, 'get').mockImplementation((async (url: string) => {
    const id = Number(/^\/api\/series\/(\d+)\/context$/.exec(url)?.[1]);
    return { data: Number.isNaN(id) ? [] : contextOf(id) };
  }) as never);
}

/** 等所有已解决的取数落到状态上。 */
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function renderEditor(seriesId: string) {
  return renderHook(
    ({ id, trigger }: { id: string; trigger: number }) => {
      const ctx = useSeriesContext({ seriesId: id, refreshTrigger: trigger });
      const edit = useSeriesEdit({
        seriesId: id,
        series: ctx.series,
        tags: ctx.tags,
        authors: ctx.authors,
        links: ctx.links,
        reload: ctx.reload,
        showToast: vi.fn(),
        t: (key: string) => key,
      });
      return { ctx, edit };
    },
    { initialProps: { id: seriesId, trigger: 0 } },
  );
}

beforeEach(() => {
  serverSummary = new Map();
  mockGet();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('useSeriesEdit 表单重置', () => {
  it('编辑中后台任务完成触发刷新，用户敲进去的内容原样留着', async () => {
    const { result, rerender } = renderEditor('1');
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '用户正在写的新简介');
      result.current.edit.onFormChange('tagsInput', ['用户加的标签']);
    });

    // 扫描、刮削、封面重建等任一后台任务完成，都会走到这一步。
    rerender({ id: '1', trigger: 1 });
    await flush();

    expect(result.current.edit.editForm.summary?.String).toBe('用户正在写的新简介');
    expect(result.current.edit.editForm.tagsInput).toEqual(['用户加的标签']);
    // 用户在编辑期间点过的锁也是编辑内容的一部分，同样不能被刷新抹掉。
    expect(result.current.edit.lockedFields.has('summary')).toBe(true);
    expect(result.current.edit.isEditing).toBe(true);
  });

  it('非编辑态下后台刷新仍把表单同步到服务端最新值', async () => {
    const { result, rerender } = renderEditor('1');
    await flush();
    expect(result.current.edit.editForm.summary?.String).toBe('系列1简介');

    serverSummary.set(1, '刮削应用提案后的简介');
    rerender({ id: '1', trigger: 1 });
    await flush();

    expect(result.current.edit.editForm.summary?.String).toBe('刮削应用提案后的简介');
  });

  it('编辑中切到另一个系列，表单换成新系列的值', async () => {
    const { result, rerender } = renderEditor('1');
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '写给系列1的简介');
    });

    // 切系列时上下文会先清空再填上新值，表单必须跟到新系列，不能因为「正在编辑」留着上一个系列的内容。
    rerender({ id: '2', trigger: 0 });
    await flush();

    expect(result.current.edit.editForm.title?.String).toBe('系列2标题');
    expect(result.current.edit.editForm.summary?.String).toBe('系列2简介');
    expect(result.current.edit.editForm.tagsInput).toEqual(['标签2']);
    expect(result.current.edit.editForm.authorsInput).toEqual([{ name: '作者2', role: 'author' }]);
  });
});

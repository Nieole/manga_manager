/**
 * @vitest-environment jsdom
 *
 * 本文件守卫「编辑框里的内容归用户」：后台刷新不得顶掉未保存的输入，保存必须带回表单长出来的
 * 那个版本、被拒时输入原样留着。破了的话，用户刚敲的简介与标签被服务端版本悄悄顶掉，或版本跟着
 * 后台刷新走、服务端查不出冲突，后写的照旧静默覆盖先写的。
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';

import { apiClient } from '../../../api/client';
import type { SeriesContextResponse } from '../types';
import { useSeriesContext } from './useSeriesContext';
import { useSeriesEdit } from './useSeriesEdit';

// 服务端此刻的简介，用例可在两次取数之间改它，模拟刮削应用提案后值真的变了。
let serverSummary = new Map<number, string>();
// 服务端此刻的元数据版本，随内容一起变。
let serverVersion = new Map<number, string>();
// 这些系列的上下文里没有元数据版本：详情载入降级、旧后端、将来某个新入口忘了带都是这个形状。
let versionMissing = new Set<number>();

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
    metadata_version: versionMissing.has(seriesId) ? undefined : (serverVersion.get(seriesId) ?? `v1-${seriesId}`),
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

function renderEditor(seriesId: string, showToast: (message: string, level: 'success' | 'error') => void = vi.fn()) {
  return renderHook(
    ({ id, trigger }: { id: string; trigger: number }) => {
      const ctx = useSeriesContext({ seriesId: id, refreshTrigger: trigger });
      const edit = useSeriesEdit({
        seriesId: id,
        series: ctx.series,
        tags: ctx.tags,
        authors: ctx.authors,
        links: ctx.links,
        metadataVersion: ctx.metadataVersion,
        reload: ctx.reload,
        showToast,
        t: (key: string) => key,
      });
      return { ctx, edit };
    },
    { initialProps: { id: seriesId, trigger: 0 } },
  );
}

beforeEach(() => {
  serverSummary = new Map();
  serverVersion = new Map();
  versionMissing = new Set();
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

/** 造一个 axios 形状的 409，模拟服务端认出「编辑期间有人改过」。 */
function conflictError(currentVersion: string) {
  return Object.assign(new Error('Request failed with status code 409'), {
    isAxiosError: true,
    response: { status: 409, data: { error: 'conflict', current_version: currentVersion } },
  });
}

describe('useSeriesEdit 并发保存', () => {
  it('保存带回表单长出来的那个版本，编辑期间的后台刷新不改这个基线', async () => {
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: {} } as never);
    const { result, rerender } = renderEditor('1');
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '我写的简介');
    });

    // 编辑期间别人改了这个系列，后台刷新把上下文换成了新内容与新版本。
    serverSummary.set(1, '别人写的简介');
    serverVersion.set(1, 'v2-1');
    rerender({ id: '1', trigger: 1 });
    await flush();

    await act(async () => {
      await result.current.edit.save();
    });

    // 必须还是打开编辑器时那一版。跟着刷新走就等于替用户认领了别人的改动，
    // 服务端查不出冲突，后写的照旧静默覆盖先写的。
    expect(put.mock.calls[0][1]).toMatchObject({ expected_version: 'v1-1' });
  });

  it('服务端报冲突时保存不落地，弹窗与用户敲进去的内容原样留着', async () => {
    vi.spyOn(apiClient, 'put').mockRejectedValue(conflictError('v2-1'));
    const showToast = vi.fn();
    const { result } = renderEditor('1', showToast);
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '用户敲了半天的简介');
      result.current.edit.onFormChange('tagsInput', ['用户加的标签']);
    });

    await act(async () => {
      await result.current.edit.save();
    });

    expect(result.current.edit.hasConflict).toBe(true);
    expect(result.current.edit.isEditing).toBe(true);
    expect(result.current.edit.editForm.summary?.String).toBe('用户敲了半天的简介');
    expect(result.current.edit.editForm.tagsInput).toEqual(['用户加的标签']);
    expect(showToast).toHaveBeenCalledWith('series.toast.saveConflict', 'error');
  });

  it('看过冲突提示后再存一次，以服务端最新版本为基线覆盖', async () => {
    const put = vi
      .spyOn(apiClient, 'put')
      .mockRejectedValueOnce(conflictError('v2-1'))
      .mockResolvedValue({ data: {} } as never);
    const { result } = renderEditor('1');
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '我写的简介');
    });

    await act(async () => {
      await result.current.edit.save();
    });
    expect(result.current.edit.hasConflict).toBe(true);

    await act(async () => {
      await result.current.edit.save();
    });
    await flush();

    expect(put.mock.calls[1][1]).toMatchObject({ expected_version: 'v2-1', summary: '我写的简介' });
    expect(result.current.edit.isEditing).toBe(false);
    expect(result.current.edit.hasConflict).toBe(false);
  });

  it('拿不到元数据版本时不保存，请求根本不发出去', async () => {
    // 带空版本发出去，服务端就跳过并发校验、后写的静默覆盖先写的，界面上那句「有人改过了」
    // 再也不会出现——用户以为自己受着保护。拿不到版本就不让存，把失效摆到明面上。
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: {} } as never);
    const showToast = vi.fn();
    versionMissing.add(1);
    const { result } = renderEditor('1', showToast);
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '我写的简介');
    });

    await act(async () => {
      await result.current.edit.save();
    });

    expect(put).not.toHaveBeenCalled();
    expect(showToast).toHaveBeenCalledWith('series.toast.saveNoVersion', 'error');
    // 保存没成功，编辑态与用户敲进去的内容都得留着。
    expect(result.current.edit.isEditing).toBe(true);
    expect(result.current.edit.editForm.summary?.String).toBe('我写的简介');
  });

  it('没人插队时保存照常成功，不留冲突态', async () => {
    const put = vi.spyOn(apiClient, 'put').mockResolvedValue({ data: {} } as never);
    const { result } = renderEditor('1');
    await flush();

    act(() => {
      result.current.edit.setIsEditing(true);
    });
    await flush();
    act(() => {
      result.current.edit.onFormChange('summary', '我写的简介');
    });

    await act(async () => {
      await result.current.edit.save();
    });
    await flush();

    expect(put.mock.calls[0][1]).toMatchObject({ expected_version: 'v1-1', summary: '我写的简介' });
    expect(result.current.edit.isEditing).toBe(false);
    expect(result.current.edit.hasConflict).toBe(false);
  });
});

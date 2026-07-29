/**
 * @vitest-environment jsdom
 *
 * 业务说明：本文件是资料库表单重构的行为基准。
 *
 * 「新增」与「编辑」此前是两套完全平行的实现（各七八个 state、各一份逐字段 setter
 * 与提交逻辑）。两套代码做同一件事，代价是漂移：给表单加第七个字段时很容易只改一边，
 * 然后编辑弹窗少一个字段、还没人发现。
 *
 * 抽成共享 hook 之后，真正需要守住的是「两条路**本来就该不同**的地方没有被合掉」。
 * 那份差异清单写在下面的用例里。
 */

import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { libraryFormPayload, useLibraryForm, type LibraryFormValues } from './useLibraryForm';

const EMPTY: LibraryFormValues = {
  name: '',
  path: '',
  scanMode: 'none',
  koreaderSyncEnabled: true,
  scanInterval: 60,
  scanFormats: 'zip,cbz,rar,cbr',
};

describe('useLibraryForm', () => {
  it('逐字段修改互不影响', () => {
    const { result } = renderHook(() => useLibraryForm(EMPTY));

    act(() => result.current.bindings.onNameChange('我的书库'));
    act(() => result.current.bindings.onScanIntervalChange(120));
    act(() => result.current.bindings.onKOReaderSyncEnabledChange(false));

    expect(result.current.values.name).toBe('我的书库');
    expect(result.current.values.scanInterval).toBe(120);
    expect(result.current.values.koreaderSyncEnabled).toBe(false);
    // 没被碰过的字段必须保持原值——逐字段 setter 最容易写成整份覆盖。
    expect(result.current.values.path).toBe('');
    expect(result.current.values.scanFormats).toBe('zip,cbz,rar,cbr');
  });

  it('reset 用于载入某个库的当前配置', () => {
    const { result } = renderHook(() => useLibraryForm(EMPTY));

    act(() =>
      result.current.reset({
        name: 'Library A',
        path: '/data/manga',
        scanMode: 'auto',
        koreaderSyncEnabled: false,
        scanInterval: 30,
        scanFormats: 'cbz',
      }),
    );

    expect(result.current.values).toEqual({
      name: 'Library A',
      path: '/data/manga',
      scanMode: 'auto',
      koreaderSyncEnabled: false,
      scanInterval: 30,
      scanFormats: 'cbz',
    });
  });

  it('setPath 只改路径，不碰其他字段', () => {
    // 目录选择器「选中当前目录」走的是这条路：它只该填路径。
    const { result } = renderHook(() => useLibraryForm(EMPTY));
    act(() => result.current.bindings.onNameChange('我的书库'));
    act(() => result.current.setPath('/mnt/usb/manga'));

    expect(result.current.values.path).toBe('/mnt/usb/manga');
    expect(result.current.values.name).toBe('我的书库');
  });

  it('setScanFormats 只改格式，不清掉用户已填的内容', () => {
    // 挂载时探测到后端支持的格式集合后会覆盖这一个字段。
    // 若图省事用整份 reset，用户刚输入的库名就没了。
    const { result } = renderHook(() => useLibraryForm(EMPTY));
    act(() => result.current.bindings.onNameChange('我的书库'));
    act(() => result.current.setScanFormats('zip,cbz,rar,cbr,7z'));

    expect(result.current.values.scanFormats).toBe('zip,cbz,rar,cbr,7z');
    expect(result.current.values.name).toBe('我的书库');
  });

  it('两次调用彼此隔离', () => {
    // 新增与编辑是两个独立实例。共享 hook 若不慎用了模块级状态，改一边会串到另一边。
    const add = renderHook(() => useLibraryForm(EMPTY));
    const edit = renderHook(() => useLibraryForm(EMPTY));

    act(() => add.result.current.bindings.onNameChange('新库'));

    expect(add.result.current.values.name).toBe('新库');
    expect(edit.result.current.values.name).toBe('');
  });

  it('bindings 的形状与弹窗的受控 props 对齐', () => {
    const { result } = renderHook(() => useLibraryForm(EMPTY));
    const keys = Object.keys(result.current.bindings).sort();
    expect(keys).toEqual(
      [
        'koreaderSyncEnabled',
        'name',
        'onKOReaderSyncEnabledChange',
        'onNameChange',
        'onPathChange',
        'onScanFormatsChange',
        'onScanIntervalChange',
        'onScanModeChange',
        'path',
        'scanFormats',
        'scanInterval',
        'scanMode',
        'submitting',
      ].sort(),
    );
  });
});

describe('libraryFormPayload', () => {
  it('字段名与后端契约一致', () => {
    // 新增（POST）与编辑（PUT）发的是同一份载荷，只是方法与 URL 不同。
    // 此前这段 JSON 在两处逐字重复，是最容易漂移的地方。
    expect(
      libraryFormPayload({
        name: 'Library A',
        path: '/data/manga',
        scanMode: 'auto',
        koreaderSyncEnabled: false,
        scanInterval: 30,
        scanFormats: 'cbz',
      }),
    ).toEqual({
      name: 'Library A',
      path: '/data/manga',
      scan_mode: 'auto',
      koreader_sync_enabled: false,
      scan_interval: 30,
      scan_formats: 'cbz',
    });
  });
});

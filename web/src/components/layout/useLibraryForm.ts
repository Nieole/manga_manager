/**
 * 业务说明：资料库表单（新增/编辑）的共享状态。
 *
 * 此前「新增」与「编辑」在 Layout 里是两套**完全平行**的实现：newLib* 七个 state
 * 与 editLib* 八个 state，各自一份逐字段的 setter 与提交逻辑。两套代码做同一件事，
 * 唯一的区别只有「打哪个接口」和「成功后重置不重置」——而这种平行结构的代价是漂移：
 * 给表单加第七个字段时很容易只改一边，然后编辑弹窗少一个字段、还没人发现。
 *
 * 这里只抽「表单状态」，不抽「提交」：两条路的提交在语义上确实不同（见 Layout 里的注释），
 * 硬合成一个带 mode 分支的函数只会把差异藏进 if 里，不会让它更清楚。
 */

import { useCallback, useMemo, useState } from 'react';

export interface LibraryFormValues {
  name: string;
  path: string;
  scanMode: string;
  koreaderSyncEnabled: boolean;
  scanInterval: number;
  scanFormats: string;
}

// LibraryFormBindings 的形状与 LibraryFormModal 的受控 props 一一对应，
// 调用点可以整个展开进去，不必再逐字段接线。
export interface LibraryFormBindings {
  name: string;
  path: string;
  scanMode: string;
  koreaderSyncEnabled: boolean;
  scanInterval: number;
  scanFormats: string;
  submitting: boolean;
  onNameChange: (value: string) => void;
  onPathChange: (value: string) => void;
  onScanModeChange: (value: string) => void;
  onKOReaderSyncEnabledChange: (value: boolean) => void;
  onScanIntervalChange: (value: number) => void;
  onScanFormatsChange: (value: string) => void;
}

export interface LibraryFormState {
  values: LibraryFormValues;
  submitting: boolean;
  setSubmitting: (value: boolean) => void;
  // reset 用于「打开编辑弹窗时载入这一行的值」与「新增成功后清空」。
  reset: (next: LibraryFormValues) => void;
  setPath: (value: string) => void;
  // setScanFormats 单独暴露：挂载时探测到后端支持的格式集合后要覆盖这一个字段，
  // 不能整份 reset（那会连用户可能已经填的内容一起清掉）。
  setScanFormats: (value: string) => void;
  bindings: LibraryFormBindings;
}

export function useLibraryForm(initial: LibraryFormValues): LibraryFormState {
  const [values, setValues] = useState<LibraryFormValues>(initial);
  const [submitting, setSubmitting] = useState(false);

  const reset = useCallback((next: LibraryFormValues) => setValues(next), []);
  // setPath 单独暴露：目录选择器选中当前目录时只改路径，不碰其他字段。
  const setPath = useCallback((value: string) => setValues((prev) => ({ ...prev, path: value })), []);
  const setScanFormats = useCallback(
    (value: string) => setValues((prev) => ({ ...prev, scanFormats: value })),
    [],
  );

  const bindings = useMemo<LibraryFormBindings>(
    () => ({
      ...values,
      submitting,
      onNameChange: (value) => setValues((prev) => ({ ...prev, name: value })),
      onPathChange: (value) => setValues((prev) => ({ ...prev, path: value })),
      onScanModeChange: (value) => setValues((prev) => ({ ...prev, scanMode: value })),
      onKOReaderSyncEnabledChange: (value) => setValues((prev) => ({ ...prev, koreaderSyncEnabled: value })),
      onScanIntervalChange: (value) => setValues((prev) => ({ ...prev, scanInterval: value })),
      onScanFormatsChange: (value) => setValues((prev) => ({ ...prev, scanFormats: value })),
    }),
    [values, submitting],
  );

  return { values, submitting, setSubmitting, reset, setPath, setScanFormats, bindings };
}

// libraryFormPayload 把表单值转成后端契约的字段名。
// 新增与编辑发的是同一份载荷（只是方法与 URL 不同），抽出来保证两边不会漂移
// ——此前这段 JSON 在 Layout 里逐字重复了两遍。
export function libraryFormPayload(values: LibraryFormValues) {
  return {
    name: values.name,
    path: values.path,
    scan_mode: values.scanMode,
    koreader_sync_enabled: values.koreaderSyncEnabled,
    scan_interval: values.scanInterval,
    scan_formats: values.scanFormats,
  };
}

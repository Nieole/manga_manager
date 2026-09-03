/**
 * 阅读器「返回」按钮导航到哪，纯函数便于测试。
 * 有站内历史时必须用浏览器回退 navigate(-1) 弹出阅读器这一条，不能 push 系列页——
 * 否则历史栈里阅读器之前又多一层，连续两次返回会绕回阅读器而不是回到资料库。
 */

export type ReaderBackAction =
  | { kind: 'history' } // navigate(-1)：浏览器回退，弹出阅读器
  | { kind: 'series'; url: string } // navigate(url)：导航到系列页
  | { kind: 'home' }; // navigate('/')：回首页

export interface ReaderBackParams {
  seriesId: string | number | null | undefined;
  bookVolume?: string | null;
  // hasInAppHistory：进入阅读器时历史栈里是否还有站内来源页（react-router location.key !== 'default'）。
  hasInAppHistory: boolean;
}

export function computeReaderBack({ seriesId, bookVolume, hasInAppHistory }: ReaderBackParams): ReaderBackAction {
  // 有站内历史：直接回退，弹出阅读器这一条——避免把系列页重复压栈导致「再返回」又回到阅读器。
  if (hasInAppHistory) {
    return { kind: 'history' };
  }
  // 直达阅读器且无所属系列：回首页。
  if (seriesId === null || seriesId === undefined || seriesId === '') {
    return { kind: 'home' };
  }
  // 直达阅读器：导航到系列页（若知道卷号则带上高亮锚点）。
  const url = bookVolume
    ? `/series/${seriesId}?volume=${encodeURIComponent(bookVolume)}`
    : `/series/${seriesId}`;
  return { kind: 'series', url };
}

/**
 * 业务说明：本文件是阅读器「返回」按钮的导航决策（纯函数，便于测试）。
 * 修复的 bug：此前返回按钮用 navigate('/series/:id')（push），会在历史栈里多压一个系列页，
 * 于是「阅读器→返回→系列页→再返回」时浏览器回退到了阅读器而非资源库。
 * 正确做法：若是从站内跳进阅读器的（历史栈里阅读器之前有来源页），返回就用浏览器回退 navigate(-1)，
 * 弹出阅读器这一条历史，回到来源页（通常是系列页），且不把阅读器留在历史里；
 * 只有直达阅读器（深链/新标签、无站内历史）时才导航到系列页。
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

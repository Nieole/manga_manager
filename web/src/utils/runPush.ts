/**
 * 推送通道这一层：把一条 SSE 载荷解成**推送帧**，并判断它接不接得上上一帧。
 * 判漏帧只能靠帧里那个 prev——逐条目进度按水位节流，被吞掉的帧照样取了号，
 * 送达的序号本来就带空档，按「序号跳了」判会让一次繁忙的扫描每收一帧就整份重拉一次。
 */

import type { RunPush } from '../api/generated';

// 推送通道上两种帧的事件名，与后端的 runSnapshotEventPrefix / runLiveEventPrefix 逐字同形。
const RUN_PUSH_PREFIXES = ['run_snapshot:', 'run_live:'];

/** parseRunPush 把一条 SSE 载荷解成推送帧；不是推送帧、或者载荷坏掉即 null。 */
export function parseRunPush(data: string): RunPush | null {
  const prefix = RUN_PUSH_PREFIXES.find((candidate) => data.startsWith(candidate));
  if (!prefix) return null;
  try {
    const frame = JSON.parse(data.slice(prefix.length)) as RunPush;
    // 两个序号是判缺口的全部依据，缺了就没法接链——认不出形状的载荷整帧丢掉，
    // 而不是拿 undefined 去比对（那会把每一帧都判成缺口）。
    if (!frame || typeof frame.sequence !== 'number' || typeof frame.prev !== 'number') return null;
    return frame;
  } catch (error) {
    console.warn('Failed to parse a task push frame:', error);
    return null;
  }
}

/**
 * trackRunPush 判断这一帧接不接得上上一帧：接不上就是中间掉了东西，调用方据此整份重拉。
 *
 * 游标为 null（刚连上，或上一次刚重拉过）时一律接得上：此刻手上没有可比的上一号，
 * 拿这一帧当新的起点即可。判出缺口之后游标同样走到这一帧——这一帧本身是收到了的，缺的是它
 * 前面那些；不往前走的话，此后每一帧都会再判一次缺口，重拉从此一路刷下去。
 */
export function trackRunPush(cursor: number | null, frame: RunPush): { gap: boolean; cursor: number } {
  return { gap: cursor !== null && frame.prev !== cursor, cursor: frame.sequence };
}

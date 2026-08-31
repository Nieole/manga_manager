/**
 * 本文件是「断网时凭什么放行」的判据。只有请求根本没拿到响应才算离线，且只放行离线书架与
 * 本机书目索引里确实有的那本书；索引在换用户时被整份清空，别人下载的书因此进不来。
 * 判据全在本地：能改 localStorage 的人（也就是这台设备的持有者）伪造得出来，它挡的是共享
 * 设备上的另一个普通用户，不是设备持有者本人。真正的隔离得靠系统账户或浏览器配置分离。
 */

import { isAxiosError } from '../api/client';
import { hasOfflineOwner, isOfflineBookDownloaded } from '../pages/book-reader/offlineReader';

// isBackendUnreachable 判断状态探测是不是「根本没走到后端」。
//
// 后端答了 4xx/5xx 就不算：它能应答，就轮不到离线态。这也与 Service Worker 对齐——阅读类
// 请求只在 fetch 抛错时才回落缓存，收到错误码是原样透传的，此时放行也读不到任何东西。
export function isBackendUnreachable(error: unknown): boolean {
  return isAxiosError(error) && !error.response;
}

const READER_PATH = /^\/reader\/([^/]+)\/?$/;

// isOfflineReadableRoute 判断某条路由在离线态下是否放行：离线书架，以及本机已下载的那本书。
//
// 逐本认而不是认「这台设备上有人下过东西」：书 id 是可枚举的小整数，认设备等于把 /reader/1
// 到 /reader/999 全部放开。没有 owner 标记则一律不放行——没人在这台设备上登录过，
// 就没有属于谁的离线数据可读。
//
// 判据只看本机，从不看会话是否有效：会话过期后照样放行，放出的仍是这台设备上这个 owner
// 自己下载的那些字节，与会话过期前完全一样，没有多放出任何东西。真正的另一个用户要读到
// 东西必须先登录，那一刻 reconcileOfflineOwner 就把上一个人的索引清空了。
export function isOfflineReadableRoute(pathname: string): boolean {
  if (!hasOfflineOwner()) return false;
  if (pathname === '/offline' || pathname === '/offline/') return true;
  const match = READER_PATH.exec(pathname);
  if (!match) return false;
  try {
    return isOfflineBookDownloaded(decodeURIComponent(match[1]));
  } catch {
    // 路径段不是合法的百分号编码：不是任何一本书的 id。
    return false;
  }
}

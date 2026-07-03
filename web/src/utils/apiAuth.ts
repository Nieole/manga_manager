/**
 * 业务说明：本文件是可选管理 API 令牌鉴权（后端 server.auth）的前端支撑层。
 * 未设置令牌时全部为无操作，默认行为与历史版本完全一致；启用后端鉴权后，
 * 只需存入令牌即可让所有 axios 管理请求与 SSE 连接自动携带令牌。
 * 维护时应保持“无令牌即无操作”的语义，避免影响未启用鉴权的部署。
 */

import axios from 'axios';

const TOKEN_KEY = 'mm_token';

// getApiToken 读取本地存储的管理令牌；存储不可用或未设置时返回空串。
export function getApiToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY)?.trim() ?? '';
  } catch {
    return '';
  }
}

// installApiAuth 安装全局 axios 请求拦截器：存在令牌时附加 X-API-Token 头。
// 应用启动时调用一次即可。未设置令牌时不改动任何请求。
export function installApiAuth(): void {
  axios.interceptors.request.use((config) => {
    const token = getApiToken();
    if (token && config.headers) {
      config.headers.set('X-API-Token', token);
    }
    return config;
  });
}

// withApiToken 给无法自定义请求头的场景（EventSource、<img> 等）在 URL 上附加 token 查询参数。
// 未设置令牌时原样返回。
export function withApiToken(url: string): string {
  const token = getApiToken();
  if (!token) return url;
  const sep = url.includes('?') ? '&' : '?';
  return `${url}${sep}token=${encodeURIComponent(token)}`;
}

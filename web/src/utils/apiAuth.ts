/**
 * 本文件是多用户 Cookie 会话鉴权的前端支撑层。会话令牌由后端以 HttpOnly Cookie 下发，
 * 前端不可读；改写类请求（POST/PUT/PATCH/DELETE）需在 X-CSRF-Token 头回传登录时拿到的 CSRF 令牌。
 * 所有 axios 请求统一 withCredentials 以携带会话 Cookie。CSRF 令牌仅存内存，由 AuthProvider 更新。
 * 维护要点：Cookie 同源自动携带，EventSource/<img>/下载这类无法自定义请求头的场景同样靠 Cookie。
 */

import axios, { type InternalAxiosRequestConfig } from 'axios';

// csrfToken 仅存内存：随登录 / 状态查询更新，页面刷新后由 AuthProvider 重新拉取。
let csrfToken = '';

export function setCsrfToken(token: string | undefined | null): void {
  csrfToken = token?.trim() ?? '';
}

export function getCsrfToken(): string {
  return csrfToken;
}

const MUTATING_METHODS = new Set(['post', 'put', 'patch', 'delete']);

// attachAuth 为请求挂上会话 Cookie（withCredentials）与改写类方法的 CSRF 头。
export function attachAuth(config: InternalAxiosRequestConfig): InternalAxiosRequestConfig {
  config.withCredentials = true;
  const method = (config.method ?? 'get').toLowerCase();
  if (csrfToken && MUTATING_METHODS.has(method) && config.headers) {
    config.headers.set('X-CSRF-Token', csrfToken);
  }
  return config;
}

// installApiAuth 在全局 axios 上安装鉴权拦截器并默认携带凭据；应用启动时调用一次。
export function installApiAuth(): void {
  axios.defaults.withCredentials = true;
  axios.interceptors.request.use(attachAuth);
}

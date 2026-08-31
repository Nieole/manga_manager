/**
 * 本文件是前端多用户鉴权的全局状态层。应用启动时探测站点初始化与登录态（GET /api/auth/status），
 * 并提供 setup / login / logout / changePassword；探测连不上后端时另报 backendUnreachable。
 * 登录成功后把后端下发的 CSRF 令牌交给 apiAuth（供改写类请求携带），会话本身走 HttpOnly Cookie。
 * 还安装了 401 响应拦截：会话过期时清空登录态，交由 AuthGate 回到登录页。
 * 维护要点：CSRF 令牌只存内存；密码一律由用户在自己的界面输入，前端从不缓存明文。
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { apiClient, getApiErrorMessage, isAxiosError } from '../api/client';
import { reconcileOfflineOwner } from '../pages/book-reader/offlineReader';
import { isBackendUnreachable } from './offlineAccess';
import { setCsrfToken } from '../utils/apiAuth';

export type UserRole = 'admin' | 'regular';

export interface AuthUser {
  id: number;
  username: string;
  role: UserRole;
  display_name: string;
  must_change_password: boolean;
}

interface AuthStatusResponse {
  setup_required: boolean;
  authenticated: boolean;
  user?: AuthUser;
  csrf_token?: string;
}

interface SessionResponse {
  user: AuthUser;
  csrf_token: string;
}

interface AuthContextValue {
  loading: boolean;
  setupRequired: boolean;
  user: AuthUser | null;
  isAdmin: boolean;
  // backendUnreachable 表示状态探测根本没走到后端（断网、后端进程不在）。
  // 它不是「已登录」的替代品，只供 AuthGate 判断要不要放行离线可读的那几条路由。
  backendUnreachable: boolean;
  refresh: () => Promise<void>;
  setup: (username: string, password: string, displayName: string) => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  changePassword: (currentPassword: string, newPassword: string) => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true);
  const [setupRequired, setSetupRequired] = useState(false);
  const [user, setUser] = useState<AuthUser | null>(null);
  const [backendUnreachable, setBackendUnreachable] = useState(false);

  const applySession = useCallback((data: SessionResponse) => {
    // 换人对账放在最前：此刻旧会话的 CSRF 还没被覆盖，清理动作用的仍是一致的上下文。
    reconcileOfflineOwner(data.user.id);
    setCsrfToken(data.csrf_token);
    setUser(data.user);
    setSetupRequired(false);
    setBackendUnreachable(false);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const { data } = await apiClient.get<AuthStatusResponse>('/api/auth/status');
      setBackendUnreachable(false);
      setSetupRequired(data.setup_required);
      if (data.authenticated && data.user) {
        reconcileOfflineOwner(data.user.id);
        setCsrfToken(data.csrf_token ?? '');
        setUser(data.user);
      } else {
        reconcileOfflineOwner(null);
        setCsrfToken('');
        setUser(null);
      }
    } catch (error) {
      // 状态探测失败：一律按未登录处理，但要分清是谁答的。连不上后端（断网、后端不在）时
      // 登录页同样打不开，AuthGate 据此放行离线可读的那几条路由；后端答了错误码则不放行。
      // 这里刻意不做 reconcileOfflineOwner(null)：断网不是换人，清了等于把用户自己的离线书目删掉。
      setBackendUnreachable(isBackendUnreachable(error));
      setUser(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  // 会话过期的全局兜底：apiClient 上任何请求返回 401（登录/状态探测除外）即清空登录态，回到登录页。
  // 全站 API 调用统一走 apiClient（ESLint 禁止直接使用 axios），故只需在此实例挂拦截器。
  useEffect(() => {
    const id = apiClient.interceptors.response.use(
      (r) => r,
      (error: unknown) => {
        if (isAxiosError(error) && error.response?.status === 401) {
          const url = error.config?.url ?? '';
          if (!url.includes('/api/auth/login') && !url.includes('/api/auth/status')) {
            reconcileOfflineOwner(null);
            setCsrfToken('');
            setUser(null);
            // 收到 401 说明后端就在那儿并且拒绝了这次会话，不是断网。
            setBackendUnreachable(false);
          }
        }
        return Promise.reject(error);
      },
    );
    return () => apiClient.interceptors.response.eject(id);
  }, []);

  const setup = useCallback(async (username: string, password: string, displayName: string) => {
    try {
      const { data } = await apiClient.post<SessionResponse>('/api/auth/setup', {
        username,
        password,
        display_name: displayName,
      });
      applySession(data);
    } catch (error) {
      // 原始错误挂到 cause 上：getApiErrorMessage 只取出给用户看的那句话，
      // 而状态码、响应体这些排查要用的信息都在原始的 axios 错误里。
      throw new Error(getApiErrorMessage(error, 'setup failed'), { cause: error });
    }
  }, [applySession]);

  const login = useCallback(async (username: string, password: string) => {
    try {
      const { data } = await apiClient.post<SessionResponse>('/api/auth/login', { username, password });
      applySession(data);
    } catch (error) {
      // 原始错误挂到 cause 上：getApiErrorMessage 只取出给用户看的那句话，
      // 而状态码、响应体这些排查要用的信息都在原始的 axios 错误里。
      throw new Error(getApiErrorMessage(error, 'login failed'), { cause: error });
    }
  }, [applySession]);

  const logout = useCallback(async () => {
    try {
      await apiClient.post('/api/auth/logout');
    } catch {
      // 忽略登出错误：无论如何都清本地态。
    }
    setCsrfToken('');
    setUser(null);
    // 清掉本地的离线阅读残留（待同步队列 + 书目索引）。
    //
    // 注意这只是四条「换人」路径中的一条，而且是最不常发生的一条——共享设备上更常见的是
    // 上一个人直接关窗口。另外三条（登录/刷新/会话过期）在 applySession、refresh
    // 与 401 拦截器里各自对账，见 reconcileOfflineOwner。
    try {
      reconcileOfflineOwner(null);
    } catch {
      // localStorage 不可用（隐私模式/配额）时忽略：登出本身不应因此失败。
    }
  }, []);

  const changePassword = useCallback(async (currentPassword: string, newPassword: string) => {
    try {
      const { data } = await apiClient.post<{ csrf_token?: string }>('/api/auth/change-password', {
        current_password: currentPassword,
        new_password: newPassword,
      });
      if (data.csrf_token) setCsrfToken(data.csrf_token);
      await refresh();
    } catch (error) {
      // 原始错误挂到 cause 上：getApiErrorMessage 只取出给用户看的那句话，
      // 而状态码、响应体这些排查要用的信息都在原始的 axios 错误里。
      throw new Error(getApiErrorMessage(error, 'change password failed'), { cause: error });
    }
  }, [refresh]);

  const value = useMemo<AuthContextValue>(() => ({
    loading,
    setupRequired,
    user,
    isAdmin: user?.role === 'admin',
    backendUnreachable,
    refresh,
    setup,
    login,
    logout,
    changePassword,
  }), [loading, setupRequired, user, backendUnreachable, refresh, setup, login, logout, changePassword]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}

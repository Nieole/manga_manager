/**
 * 业务说明：本文件是前端多用户鉴权的全局状态层。应用启动时探测站点初始化与登录态
 * （GET /api/auth/status），并提供 setup / login / logout / changePassword 等动作。
 * 登录成功后把后端下发的 CSRF 令牌交给 apiAuth（供改写类请求携带），会话本身走 HttpOnly Cookie。
 * 还安装了 401 响应拦截：会话过期时清空登录态，交由 AuthGate 回到登录页。
 * 维护要点：CSRF 令牌只存内存；密码一律由用户在自己的界面输入，前端从不缓存明文。
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { apiClient, getApiErrorMessage, isAxiosError } from '../api/client';
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

  const applySession = useCallback((data: SessionResponse) => {
    setCsrfToken(data.csrf_token);
    setUser(data.user);
    setSetupRequired(false);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const { data } = await apiClient.get<AuthStatusResponse>('/api/auth/status');
      setSetupRequired(data.setup_required);
      if (data.authenticated && data.user) {
        setCsrfToken(data.csrf_token ?? '');
        setUser(data.user);
      } else {
        setCsrfToken('');
        setUser(null);
      }
    } catch {
      // 状态探测失败（后端不可达等）：按未登录处理，交由界面提示。
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
            setCsrfToken('');
            setUser(null);
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
      throw new Error(getApiErrorMessage(error, 'setup failed'));
    }
  }, [applySession]);

  const login = useCallback(async (username: string, password: string) => {
    try {
      const { data } = await apiClient.post<SessionResponse>('/api/auth/login', { username, password });
      applySession(data);
    } catch (error) {
      throw new Error(getApiErrorMessage(error, 'login failed'));
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
      throw new Error(getApiErrorMessage(error, 'change password failed'));
    }
  }, [refresh]);

  const value = useMemo<AuthContextValue>(() => ({
    loading,
    setupRequired,
    user,
    isAdmin: user?.role === 'admin',
    refresh,
    setup,
    login,
    logout,
    changePassword,
  }), [loading, setupRequired, user, refresh, setup, login, logout, changePassword]);

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}

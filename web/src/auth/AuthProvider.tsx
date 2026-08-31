/**
 * 本文件是前端多用户鉴权的全局状态层。应用启动时探测站点初始化与登录态（GET /api/auth/status），
 * 并提供 setup / login / logout / changePassword；探测连不上后端时另报 backendUnreachable。
 * 登录成功后把后端下发的 CSRF 令牌交给 apiAuth（供改写类请求携带），会话本身走 HttpOnly Cookie。
 * 还安装了 401 响应拦截：会话过期时清空登录态，交由 AuthGate 回到登录页。
 * 维护要点：CSRF 令牌只存内存；密码一律由用户在自己的界面输入，前端从不缓存明文。
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { apiClient, getApiErrorMessage, isAxiosError } from '../api/client';
import { reconcileOfflineOwner, releaseOfflineOwner, syncQueuedOfflineProgress } from '../pages/book-reader/offlineReader';
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
        // 后端答「未登录」是会话过期或被踢：30 天到期、管理员重置密码、他在别的设备上改了
        // 密码，都从这条路回来。用户还是同一个人，只是要重新登录，本机的离线书目、缓存字节
        // 与还没回传的进度队列一概不动——清了就是删他自己的数据，且没有恢复路径。
        // 换人隔离由 reconcileOfflineOwner 在「谁登进来了」那一刻兜住，不靠「此刻没人登录」。
        setCsrfToken('');
        setUser(null);
      }
    } catch (error) {
      // 状态探测失败：一律按未登录处理，但要分清是谁答的。连不上后端（断网、后端不在）时
      // 登录页同样打不开，AuthGate 据此放行离线可读的那几条路由；后端答了错误码则不放行。
      // 同样不碰本机离线数据：断网不是换人，清了等于把用户自己的离线书目删掉。
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
            // 与状态探测答「未登录」同一件事：这次会话被拒，不是换了人，本机离线数据不动。
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
    // 先尽力把待回传的进度传上去：登出这一刻会话仍然有效，是队列最后一次能上行的机会。
    // 传不掉的（断网时登出）随后照清——留着它，下一个在这台设备上登录的人会被
    // useReaderOffline 自动把它当成自己的进度传上去，那比丢几页更糟。
    try {
      await syncQueuedOfflineProgress();
    } catch {
      // 回传失败不拦着登出。
    }
    try {
      await apiClient.post('/api/auth/logout');
    } catch {
      // 忽略登出错误：无论如何都清本地态。
    }
    setCsrfToken('');
    setUser(null);
    // 登出是「我要离开这台设备」的明确表态，本机离线残留整份交还。三条不经过登出的换人路径
    //（登录/setup、刷新页面状态探测、上一个人直接关窗口）由 reconcileOfflineOwner 对账。
    try {
      releaseOfflineOwner();
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

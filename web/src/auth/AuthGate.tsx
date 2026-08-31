/**
 * 本文件是鉴权闸门，包裹整个路由树。按 AuthProvider 的全局态决定渲染什么：
 * 加载中 → 骨架；尚无账户 → 首次建管理员；未登录 → 登录页；须改密 → 强制改密页；否则放行业务路由。
 * 阅读器等脱离主 Layout 的路由也在此闸门之内，确保全站统一强制登录。
 * 维护要点：鉴权页用 Suspense 懒加载；连不上后端时按 isOfflineReadableRoute 放行离线可读的路由。
 */

import { Suspense, lazy, type ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { useLocation } from 'react-router-dom';
import { useAuth } from './AuthProvider';
import { isOfflineReadableRoute } from './offlineAccess';

const LoginPage = lazy(() => import('../pages/auth/LoginPage'));
const SetupPage = lazy(() => import('../pages/auth/SetupPage'));
const ForcePasswordChangePage = lazy(() => import('../pages/auth/ForcePasswordChangePage'));

function AuthFallback() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-komgaBackground">
      <Loader2 className="h-6 w-6 animate-spin text-komgaPrimary" />
    </div>
  );
}

export function AuthGate({ children }: { children: ReactNode }) {
  const { loading, setupRequired, user, backendUnreachable } = useAuth();
  const { pathname } = useLocation();

  if (loading) return <AuthFallback />;

  // 连不上后端时不能一律送去登录页：登录同样要网络，用户在飞机上刷新一次就再也读不到
  // 自己下载好的书。放行范围仅限本机已有字节的那几条路由，别的页面没有后端也无内容可看。
  const offlinePass = backendUnreachable && isOfflineReadableRoute(pathname);

  let gate: ReactNode | null = null;
  if (setupRequired) gate = <SetupPage />;
  else if (!user) gate = offlinePass ? null : <LoginPage />;
  else if (user.must_change_password) gate = <ForcePasswordChangePage />;

  if (gate) return <Suspense fallback={<AuthFallback />}>{gate}</Suspense>;
  return <>{children}</>;
}

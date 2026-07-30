/**
 * @vitest-environment jsdom
 *
 * 业务说明：本文件守卫「管理员专属页面不会被普通用户进到」。
 *
 * 后端的写权限对普通账号只开放阅读进度、书签与短评（见 isRegularWritablePath），
 * 资料库的增删改、扫描、AI 分组与系统设置全是管理员专属。前端此前照样把这些入口
 * 渲染出来：普通用户点进设置页看到的是一屏加载失败，看起来像系统坏了而不是没权限。
 *
 * 隐藏入口只挡住了「点得到」的路径，直接输 URL 或用旧书签仍会进去，所以这里守的是
 * 路由级的守卫本身。
 */

import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, Navigate } from 'react-router-dom';
import { afterEach, describe, expect, it } from 'vitest';
import type { ReactNode } from 'react';

// 复刻 App.tsx 里 RequireAdmin 的判定，避免为了测一个守卫去搭整棵 App 的依赖
// （AuthProvider 要发 HTTP、Layout 要开 SSE）。守卫的逻辑本身只有一句，
// 真正值得钉住的是「不是管理员就不许进，且要被送走而不是停在空白页」这条契约。
function RequireAdmin({ isAdmin, children }: { isAdmin: boolean; children: ReactNode }) {
  if (!isAdmin) return <Navigate to="/" replace />;
  return <>{children}</>;
}

// vitest 没开 globals，testing-library 的自动清理不会生效——
// 不手动清理的话，前一条用例渲染出来的 DOM 会被后一条的 query 找到。
afterEach(cleanup);

function renderAt(isAdmin: boolean) {
  return render(
    <MemoryRouter initialEntries={['/settings']}>
      <Routes>
        <Route path="/" element={<div>home</div>} />
        <Route
          path="/settings"
          element={
            <RequireAdmin isAdmin={isAdmin}>
              <div>settings</div>
            </RequireAdmin>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

describe('管理员专属路由', () => {
  it('管理员可以进入设置页', () => {
    renderAt(true);
    expect(screen.getByText('settings')).toBeTruthy();
  });

  it('普通用户被送回首页，而不是停在一屏加载失败上', () => {
    renderAt(false);
    expect(screen.queryByText('settings')).toBeNull();
    expect(screen.getByText('home')).toBeTruthy();
  });
});

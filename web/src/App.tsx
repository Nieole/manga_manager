import { Suspense, lazy, useEffect, useRef, type ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { Routes, Route, Navigate, useLocation } from 'react-router-dom';
import Layout from './components/Layout';
import { useAuth } from './auth/AuthProvider';
import ErrorBoundary from './components/ErrorBoundary';
import { AuthGate } from './auth/AuthGate';
import { useToast } from './components/ToastProvider';
import { useI18n } from './i18n/LocaleProvider';

const Home = lazy(() => import('./pages/library'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const Stats = lazy(() => import('./pages/Stats'));
const Collections = lazy(() => import('./pages/collections'));
const Organize = lazy(() => import('./pages/Organize'));
const Ops = lazy(() => import('./pages/Ops'));
const ReviewCenter = lazy(() => import('./pages/ReviewCenter'));
const ReadingLists = lazy(() => import('./pages/ReadingLists'));
const OfflineShelf = lazy(() => import('./pages/OfflineShelf'));
const SeriesDetail = lazy(() => import('./pages/series-detail'));
const FranchiseGraphPage = lazy(() => import('./pages/franchise-graph').then(m => ({ default: m.FranchiseGraphPage })));
const BookReader = lazy(() => import('./pages/BookReader'));
const Settings = lazy(() => import('./pages/Settings'));
const SettingsOverviewPage = lazy(() => import('./pages/settings/SettingsOverviewPage').then((module) => ({ default: module.SettingsOverviewPage })));
const SettingsAppearancePage = lazy(() => import('./pages/settings/SettingsAppearancePage').then((module) => ({ default: module.SettingsAppearancePage })));
const SettingsLibraryPage = lazy(() => import('./pages/settings/SettingsLibraryPage').then((module) => ({ default: module.SettingsLibraryPage })));
const SettingsMediaPage = lazy(() => import('./pages/settings/SettingsMediaPage').then((module) => ({ default: module.SettingsMediaPage })));
const SettingsAIPage = lazy(() => import('./pages/settings/SettingsAIPage').then((module) => ({ default: module.SettingsAIPage })));
const SettingsKOReaderPage = lazy(() => import('./pages/settings/SettingsKOReaderPage').then((module) => ({ default: module.SettingsKOReaderPage })));
const SettingsConnectionsPage = lazy(() => import('./pages/settings/SettingsConnectionsPage').then((module) => ({ default: module.SettingsConnectionsPage })));
const SettingsTagsPage = lazy(() => import('./pages/settings/SettingsTagsPage').then((module) => ({ default: module.SettingsTagsPage })));
const SettingsUsersPage = lazy(() => import('./pages/settings/SettingsUsersPage').then((module) => ({ default: module.SettingsUsersPage })));
const SettingsMaintenancePage = lazy(() => import('./pages/settings/SettingsMaintenancePage').then((module) => ({ default: module.SettingsMaintenancePage })));

function RouteFallback() {
  const { t } = useI18n();

  return (
    <div className="flex min-h-[40vh] items-center justify-center px-6">
      <div className="flex items-center gap-3 rounded-2xl border border-gray-800 bg-gray-900/70 px-5 py-4 text-sm text-gray-300 shadow-lg shadow-black/20">
        <Loader2 className="h-4 w-4 animate-spin text-komgaPrimary" />
        <span>{t('common.loading')}</span>
      </div>
    </div>
  );
}

// RouteBoundary 给每个路由元素套一层独立的错误边界。
//
// 只有一个顶层 ErrorBoundary 时，任一页面渲染异常会把整棵树连同 Layout 一起卸载——
// 侧边栏、SSE 长连接、任务进度气泡全没了，用户唯一的恢复手段是整页跳转。
// 按 pathname 作 key：路由切换时重置边界状态，否则一次崩溃会让后续导航一直停在错误页。
function RouteBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  return <ErrorBoundary key={pathname}>{children}</ErrorBoundary>;
}

function withRouteFallback(element: ReactNode) {
  return (
    <RouteBoundary>
      <Suspense fallback={<RouteFallback />}>{element}</Suspense>
    </RouteBoundary>
  );
}

// RequireAdmin 是路由级的管理员守卫，套在「整屏都是管理动作」的页面上（设置、任务与日志、审核中心）。
//
// 隐藏入口只挡住了「点得到」的路径，直接输 URL 或用旧书签仍会进去——那里的每个接口在后端
// 都归 isAdminOnlyPath 管，普通用户看到的是一屏加载失败，像系统坏了。这里把他们送回首页，
// 并明说是权限问题：只跳转不吭声，用户只会以为链接坏了。
// 真正的权限判定仍在后端，这层只负责别让界面自相矛盾。
function RequireAdmin({ children }: { children: ReactNode }) {
  const { isAdmin } = useAuth();
  const { showToast } = useToast();
  const { t } = useI18n();
  // StrictMode 下挂载期 effect 会跑两次，不记一笔就会弹出两条一模一样的提示。
  const notified = useRef(false);

  useEffect(() => {
    if (isAdmin || notified.current) return;
    notified.current = true;
    showToast(t('auth.adminOnly.toast'), 'error');
  }, [isAdmin, showToast, t]);

  if (!isAdmin) return <Navigate to="/" replace />;
  return <>{children}</>;
}

function App() {
  return (
    <ErrorBoundary>
      <AuthGate>
      <Routes>
        <Route path="/" element={<Layout />}>
          {/* 默认首页 - 仪表板 */}
          <Route index element={withRouteFallback(<Dashboard />)} />
          {/* 选择具体 Library 后的系列浏览 */}
          <Route path="library/:libId" element={withRouteFallback(<Home />)} />
          {/* 点击特定系列后展示其中的电子书/卷册 */}
          <Route path="series/:seriesId" element={withRouteFallback(<SeriesDetail />)} />
          {/* 深度统计 */}
          <Route path="stats" element={withRouteFallback(<Stats />)} />
          {/* 合集管理 */}
          <Route path="collections" element={withRouteFallback(<Collections />)} />
          {/* 整理工作台 */}
          <Route path="organize" element={withRouteFallback(<Organize />)} />
          {/* 任务与日志（合并自 BackgroundTasks + Logs）：整屏四个接口全在 /api/system/ 下 */}
          <Route path="ops" element={<RequireAdmin>{withRouteFallback(<Ops />)}</RequireAdmin>} />
          {/* 向后兼容旧路由 */}
          <Route path="organize/tasks" element={<Navigate to="/ops?tab=tasks" replace />} />
          <Route path="logs" element={<Navigate to="/ops?tab=logs" replace />} />
          {/* 审核中心（合并元数据审核 + AI 分组审核）：整屏只有裁决提案这一件事，全要管理员 */}
          <Route path="reviews" element={<RequireAdmin>{withRouteFallback(<ReviewCenter />)}</RequireAdmin>} />
          {/* 向后兼容旧路由 */}
          <Route path="metadata-reviews" element={<Navigate to="/reviews?tab=metadata" replace />} />
          <Route path="ai-grouping-reviews" element={<Navigate to="/reviews?tab=ai-grouping" replace />} />
          {/* 有序阅读清单 */}
          <Route path="reading-lists" element={withRouteFallback(<ReadingLists />)} />
          {/* 离线书架 */}
          <Route path="offline" element={withRouteFallback(<OfflineShelf />)} />
          {/* 系统配置中心 */}
          <Route path="settings" element={<RequireAdmin>{withRouteFallback(<Settings />)}</RequireAdmin>}>
            <Route index element={withRouteFallback(<SettingsOverviewPage />)} />
            <Route path="appearance" element={withRouteFallback(<SettingsAppearancePage />)} />
            <Route path="library" element={withRouteFallback(<SettingsLibraryPage />)} />
            <Route path="media" element={withRouteFallback(<SettingsMediaPage />)} />
            <Route path="ai" element={withRouteFallback(<SettingsAIPage />)} />
            <Route path="koreader" element={withRouteFallback(<SettingsKOReaderPage />)} />
            <Route path="connections" element={withRouteFallback(<SettingsConnectionsPage />)} />
            <Route path="tags" element={withRouteFallback(<SettingsTagsPage />)} />
            <Route path="users" element={withRouteFallback(<SettingsUsersPage />)} />
            <Route path="maintenance" element={withRouteFallback(<SettingsMaintenancePage />)} />
          </Route>
        </Route>

        {/* 阅读器作为需要接管全屏沉浸体验的独立路由，跳过常规 Layout */}
        <Route path="/reader/:bookId" element={withRouteFallback(<BookReader />)} />
        
        {/* 系列/库关系图谱 */}
        <Route path="/series/:id/franchise-graph" element={withRouteFallback(<FranchiseGraphPage />)} />
        <Route path="/libraries/:libId/franchise-graph" element={withRouteFallback(<FranchiseGraphPage />)} />

        {/* 404 Catcher */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
      </AuthGate>
    </ErrorBoundary>
  );
}

export default App;

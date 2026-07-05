/**
 * 业务说明：本文件是业务实现，属于前端应用路由入口，负责组织资料库、阅读器、系列、设置和任务等页面的导航关系。
 * 它定义用户从一个业务场景进入另一个场景的路径，是前端页面编排的中心。
 * 维护时应关注路由参数兼容、布局嵌套、错误兜底和页面级状态传递。
 */

import { Suspense, lazy, type ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import ErrorBoundary from './components/ErrorBoundary';
import { AuthGate } from './auth/AuthGate';
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

function withRouteFallback(element: ReactNode) {
  return <Suspense fallback={<RouteFallback />}>{element}</Suspense>;
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
          {/* 任务与日志（合并自 BackgroundTasks + Logs） */}
          <Route path="ops" element={withRouteFallback(<Ops />)} />
          {/* 向后兼容旧路由 */}
          <Route path="organize/tasks" element={<Navigate to="/ops?tab=tasks" replace />} />
          <Route path="logs" element={<Navigate to="/ops?tab=logs" replace />} />
          {/* 审核中心（合并元数据审核 + AI 分组审核） */}
          <Route path="reviews" element={withRouteFallback(<ReviewCenter />)} />
          {/* 向后兼容旧路由 */}
          <Route path="metadata-reviews" element={<Navigate to="/reviews?tab=metadata" replace />} />
          <Route path="ai-grouping-reviews" element={<Navigate to="/reviews?tab=ai-grouping" replace />} />
          {/* 有序阅读清单 */}
          <Route path="reading-lists" element={withRouteFallback(<ReadingLists />)} />
          {/* 离线书架 */}
          <Route path="offline" element={withRouteFallback(<OfflineShelf />)} />
          {/* 系统配置中心 */}
          <Route path="settings" element={withRouteFallback(<Settings />)}>
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

import { Suspense, lazy, type ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import ErrorBoundary from './components/ErrorBoundary';
import { useI18n } from './i18n/LocaleProvider';

const Home = lazy(() => import('./pages/Home'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const Collections = lazy(() => import('./pages/Collections'));
const SeriesDetail = lazy(() => import('./pages/SeriesDetail'));
const BookReader = lazy(() => import('./pages/BookReader'));
const Settings = lazy(() => import('./pages/Settings'));
const Logs = lazy(() => import('./pages/Logs'));
const SettingsOverviewPage = lazy(() => import('./pages/settings/SettingsOverviewPage').then((module) => ({ default: module.SettingsOverviewPage })));
const SettingsAppearancePage = lazy(() => import('./pages/settings/SettingsAppearancePage').then((module) => ({ default: module.SettingsAppearancePage })));
const SettingsLibraryPage = lazy(() => import('./pages/settings/SettingsLibraryPage').then((module) => ({ default: module.SettingsLibraryPage })));
const SettingsMediaPage = lazy(() => import('./pages/settings/SettingsMediaPage').then((module) => ({ default: module.SettingsMediaPage })));
const SettingsAIPage = lazy(() => import('./pages/settings/SettingsAIPage').then((module) => ({ default: module.SettingsAIPage })));
const SettingsKOReaderPage = lazy(() => import('./pages/settings/SettingsKOReaderPage').then((module) => ({ default: module.SettingsKOReaderPage })));
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
      <Routes>
        <Route path="/" element={<Layout />}>
          {/* 默认首页 - 仪表板 */}
          <Route index element={withRouteFallback(<Dashboard />)} />
          {/* 选择具体 Library 后的系列浏览 */}
          <Route path="library/:libId" element={withRouteFallback(<Home />)} />
          {/* 点击特定系列后展示其中的电子书/卷册 */}
          <Route path="series/:seriesId" element={withRouteFallback(<SeriesDetail />)} />
          {/* 合集管理 */}
          <Route path="collections" element={withRouteFallback(<Collections />)} />
          {/* 系统日志 */}
          <Route path="logs" element={withRouteFallback(<Logs />)} />
          {/* 系统配置中心 */}
          <Route path="settings" element={withRouteFallback(<Settings />)}>
            <Route index element={withRouteFallback(<SettingsOverviewPage />)} />
            <Route path="appearance" element={withRouteFallback(<SettingsAppearancePage />)} />
            <Route path="library" element={withRouteFallback(<SettingsLibraryPage />)} />
            <Route path="media" element={withRouteFallback(<SettingsMediaPage />)} />
            <Route path="ai" element={withRouteFallback(<SettingsAIPage />)} />
            <Route path="koreader" element={withRouteFallback(<SettingsKOReaderPage />)} />
            <Route path="maintenance" element={withRouteFallback(<SettingsMaintenancePage />)} />
          </Route>
        </Route>

        {/* 阅读器作为需要接管全屏沉浸体验的独立路由，跳过常规 Layout */}
        <Route path="/reader/:bookId" element={withRouteFallback(<BookReader />)} />

        {/* 404 Catcher */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </ErrorBoundary>
  );
}

export default App;

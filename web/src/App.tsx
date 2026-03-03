import { Routes, Route, Navigate } from 'react-router-dom';
import Layout from './components/Layout';
import ErrorBoundary from './components/ErrorBoundary';
import Home from './pages/Home';
import Dashboard from './pages/Dashboard';
import Collections from './pages/Collections';
import SeriesDetail from './pages/SeriesDetail';
import BookReader from './pages/BookReader';
import Settings from './pages/Settings';
import Logs from './pages/Logs';

function App() {
  return (
    <ErrorBoundary>
      <Routes>
        <Route path="/" element={<Layout />}>
          {/* 默认首页 - 仪表板 */}
          <Route index element={<Dashboard />} />
          {/* 选择具体 Library 后的系列浏览 */}
          <Route path="library/:libId" element={<Home />} />
          {/* 点击特定系列后展示其中的电子书/卷册 */}
          <Route path="series/:seriesId" element={<SeriesDetail />} />
          {/* 合集管理 */}
          <Route path="collections" element={<Collections />} />
          {/* 系统日志 */}
          <Route path="logs" element={<Logs />} />
          {/* 系统配置中心 */}
          <Route path="settings" element={<Settings />} />
        </Route>

        {/* 阅读器作为需要接管全屏沉浸体验的独立路由，跳过常规 Layout */}
        <Route path="/reader/:bookId" element={<BookReader />} />

        {/* 404 Catcher */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </ErrorBoundary>
  );
}

export default App;

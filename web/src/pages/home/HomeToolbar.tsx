import { ArrowDown, ArrowUp } from 'lucide-react';

interface HomeToolbarProps {
  totalSeries: number;
  hasSeries: boolean;
  isSelectionMode: boolean;
  allCurrentPageSelected?: boolean;
  selectedCount?: number;
  sortByField: string;
  sortDir: string;
  onToggleSelectionMode: () => void;
  onToggleSelectCurrentPage?: () => void;
  onSortFieldChange: (value: string) => void;
  onToggleSortDir: () => void;
}

export function HomeToolbar({
  totalSeries,
  hasSeries,
  isSelectionMode,
  allCurrentPageSelected = false,
  selectedCount = 0,
  sortByField,
  sortDir,
  onToggleSelectionMode,
  onToggleSelectCurrentPage,
  onSortFieldChange,
  onToggleSortDir,
}: HomeToolbarProps) {
  return (
    <div className="mb-6 flex flex-col sm:flex-row sm:justify-between sm:items-end gap-4 border-b border-gray-800/30 pb-4">
      <div>
        <h2 className="text-2xl sm:text-3xl font-bold text-white tracking-tight mb-1">浏览系列</h2>
        <p className="text-gray-400 text-xs sm:text-sm">资源库返回 {totalSeries} 个结果</p>
      </div>
      <div className="flex flex-wrap items-center gap-2 sm:gap-3 mt-4 sm:mt-0 w-full sm:w-auto justify-between sm:justify-end">
        {hasSeries && (
          <button
            onClick={onToggleSelectionMode}
            className={`px-3 py-1.5 text-xs sm:text-sm font-medium rounded-lg transition-colors border focus:outline-none flex-shrink-0 ${isSelectionMode ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-transparent border-white/10 text-gray-400 hover:border-white/20 hover:text-white'}`}
          >
            {isSelectionMode ? '取消选择' : '批量操作'}
          </button>
        )}
        {isSelectionMode && hasSeries && onToggleSelectCurrentPage && (
          <button
            onClick={onToggleSelectCurrentPage}
            className="px-3 py-1.5 text-xs sm:text-sm font-medium rounded-lg transition-colors border border-white/10 text-gray-300 hover:border-white/20 hover:text-white bg-transparent"
          >
            {allCurrentPageSelected ? '取消本页' : '全选本页'}
          </button>
        )}
        {isSelectionMode && selectedCount > 0 && (
          <span className="text-xs sm:text-sm text-komgaPrimary font-medium px-2">
            已选 {selectedCount} 项
          </span>
        )}
        <span className="text-xs sm:text-sm text-gray-400 font-medium ml-auto sm:ml-0">排序方式</span>
        <select
          value={sortByField}
          onChange={(e) => onSortFieldChange(e.target.value)}
          className="bg-komgaSurface border border-white/10 text-gray-100 text-sm rounded-lg focus:ring-komgaPrimary focus:border-komgaPrimary block p-2 outline-none transition-colors cursor-pointer hover:border-white/20 shadow-sm"
        >
          <option value="name">名称</option>
          <option value="created">入库时间</option>
          <option value="updated">最新更新</option>
          <option value="rating">评分</option>
          <option value="volumes">卷数量</option>
          <option value="books">册数量</option>
          <option value="pages">总页数</option>
          <option value="read">已读进度</option>
          <option value="favorite">收藏状态</option>
        </select>
        <button
          onClick={onToggleSortDir}
          className="p-2 bg-komgaSurface border border-white/10 hover:border-white/20 rounded-lg text-gray-400 hover:text-komgaPrimary transition-colors flex items-center justify-center shadow-sm"
          title={sortDir === 'asc' ? '当前正序 (点击切换倒序)' : '当前倒序 (点击切换正序)'}
        >
          {sortDir === 'asc' ? <ArrowUp className="w-5 h-5" /> : <ArrowDown className="w-5 h-5" />}
        </button>
      </div>
    </div>
  );
}

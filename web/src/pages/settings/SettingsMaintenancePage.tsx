import { AlertTriangle } from 'lucide-react';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro, sectionClassName } from './shared';

export function SettingsMaintenancePage() {
  const { handleAction } = useSettings();

  return (
    <div className="space-y-6">
      <SettingsPageIntro title="维护工具" description="这些操作会立即触发后台任务，不属于持久配置。适合用于索引恢复、缩略图重建和批量元数据维护。" />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-red-400">
          <AlertTriangle className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">后台维护任务</h3>
        </div>
        <div className="grid gap-3 md:grid-cols-3">
          <button onClick={() => handleAction('/api/system/rebuild-index', '搜索索引已重建')} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">重建搜索索引</p>
            <p className="mt-1 text-xs text-red-200/80">适合搜索结果异常、索引损坏或切换分词策略后执行。</p>
          </button>
          <button onClick={() => handleAction('/api/system/rebuild-thumbnails', '缩略图重建已启动')} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">重建缩略图缓存</p>
            <p className="mt-1 text-xs text-red-200/80">会触发大量磁盘 IO，适合封面损坏或切换缓存格式后执行。</p>
          </button>
          <button onClick={() => handleAction('/api/system/batch-scrape', '批量元数据刮削已启动')} className="rounded-xl border border-red-500/20 bg-red-500/10 px-4 py-4 text-left text-red-200 hover:bg-red-500/15">
            <p className="font-medium">批量元数据刮削</p>
            <p className="mt-1 text-xs text-red-200/80">会持续占用 LLM 或外部数据源，请优先在空闲时段运行。</p>
          </button>
        </div>
      </section>
    </div>
  );
}

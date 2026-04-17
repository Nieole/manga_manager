import { AlertTriangle, CheckCircle2, FolderOpen, HardDrive, Palette, Settings as SettingsIcon, Sparkles, TabletSmartphone, Wrench } from 'lucide-react';
import { useOutletContext } from 'react-router-dom';
import { useTheme } from '../../theme/ThemeProvider';
import { useSettings } from './SettingsContext';
import { SettingsPageIntro } from './shared';

export function SettingsOverviewPage() {
  const { validation, config, koreaderStatus, capabilities } = useSettings();
  const { resolvedTheme } = useTheme();
  const { navigateSettingsSection } = useOutletContext<{ navigateSettingsSection: (path: string) => void }>();

  const cards = [
    {
      title: '外观',
      description: `当前主题：${resolvedTheme.name}`,
      icon: <Palette className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/appearance'),
    },
    {
      title: '库与扫描',
      description: `${config?.library.paths?.length ?? 0} 个绑定目录，扫描格式 ${capabilities?.default_scan_formats ?? '载入中'}`,
      icon: <FolderOpen className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/library'),
    },
    {
      title: '图片与缓存',
      description: `缓存目录 ${config?.cache.dir || '未配置'}`,
      icon: <HardDrive className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/media'),
    },
    {
      title: 'AI / 元数据',
      description: `${config?.llm.provider || '未配置'} · ${config?.llm.model || '未设置模型'}`,
      icon: <Sparkles className="h-5 w-5 text-komgaPrimary" />,
      action: () => navigateSettingsSection('/settings/ai'),
    },
    {
      title: 'KOReader',
      description: koreaderStatus?.enabled ? `服务已启用，${koreaderStatus.enabled_account_count}/${koreaderStatus.account_count} 个账号可用` : '服务未启用',
      icon: <TabletSmartphone className="h-5 w-5 text-sky-400" />,
      action: () => navigateSettingsSection('/settings/koreader'),
    },
    {
      title: '维护工具',
      description: '索引、缩略图和批量元数据任务入口',
      icon: <Wrench className="h-5 w-5 text-red-400" />,
      action: () => navigateSettingsSection('/settings/maintenance'),
    },
  ];

  return (
    <div className="space-y-6">
      <SettingsPageIntro
        title="设置概览"
        description="按场景拆分后的设置入口。先看健康状态和当前配置概况，再进入对应设置页处理。"
        badge={
          <div
            className={`inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-sm ${
              validation.valid ? 'border border-emerald-500/20 bg-emerald-500/10 text-emerald-300' : 'border border-amber-500/20 bg-amber-500/10 text-amber-300'
            }`}
          >
            {validation.valid ? <CheckCircle2 className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />}
            {validation.valid ? '配置健康' : `待修正 ${validation.issues.length} 项`}
          </div>
        }
      />

      {!validation.valid && (
        <div className="rounded-2xl border border-amber-500/20 bg-amber-500/10 p-4">
          <p className="text-sm font-medium text-amber-100">当前仍有阻塞保存的问题。</p>
          <div className="mt-2 space-y-1">
            {validation.issues.slice(0, 5).map((issue) => (
              <p key={`${issue.field}-${issue.message}`} className="text-sm text-amber-200/90">
                {issue.field}: {issue.message}
              </p>
            ))}
          </div>
        </div>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {cards.map((card) => (
          <button
            key={card.title}
            type="button"
            onClick={card.action}
            className="rounded-2xl border border-gray-800 bg-komgaSurface/80 p-5 text-left transition-all hover:border-gray-700 hover:bg-gray-900/70"
          >
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-white/10 bg-white/[0.03]">
                {card.icon}
              </div>
              <div>
                <p className="text-base font-semibold text-white">{card.title}</p>
                <p className="mt-1 text-sm leading-6 text-gray-400">{card.description}</p>
              </div>
            </div>
          </button>
        ))}
      </div>

      <div className="rounded-2xl border border-gray-800 bg-komgaSurface p-6">
        <div className="flex items-center gap-2 text-komgaPrimary">
          <SettingsIcon className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">当前全局状态</h3>
        </div>
        <div className="mt-4 grid gap-4 md:grid-cols-3">
          <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-4">
            <p className="text-sm text-gray-400">已绑定目录</p>
            <p className="mt-2 text-2xl font-bold text-white">{config?.library.paths?.length ?? 0}</p>
          </div>
          <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-4">
            <p className="text-sm text-gray-400">KOReader 未匹配记录</p>
            <p className="mt-2 text-2xl font-bold text-white">{koreaderStatus?.stats.unmatched_progress_count ?? 0}</p>
          </div>
          <div className="rounded-xl border border-gray-800 bg-gray-900/40 p-4">
            <p className="text-sm text-gray-400">当前主题</p>
            <p className="mt-2 text-2xl font-bold text-white">{resolvedTheme.name}</p>
          </div>
        </div>
      </div>
    </div>
  );
}

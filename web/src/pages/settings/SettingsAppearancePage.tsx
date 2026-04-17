import { Palette } from 'lucide-react';
import { useTheme } from '../../theme/ThemeProvider';
import { SettingsPageIntro } from './shared';

function ThemeSection({
  title,
  description,
  themes,
  themeId,
  onSelect,
}: {
  title: string;
  description: string;
  themes: ReturnType<typeof useTheme>['availableThemes'];
  themeId: string;
  onSelect: (themeId: string) => void;
}) {
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h3 className="text-lg font-semibold text-white">{title}</h3>
          <p className="mt-1 text-sm leading-6 text-gray-400">{description}</p>
        </div>
        <span className="rounded-full border border-gray-700 bg-gray-900/60 px-3 py-1 text-xs font-medium uppercase tracking-[0.2em] text-gray-300">
          {themes.length} 套
        </span>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {themes.map((theme) => {
          const selected = theme.id === themeId;
          return (
            <button
              key={theme.id}
              type="button"
              onClick={() => onSelect(theme.id)}
              className={`rounded-2xl border p-5 text-left transition-all ${
                selected ? 'border-komgaPrimary bg-komgaPrimary/10 shadow-lg shadow-komgaPrimary/10' : 'border-gray-800 bg-komgaSurface hover:border-gray-700 hover:bg-gray-900/70'
              }`}
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="text-lg font-semibold text-white">{theme.name}</p>
                  <p className="mt-2 text-sm leading-6 text-gray-400">{theme.description}</p>
                </div>
                {selected && <span className="shrink-0 whitespace-nowrap rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2 py-1 text-[11px] font-medium text-komgaPrimary">当前</span>}
              </div>
              <div className="mt-5 flex items-center gap-2">
                {theme.swatches.map((swatch) => (
                  <span key={swatch} className="h-9 w-9 rounded-full border border-white/10 shadow-inner shadow-black/20" style={{ backgroundColor: swatch }} />
                ))}
              </div>
            </button>
          );
        })}
      </div>
    </section>
  );
}

export function SettingsAppearancePage() {
  const { themeId, resolvedTheme, lightThemes, darkThemes, setTheme } = useTheme();

  return (
    <div className="space-y-6">
      <SettingsPageIntro
        title="外观 / 主题"
        description="主题只保存在当前浏览器。阅读器默认跟随应用主题，阅读模式和图像处理偏好保持原有本地存储。"
        badge={
          <div className="inline-flex items-center gap-2 rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-3 py-1.5 text-sm text-komgaPrimary">
            <Palette className="h-4 w-4" />
            当前主题：{resolvedTheme.name} · {resolvedTheme.colorScheme === 'light' ? '浅色' : '深色'}
          </div>
        }
      />

      <ThemeSection
        title="浅色主题"
        description="适合白天环境和管理型操作，整体更接近文档和桌面工具。"
        themes={lightThemes}
        themeId={themeId}
        onSelect={setTheme}
      />

      <ThemeSection
        title="深色主题"
        description="适合夜间使用和沉浸式浏览，保留更强的层次和氛围感。"
        themes={darkThemes}
        themeId={themeId}
        onSelect={setTheme}
      />
    </div>
  );
}

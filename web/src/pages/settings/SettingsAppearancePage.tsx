import { Palette } from 'lucide-react';
import { useTheme } from '../../theme/ThemeProvider';
import { SettingsPageIntro } from './shared';

export function SettingsAppearancePage() {
  const { themeId, resolvedTheme, availableThemes, setTheme } = useTheme();

  return (
    <div className="space-y-6">
      <SettingsPageIntro
        title="外观 / 主题"
        description="主题只保存在当前浏览器。阅读器默认跟随应用主题，阅读模式和图像处理偏好保持原有本地存储。"
        badge={
          <div className="inline-flex items-center gap-2 rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-3 py-1.5 text-sm text-komgaPrimary">
            <Palette className="h-4 w-4" />
            当前主题：{resolvedTheme.name}
          </div>
        }
      />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {availableThemes.map((theme) => {
          const selected = theme.id === themeId;
          return (
            <button
              key={theme.id}
              type="button"
              onClick={() => setTheme(theme.id)}
              className={`rounded-2xl border p-5 text-left transition-all ${
                selected ? 'border-komgaPrimary bg-komgaPrimary/10 shadow-lg shadow-komgaPrimary/10' : 'border-gray-800 bg-komgaSurface hover:border-gray-700 hover:bg-gray-900/70'
              }`}
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <p className="text-lg font-semibold text-white">{theme.name}</p>
                  <p className="mt-2 text-sm leading-6 text-gray-400">{theme.description}</p>
                </div>
                {selected && <span className="rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2 py-1 text-[11px] font-medium text-komgaPrimary">当前</span>}
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
    </div>
  );
}

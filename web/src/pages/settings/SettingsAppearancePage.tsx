import { Palette } from 'lucide-react';
import { useTheme } from '../../theme/ThemeProvider';
import { useI18n } from '../../i18n/LocaleProvider';
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
  const { t } = useI18n();

  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h3 className="text-lg font-semibold text-white">{title}</h3>
          <p className="mt-1 text-sm leading-6 text-gray-400">{description}</p>
        </div>
        <span className="rounded-full border border-gray-700 bg-gray-900/60 px-3 py-1 text-xs font-medium uppercase tracking-[0.2em] text-gray-300">
          {t('settings.appearance.themeCount', { count: themes.length })}
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
                  <p className="text-lg font-semibold text-white">{t(theme.nameKey)}</p>
                  <p className="mt-2 text-sm leading-6 text-gray-400">{t(theme.descriptionKey)}</p>
                </div>
                {selected && <span className="shrink-0 whitespace-nowrap rounded-full border border-komgaPrimary/30 bg-komgaPrimary/10 px-2 py-1 text-[11px] font-medium text-komgaPrimary">{t('settings.appearance.current')}</span>}
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
  const { locale, locales, setLocale, t } = useI18n();

  return (
    <div className="space-y-6">
      <SettingsPageIntro
        title={t('settings.appearance.title')}
        description={t('settings.appearance.description')}
        badge={
          <div className="inline-flex items-center gap-2 rounded-full border border-komgaPrimary/20 bg-komgaPrimary/10 px-3 py-1.5 text-sm text-komgaPrimary">
            <Palette className="h-4 w-4" />
            {t('settings.appearance.currentTheme', {
              name: t(resolvedTheme.nameKey),
              scheme: resolvedTheme.colorScheme === 'light' ? t('settings.appearance.light') : t('settings.appearance.dark'),
            })}
          </div>
        }
      />

      <section className="space-y-4 rounded-3xl border border-white/5 bg-komgaSurface/70 p-6 backdrop-blur-sm">
        <div>
          <h3 className="text-lg font-semibold text-white">{t('settings.appearance.languageTitle')}</h3>
          <p className="mt-1 text-sm leading-6 text-gray-400">{t('settings.appearance.languageDescription')}</p>
        </div>
        <div className="flex flex-wrap gap-3">
          {locales.map((option) => {
            const selected = option === locale;
            return (
              <button
                key={option}
                type="button"
                onClick={() => setLocale(option)}
                className={`rounded-2xl border px-4 py-3 text-sm transition-all ${
                  selected
                    ? 'border-komgaPrimary bg-komgaPrimary/10 text-komgaPrimary'
                    : 'border-gray-800 bg-gray-900/40 text-gray-300 hover:border-gray-700 hover:text-white'
                }`}
              >
                {t(`common.locale.${option}`)}
              </button>
            );
          })}
        </div>
      </section>

      <ThemeSection
        title={t('settings.appearance.lightThemes')}
        description={t('settings.appearance.lightThemesDescription')}
        themes={lightThemes}
        themeId={themeId}
        onSelect={setTheme}
      />

      <ThemeSection
        title={t('settings.appearance.darkThemes')}
        description={t('settings.appearance.darkThemesDescription')}
        themes={darkThemes}
        themeId={themeId}
        onSelect={setTheme}
      />
    </div>
  );
}

import { HardDrive, Image as ImageIcon } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsMediaPage() {
  const { t } = useI18n();
  const { config, setConfig, fieldErrors, saving, saveConfig } = useSettings();

  if (!config) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settings.media.title')} description={t('settings.media.description')} />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <HardDrive className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.media.cacheTitle')}</h3>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.media.cacheDir')}</label>
            <input
              type="text"
              value={config.cache.dir}
              onChange={(e) => setConfig({ ...config, cache: { ...config.cache, dir: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('cache.dir')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.media.thumbnailFormat')}</label>
            <select
              value={config.scanner.thumbnail_format}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, thumbnail_format: e.target.value } })}
              className={inputClassName}
            >
              <option value="webp">{t('settings.media.format.webp')}</option>
              <option value="avif">{t('settings.media.format.avif')}</option>
              <option value="jpg">{t('settings.media.format.jpg')}</option>
            </select>
            <FieldErrors messages={fieldErrors('scanner.thumbnail_format')} />
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <ImageIcon className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.media.upscaleTitle')}</h3>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.media.waifu2x')}</label>
            <input
              type="text"
              value={config.scanner.waifu2x_path}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, waifu2x_path: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('scanner.waifu2x_path')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.media.realcugan')}</label>
            <input
              type="text"
              value={config.scanner.realcugan_path}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, realcugan_path: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('scanner.realcugan_path')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.media.aiConcurrency', { count: config.scanner.max_ai_concurrency })}</label>
            <input
              type="range"
              min="1"
              max="10"
              value={config.scanner.max_ai_concurrency}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, max_ai_concurrency: Number(e.target.value) || 1 } })}
              className="w-full accent-komgaPrimary"
            />
            <FieldErrors messages={fieldErrors('scanner.max_ai_concurrency')} />
          </div>
          <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4 text-sm text-gray-300">
            <p className="font-medium text-white">{t('settings.media.instructionsTitle')}</p>
            <p className="mt-1">{t('settings.media.instructions')}</p>
          </div>
        </div>
      </section>

      <SettingsSaveBar saving={saving} label={t('settings.media.saveLabel')} hint={t('settings.media.saveHint')} onSave={() => saveConfig(t('settings.media.saved'))} />
    </div>
  );
}

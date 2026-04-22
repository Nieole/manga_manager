import { FolderOpen, Server } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsLibraryPage() {
  const { t } = useI18n();
  const { config, setConfig, fieldErrors, capabilities, saving, saveConfig } = useSettings();

  if (!config) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title={t('settings.library.title')} description={t('settings.library.description')} />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Server className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.library.serviceTitle')}</h3>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.serverPort')}</label>
            <input
              type="number"
              value={config.server.port}
              onChange={(e) => setConfig({ ...config, server: { ...config.server, port: Number(e.target.value) || 8080 } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('server.port')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.databasePath')}</label>
            <input
              type="text"
              value={config.database.path}
              onChange={(e) => setConfig({ ...config, database: { ...config.database, path: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('database.path')} />
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <FolderOpen className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.library.scanTitle')}</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.scanWorkers', { count: config.scanner.workers })}</label>
            <input
              type="range"
              min="0"
              max="64"
              value={config.scanner.workers}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, workers: Number(e.target.value) || 0 } })}
              className="w-full accent-komgaPrimary"
            />
            <p className="mt-1 text-xs text-gray-500">
              {config.scanner.workers === 0
                ? t('settings.library.scanWorkersAuto')
                : t('settings.library.scanWorkersFixed', { count: config.scanner.workers })}
            </p>
            <FieldErrors messages={fieldErrors('scanner.workers')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.archivePoolSize', { count: config.scanner.archive_pool_size })}</label>
            <input
              type="range"
              min="1"
              max="50"
              value={config.scanner.archive_pool_size}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, archive_pool_size: Number(e.target.value) || 1 } })}
              className="w-full accent-komgaPrimary"
            />
            <p className="mt-1 text-xs text-gray-500">{t('settings.library.archivePoolHint')}</p>
            <FieldErrors messages={fieldErrors('scanner.archive_pool_size')} />
          </div>
        </div>

        <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4 text-sm text-gray-300">
          <p className="font-medium text-white">{t('settings.library.supportedFormats')}</p>
          <p className="mt-1">{capabilities?.default_scan_formats || 'zip,cbz,rar,cbr'}</p>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Server className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.library.logLevelTitle')}</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.logLevel')}</label>
            <select
              value={config.logging.level}
              onChange={(e) => setConfig({ ...config, logging: { level: e.target.value } })}
              className={inputClassName}
            >
              {(capabilities?.supported_log_levels || ['debug', 'info', 'warn', 'error']).map((level) => (
                <option key={level} value={level}>
                  {level.toUpperCase()}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-gray-500">{t('settings.library.logLevelHint')}</p>
            <FieldErrors messages={fieldErrors('logging.level')} />
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <h3 className="text-lg font-semibold text-white">{t('settings.library.boundPaths')}</h3>
        {config.library.paths?.length ? (
          <div className="space-y-2">
            {config.library.paths.map((path) => (
              <div key={path} className="rounded-lg border border-gray-800 bg-gray-900/50 px-3 py-2 text-sm text-gray-300">
                {path}
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-gray-500">{t('settings.library.noBoundPaths')}</p>
        )}
      </section>

      <SettingsSaveBar saving={saving} label={t('settings.library.saveLabel')} hint={t('settings.library.saveHint')} onSave={() => saveConfig(t('settings.library.saved'))} />
    </div>
  );
}

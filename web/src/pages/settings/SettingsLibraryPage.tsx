import { FolderOpen, HardDrive, Server } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsLibraryPage() {
  const { t } = useI18n();
  const { config, setConfig, fieldErrors, capabilities, saving, saveConfig } = useSettings();

  if (!config) return null;
  const ioPolicy = config.library.io_policy || {
    scan_concurrency: 0,
    archive_open_concurrency: 0,
    cover_concurrency: 0,
    hash_concurrency: 0,
    pause_background_when_reading: false,
    idle_only_heavy_tasks: false,
    disable_same_disk_page_cache: false,
  };
  const updateIOPolicy = (patch: Partial<typeof ioPolicy>) =>
    setConfig({
      ...config,
      library: {
        ...config.library,
        io_policy: { ...ioPolicy, ...patch },
      },
    });

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
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.serverHost')}</label>
            <input
              type="text"
              value={config.server.host}
              onChange={(e) => setConfig({ ...config, server: { ...config.server, host: e.target.value } })}
              className={inputClassName}
            />
            <p className="mt-1 text-xs text-gray-500">{t('settings.library.serverHostHint')}</p>
            <FieldErrors messages={fieldErrors('server.host')} />
          </div>
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
          <div className="md:col-span-2">
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.allowedOrigins')}</label>
            <textarea
              value={(config.server.allowed_origins || []).join('\n')}
              onChange={(e) =>
                setConfig({
                  ...config,
                  server: {
                    ...config.server,
                    allowed_origins: e.target.value.split('\n').map((item) => item.trim()).filter(Boolean),
                  },
                })
              }
              className={`${inputClassName} min-h-24 resize-y`}
            />
            <p className="mt-1 text-xs text-gray-500">{t('settings.library.allowedOriginsHint')}</p>
            <FieldErrors messages={fieldErrors('server.allowed_origins')} />
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
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.scanProfile')}</label>
            <select
              value={config.scanner.scan_profile}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, scan_profile: e.target.value } })}
              className={inputClassName}
            >
              {(capabilities?.supported_scan_profiles || ['fast_scan', 'metadata_scan', 'identity_scan', 'repair_scan']).map((profile) => (
                <option key={profile} value={profile}>
                  {t(`settings.library.scanProfile.${profile}`)}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-gray-500">{t(`settings.library.scanProfileHint.${config.scanner.scan_profile}`)}</p>
            <FieldErrors messages={fieldErrors('scanner.scan_profile')} />
          </div>
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
          <HardDrive className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">{t('settings.library.storageTitle')}</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.storageProfile')}</label>
            <select
              value={config.library.storage_profile || 'auto'}
              onChange={(e) =>
                setConfig({
                  ...config,
                  library: { ...config.library, storage_profile: e.target.value },
                })
              }
              className={inputClassName}
            >
              {(capabilities?.supported_storage_profiles || ['auto', 'ssd', 'hdd_external', 'network', 'custom']).map((profile) => (
                <option key={profile} value={profile}>
                  {t(`settings.library.storageProfile.${profile}`)}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-gray-500">{t(`settings.library.storageProfileHint.${config.library.storage_profile || 'auto'}`)}</p>
            <FieldErrors messages={fieldErrors('library.storage_profile')} />
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.archiveOpenConcurrency', { count: ioPolicy.archive_open_concurrency })}</label>
            <input
              type="range"
              min="0"
              max="16"
              value={ioPolicy.archive_open_concurrency}
              onChange={(e) => updateIOPolicy({ archive_open_concurrency: Number(e.target.value) || 0 })}
              className="w-full accent-komgaPrimary"
            />
            <p className="mt-1 text-xs text-gray-500">{t('settings.library.zeroMeansProfileDefault')}</p>
            <FieldErrors messages={fieldErrors('library.io_policy.archive_open_concurrency')} />
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.coverConcurrency', { count: ioPolicy.cover_concurrency })}</label>
            <input
              type="range"
              min="0"
              max="16"
              value={ioPolicy.cover_concurrency}
              onChange={(e) => updateIOPolicy({ cover_concurrency: Number(e.target.value) || 0 })}
              className="w-full accent-komgaPrimary"
            />
            <FieldErrors messages={fieldErrors('library.io_policy.cover_concurrency')} />
          </div>

          <div>
            <label className="mb-1 block text-sm text-gray-400">{t('settings.library.hashConcurrency', { count: ioPolicy.hash_concurrency })}</label>
            <input
              type="range"
              min="0"
              max="16"
              value={ioPolicy.hash_concurrency}
              onChange={(e) => updateIOPolicy({ hash_concurrency: Number(e.target.value) || 0 })}
              className="w-full accent-komgaPrimary"
            />
            <FieldErrors messages={fieldErrors('library.io_policy.hash_concurrency')} />
          </div>
        </div>

        <div className="grid gap-3 md:grid-cols-3">
          {[
            ['pause_background_when_reading', 'settings.library.pauseWhenReading', 'settings.library.pauseWhenReadingHint'],
            ['idle_only_heavy_tasks', 'settings.library.idleOnlyHeavyTasks', 'settings.library.idleOnlyHeavyTasksHint'],
            ['disable_same_disk_page_cache', 'settings.library.disableSameDiskPageCache', 'settings.library.disableSameDiskPageCacheHint'],
          ].map(([key, label, hint]) => {
            const typedKey = key as keyof typeof ioPolicy;
            const enabled = Boolean(ioPolicy[typedKey]);
            return (
              <button
                key={key}
                type="button"
                onClick={() => updateIOPolicy({ [typedKey]: !enabled } as Partial<typeof ioPolicy>)}
                className={`rounded-lg border p-4 text-left transition ${enabled ? 'border-komgaPrimary/50 bg-komgaPrimary/10 text-white' : 'border-gray-800 bg-gray-900/50 text-gray-300 hover:border-gray-700'}`}
              >
                <span className="block text-sm font-semibold">{t(label)}</span>
                <span className="mt-1 block text-xs leading-5 text-gray-500">{t(hint)}</span>
              </button>
            );
          })}
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

import { FolderOpen, HardDrive, Plus, Server, Trash2 } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';
import { type LibraryStoragePolicy, type StorageIOPolicy, useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

const defaultStorageIOPolicy: StorageIOPolicy = {
  scan_concurrency: 0,
  archive_open_concurrency: 0,
  cover_concurrency: 0,
  hash_concurrency: 0,
  pause_background_when_reading: false,
  idle_only_heavy_tasks: false,
  disable_same_disk_page_cache: false,
};

function normalizeStorageIOPolicy(policy?: Partial<StorageIOPolicy>): StorageIOPolicy {
  return { ...defaultStorageIOPolicy, ...(policy || {}) };
}

export function SettingsLibraryPage() {
  const { t } = useI18n();
  const { config, setConfig, fieldErrors, capabilities, saving, saveConfig } = useSettings();

  if (!config) return null;
  const ioPolicy = normalizeStorageIOPolicy(config.library.io_policy);
  const storageProfiles = capabilities?.supported_storage_profiles || ['auto', 'ssd', 'hdd_external', 'network', 'custom'];
  const storagePolicies = config.library.storage_policies || [];
  const updateIOPolicy = (patch: Partial<typeof ioPolicy>) =>
    setConfig({
      ...config,
      library: {
        ...config.library,
        io_policy: { ...ioPolicy, ...patch },
      },
    });
  const updateStoragePolicies = (next: LibraryStoragePolicy[]) =>
    setConfig({
      ...config,
      library: {
        ...config.library,
        storage_policies: next,
      },
    });
  const updateStoragePolicyAt = (index: number, patch: Partial<LibraryStoragePolicy>) =>
    updateStoragePolicies(storagePolicies.map((policy, i) => (i === index ? { ...policy, ...patch } : policy)));
  const updateStoragePolicyIOAt = (index: number, patch: Partial<StorageIOPolicy>) =>
    updateStoragePolicies(
      storagePolicies.map((policy, i) =>
        i === index ? { ...policy, io_policy: { ...normalizeStorageIOPolicy(policy.io_policy), ...patch } } : policy,
      ),
    );
  const removeStoragePolicyAt = (index: number) => updateStoragePolicies(storagePolicies.filter((_, i) => i !== index));
  const addStoragePolicy = (path = '') =>
    updateStoragePolicies([
      ...storagePolicies,
      {
        path,
        storage_profile: 'hdd_external',
        io_policy: {
          scan_concurrency: 1,
          archive_open_concurrency: 1,
          cover_concurrency: 1,
          hash_concurrency: 1,
          pause_background_when_reading: true,
          idle_only_heavy_tasks: true,
          disable_same_disk_page_cache: true,
        },
      },
    ]);
  const pathsWithoutPolicy = (config.library.paths || []).filter((path) => !storagePolicies.some((policy) => policy.path === path));

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
              {storageProfiles.map((profile) => (
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

        <div className="space-y-3 rounded-lg border border-gray-800 bg-gray-900/40 p-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 className="text-sm font-semibold text-white">{t('settings.library.storageOverridesTitle')}</h4>
              <p className="mt-1 text-xs text-gray-500">{t('settings.library.storageOverridesHint')}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              {pathsWithoutPolicy.slice(0, 3).map((path) => (
                <button
                  key={path}
                  type="button"
                  onClick={() => addStoragePolicy(path)}
                  className="inline-flex items-center gap-2 rounded-lg border border-komgaPrimary/30 bg-komgaPrimary/10 px-3 py-2 text-xs text-komgaPrimary hover:bg-komgaPrimary/15"
                >
                  <Plus className="h-3.5 w-3.5" />
                  <span className="max-w-48 truncate">{path}</span>
                </button>
              ))}
              <button
                type="button"
                onClick={() => addStoragePolicy()}
                className="inline-flex items-center gap-2 rounded-lg border border-white/10 px-3 py-2 text-xs text-white/70 hover:bg-white/10"
              >
                <Plus className="h-3.5 w-3.5" />
                {t('settings.library.addStorageOverride')}
              </button>
            </div>
          </div>

          {storagePolicies.length ? (
            <div className="space-y-3">
              {storagePolicies.map((policy, index) => {
                const policyIO = normalizeStorageIOPolicy(policy.io_policy);
                return (
                  <div key={`${policy.path}-${index}`} className="rounded-lg border border-gray-800 bg-gray-950/50 p-3">
                    <div className="grid gap-3 md:grid-cols-[minmax(0,1.5fr)_220px_40px]">
                      <div>
                        <label className="mb-1 block text-xs text-gray-500">{t('settings.library.storageOverridePath')}</label>
                        <input
                          type="text"
                          value={policy.path}
                          onChange={(e) => updateStoragePolicyAt(index, { path: e.target.value })}
                          className={inputClassName}
                          placeholder={t('settings.library.storageOverridePathPlaceholder')}
                        />
                        <FieldErrors messages={fieldErrors(`library.storage_policies[${index}].path`)} />
                      </div>
                      <div>
                        <label className="mb-1 block text-xs text-gray-500">{t('settings.library.storageProfile')}</label>
                        <select
                          value={policy.storage_profile || 'auto'}
                          onChange={(e) => updateStoragePolicyAt(index, { storage_profile: e.target.value })}
                          className={inputClassName}
                        >
                          {storageProfiles.map((profile) => (
                            <option key={profile} value={profile}>
                              {t(`settings.library.storageProfile.${profile}`)}
                            </option>
                          ))}
                        </select>
                        <FieldErrors messages={fieldErrors(`library.storage_policies[${index}].storage_profile`)} />
                      </div>
                      <button
                        type="button"
                        onClick={() => removeStoragePolicyAt(index)}
                        className="mt-6 inline-flex h-10 w-10 items-center justify-center rounded-lg border border-red-500/20 text-red-300 hover:bg-red-500/10"
                        aria-label={t('settings.library.removeStorageOverride')}
                        title={t('settings.library.removeStorageOverride')}
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>

                    <div className="mt-3 grid gap-3 md:grid-cols-4">
                      {[
                        ['archive_open_concurrency', 'settings.library.archiveOpenConcurrency'],
                        ['cover_concurrency', 'settings.library.coverConcurrency'],
                        ['hash_concurrency', 'settings.library.hashConcurrency'],
                        ['scan_concurrency', 'settings.library.scanConcurrency'],
                      ].map(([key, label]) => {
                        const typedKey = key as keyof StorageIOPolicy;
                        return (
                          <div key={key}>
                            <label className="mb-1 block text-xs text-gray-500">{t(label, { count: policyIO[typedKey] })}</label>
                            <input
                              type="range"
                              min="0"
                              max="16"
                              value={Number(policyIO[typedKey]) || 0}
                              onChange={(e) => updateStoragePolicyIOAt(index, { [typedKey]: Number(e.target.value) || 0 } as Partial<StorageIOPolicy>)}
                              className="w-full accent-komgaPrimary"
                            />
                          </div>
                        );
                      })}
                    </div>

                    <div className="mt-3 grid gap-2 md:grid-cols-3">
                      {[
                        ['pause_background_when_reading', 'settings.library.pauseWhenReading'],
                        ['idle_only_heavy_tasks', 'settings.library.idleOnlyHeavyTasks'],
                        ['disable_same_disk_page_cache', 'settings.library.disableSameDiskPageCache'],
                      ].map(([key, label]) => {
                        const typedKey = key as keyof StorageIOPolicy;
                        const enabled = Boolean(policyIO[typedKey]);
                        return (
                          <button
                            key={key}
                            type="button"
                            onClick={() => updateStoragePolicyIOAt(index, { [typedKey]: !enabled } as Partial<StorageIOPolicy>)}
                            className={`rounded-lg border px-3 py-2 text-left text-xs transition ${enabled ? 'border-komgaPrimary/50 bg-komgaPrimary/10 text-white' : 'border-gray-800 bg-gray-900/50 text-gray-400 hover:border-gray-700'}`}
                          >
                            {t(label)}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="rounded-lg border border-dashed border-gray-800 px-3 py-4 text-sm text-gray-500">{t('settings.library.noStorageOverrides')}</p>
          )}
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

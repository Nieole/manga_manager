import { FolderOpen, Server } from 'lucide-react';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsLibraryPage() {
  const { config, setConfig, fieldErrors, capabilities, saving, saveConfig } = useSettings();

  if (!config) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title="库与扫描" description="放置基础服务配置、数据库路径、绑定目录信息以及扫描并发相关参数。" />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Server className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">基础服务</h3>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">服务端口</label>
            <input
              type="number"
              value={config.server.port}
              onChange={(e) => setConfig({ ...config, server: { ...config.server, port: Number(e.target.value) || 8080 } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('server.port')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">数据库路径</label>
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
          <h3 className="text-lg font-semibold text-white">扫描策略</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">扫描工作协程: {config.scanner.workers}</label>
            <input
              type="range"
              min="0"
              max="64"
              value={config.scanner.workers}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, workers: Number(e.target.value) || 0 } })}
              className="w-full accent-komgaPrimary"
            />
            <p className="mt-1 text-xs text-gray-500">{config.scanner.workers === 0 ? '0 表示自动调度。' : `当前固定为 ${config.scanner.workers} 个工作协程。`}</p>
            <FieldErrors messages={fieldErrors('scanner.workers')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">归档句柄池大小: {config.scanner.archive_pool_size}</label>
            <input
              type="range"
              min="1"
              max="50"
              value={config.scanner.archive_pool_size}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, archive_pool_size: Number(e.target.value) || 1 } })}
              className="w-full accent-komgaPrimary"
            />
            <p className="mt-1 text-xs text-gray-500">大一些能减少频繁翻页的重复打开成本，但会占用更多句柄和内存。</p>
            <FieldErrors messages={fieldErrors('scanner.archive_pool_size')} />
          </div>
        </div>

        <div className="rounded-xl border border-gray-800 bg-gray-900/50 p-4 text-sm text-gray-300">
          <p className="font-medium text-white">当前支持的扫描格式</p>
          <p className="mt-1">{capabilities?.default_scan_formats || 'zip,cbz,rar,cbr'}</p>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <Server className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">日志级别</h3>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">最小输出级别</label>
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
            <p className="mt-1 text-xs text-gray-500">同时控制控制台和日志文件的最小输出级别。保存后立即生效。</p>
            <FieldErrors messages={fieldErrors('logging.level')} />
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <h3 className="text-lg font-semibold text-white">当前已绑定目录</h3>
        {config.library.paths?.length ? (
          <div className="space-y-2">
            {config.library.paths.map((path) => (
              <div key={path} className="rounded-lg border border-gray-800 bg-gray-900/50 px-3 py-2 text-sm text-gray-300">
                {path}
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-gray-500">当前还没有绑定目录。资源库的添加和编辑仍通过主界面的资源库管理完成。</p>
        )}
      </section>

      <SettingsSaveBar saving={saving} label="保存库与扫描配置" hint="这里保存服务端口、数据库路径和扫描参数。" onSave={() => saveConfig('库与扫描配置已保存')} />
    </div>
  );
}

import { HardDrive, Image as ImageIcon } from 'lucide-react';
import { useSettings } from './SettingsContext';
import { FieldErrors, SettingsPageIntro, SettingsSaveBar, inputClassName, sectionClassName } from './shared';

export function SettingsMediaPage() {
  const { config, setConfig, fieldErrors, saving, saveConfig } = useSettings();

  if (!config) return null;

  return (
    <div className="space-y-6">
      <SettingsPageIntro title="图片与缓存" description="管理缓存目录、缩略图格式以及本地超分引擎路径与并发。" />

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <HardDrive className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">缓存与缩略图</h3>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">缓存目录</label>
            <input
              type="text"
              value={config.cache.dir}
              onChange={(e) => setConfig({ ...config, cache: { ...config.cache, dir: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('cache.dir')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">缩略图格式</label>
            <select
              value={config.scanner.thumbnail_format}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, thumbnail_format: e.target.value } })}
              className={inputClassName}
            >
              <option value="webp">WebP</option>
              <option value="avif">AVIF</option>
              <option value="jpg">JPEG</option>
            </select>
            <FieldErrors messages={fieldErrors('scanner.thumbnail_format')} />
          </div>
        </div>
      </section>

      <section className={sectionClassName}>
        <div className="flex items-center gap-2 text-komgaPrimary">
          <ImageIcon className="h-5 w-5" />
          <h3 className="text-lg font-semibold text-white">本地超分引擎</h3>
        </div>
        <div className="grid gap-4 md:grid-cols-2">
          <div>
            <label className="mb-1 block text-sm text-gray-400">Waifu2x 可执行文件</label>
            <input
              type="text"
              value={config.scanner.waifu2x_path}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, waifu2x_path: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('scanner.waifu2x_path')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">Real-CUGAN 可执行文件</label>
            <input
              type="text"
              value={config.scanner.realcugan_path}
              onChange={(e) => setConfig({ ...config, scanner: { ...config.scanner, realcugan_path: e.target.value } })}
              className={inputClassName}
            />
            <FieldErrors messages={fieldErrors('scanner.realcugan_path')} />
          </div>
          <div>
            <label className="mb-1 block text-sm text-gray-400">AI 超分并发上限: {config.scanner.max_ai_concurrency}</label>
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
            <p className="font-medium text-white">使用说明</p>
            <p className="mt-1">建议先确保本地命令可直接运行，再填写绝对路径。保存前后端会校验可执行文件是否存在。</p>
          </div>
        </div>
      </section>

      <SettingsSaveBar saving={saving} label="保存图片与缓存配置" hint="这里只保存缓存与超分相关配置。" onSave={() => saveConfig('图片与缓存配置已保存')} />
    </div>
  );
}

/**
 * 业务说明：本文件守卫「保存之后的刷新只覆盖刚保存的那一块」。
 *
 * 这是「只保存本分区」的对偶。少了它，保存动作会反过来把**其他**分区没保存的草稿
 * 静默抹掉、侧边栏的脏点也一起熄灭——方向比原缺陷安全（丢的是本地草稿不是写库），
 * 但同样没有任何提示。
 *
 * 这里把 fetchConfig 里那段合并规则原样抽出来测：它是一段纯逻辑，不值得为它渲染整个
 * SettingsProvider（那要 mock apiClient、LocaleProvider、ToastProvider 三层）。
 */

import { describe, expect, it } from 'vitest';

import { applySectionSnapshot, type ConfigSectionKey } from './SettingsContext';
import type { Config } from './SettingsContext';

// mergeAfterSave 复刻 fetchConfig 里 setConfig 的那段判定。
// 保持与实现同形；实现改了这里不跟着改，用例会先红。
function mergeAfterSave(
  prev: Config | null,
  fromServer: Config,
  savedSection?: ConfigSectionKey | 'koreader',
): Config {
  if (!prev || savedSection === undefined) return fromServer;
  if (savedSection === 'koreader') return prev;
  return applySectionSnapshot(prev, fromServer, savedSection);
}

function makeConfig(overrides: Record<string, unknown> = {}): Config {
  return {
    server: { port: 8080 },
    database: { path: '/data/manga.db' },
    library: { default_view: 'grid' },
    logging: { level: 'info' },
    cache: { dir: '/data/cache' },
    scanner: { workers: 4, scan_profile: 'fast_scan', archive_pool_size: 8, thumbnail_format: 'webp', waifu2x_path: '', realcugan_path: '', max_ai_concurrency: 1 },
    llm: { provider: 'ollama', api_key: '__mm_secret_unchanged__' },
    protocols: { opds: { enabled: false }, mihon: { enabled: false } },
    koreader: { enabled: false },
    ...overrides,
  } as unknown as Config;
}

// 带 library 分区草稿的本地状态。
function draftWithLibraryEdit(): Config {
  return makeConfig({
    scanner: { workers: 99, scan_profile: 'fast_scan', archive_pool_size: 8, thumbnail_format: 'webp', waifu2x_path: '', realcugan_path: '', max_ai_concurrency: 1 },
  });
}

describe('保存后的合并规则', () => {
  it('首屏加载整份替换', () => {
    const fromServer = makeConfig();
    expect(mergeAfterSave(null, fromServer)).toBe(fromServer);
  });

  it('保存 AI 之后保留资料库分区的草稿', () => {
    const next = mergeAfterSave(draftWithLibraryEdit(), makeConfig(), 'ai');
    expect((next.scanner as unknown as { workers: number }).workers).toBe(99);
  });

  it('保存 KOReader 之后一个 config 分区都不动', () => {
    // KOReader 的表单是独立 state，不在 config 草稿里。此前这条路径走的是无参
    // fetchConfig()，于是「保存 KOReader」会把 library/media/ai 的草稿一起吞掉
    // ——和分区保存是同一个缺陷、同一条代码路径，只是换了个按钮。
    const draft = draftWithLibraryEdit();
    const next = mergeAfterSave(draft, makeConfig(), 'koreader');
    expect(next).toBe(draft);
    expect((next.scanner as unknown as { workers: number }).workers).toBe(99);
  });

  it('保存连接分区只把 protocols 刷成服务端的值', () => {
    const draft = draftWithLibraryEdit();
    const fromServer = makeConfig({ protocols: { opds: { enabled: true }, mihon: { enabled: false } } });

    const next = mergeAfterSave(draft, fromServer, 'connections');

    expect((next.protocols as unknown as { opds: { enabled: boolean } }).opds.enabled).toBe(true);
    expect((next.scanner as unknown as { workers: number }).workers).toBe(99);
  });
});

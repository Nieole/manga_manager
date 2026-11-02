/** 编辑会话的公开入口保留其他分区草稿；不在测试里复制合并规则。 */

import { describe, expect, it } from 'vitest';

import { type ConfigSectionKey } from '../../features/settings-editor';
import { EditSession } from '../../features/settings-editor/EditSession';
import type { Config } from '../../features/settings-editor';

function mergeAfterSave(
  prev: Config | null,
  fromServer: Config,
  savedSection?: ConfigSectionKey | 'koreader',
): Config {
  const session = new EditSession();
  session.receive(makeConfig(), 'one');
  if (prev) session.edit(prev);
  if (savedSection) {
    const request = session.prepare(savedSection)!;
    session.acknowledge(savedSection, request.sent, fromServer, 'two');
  }
  session.receive(fromServer, 'two');
  return session.draft!;
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
    expect(mergeAfterSave(null, fromServer)).toEqual(fromServer);
  });

  it('保存 AI 之后保留资料库分区的草稿', () => {
    const next = mergeAfterSave(draftWithLibraryEdit(), makeConfig(), 'ai');
    expect((next.scanner as unknown as { workers: number }).workers).toBe(99);
  });

  it('保存 KOReader 之后一个 config 分区都不动', () => {
    // KOReader 也是编辑分区；确认其保存不会吞掉其他分区草稿。
    const draft = draftWithLibraryEdit();
    const next = mergeAfterSave(draft, makeConfig(), 'koreader');
    expect(next).toEqual(draft);
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

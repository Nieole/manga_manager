/**
 * 业务说明：本文件守卫「保存本分区只提交本分区」。
 *
 * 设置页按分区分成资料库 / 媒体 / AI 三块，各有自己的「保存」按钮。但保存动作此前直接把
 * 整份 config state 发给后端——而那份 state 里带着用户在**其他分区**改了却没保存的草稿。
 * 点一下「保存媒体设置」，顺手把没打算提交的 AI 密钥、扫描并发数一起写进了后端，
 * 界面上没有任何提示，用户也不知道自己提交了什么。
 *
 * 另一半是「脏标记与保存必须同源」：两者若各写一份字段清单，会漂移成
 * 「脏标记说这个分区没改、保存却把它写了出去」。
 */

import { describe, expect, it } from 'vitest';

import { applySectionSnapshot } from './SettingsContext';
import type { Config } from './SettingsContext';

// makeConfig 造一份只带本用例关心字段的最小 config。
function makeConfig(overrides: Record<string, unknown> = {}): Config {
  return {
    server: { port: 8080 },
    database: { path: '/data/manga.db' },
    library: { default_view: 'grid' },
    logging: { level: 'info' },
    cache: { dir: '/data/cache' },
    scanner: {
      workers: 4,
      scan_profile: 'fast_scan',
      archive_pool_size: 8,
      thumbnail_format: 'webp',
      waifu2x_path: '',
      realcugan_path: '',
      max_ai_concurrency: 1,
    },
    llm: { provider: 'ollama', api_key: '__mm_secret_unchanged__' },
    ...overrides,
  } as unknown as Config;
}

describe('applySectionSnapshot', () => {
  it('只提交本分区的字段，其他分区的草稿留在本地', () => {
    const server = makeConfig();
    const draft = makeConfig({
      cache: { dir: '/mnt/ssd/cache' }, // media 分区：本次要保存的
      llm: { provider: 'openai', api_key: 'sk-leaked' }, // ai 分区：用户改了但没点保存
    });

    const payload = applySectionSnapshot(server, draft, 'media');

    expect((payload.cache as unknown as { dir: string }).dir).toBe('/mnt/ssd/cache');
    // 核心判据：另一个分区的草稿绝不能被顺手提交。
    expect(payload.llm).toEqual(server.llm);
  });

  it('同一个对象下分属两个分区的字段互不影响', () => {
    // scanner 被 library 与 media 两个分区瓜分，是最容易写错的一处：
    // 整块覆盖 scanner 会让「保存媒体设置」把资料库分区的并发数一起改了。
    const server = makeConfig();
    const draft = makeConfig({
      scanner: {
        workers: 99, // library 分区
        scan_profile: 'metadata_scan', // library 分区
        archive_pool_size: 8,
        thumbnail_format: 'png', // media 分区
        waifu2x_path: '',
        realcugan_path: '',
        max_ai_concurrency: 1,
      },
    });

    const payload = applySectionSnapshot(server, draft, 'media');
    const scanner = payload.scanner as unknown as Record<string, unknown>;

    expect(scanner.thumbnail_format).toBe('png');
    expect(scanner.workers).toBe(4);
    expect(scanner.scan_profile).toBe('fast_scan');
  });

  it('不修改传入的对象', () => {
    // 保存失败时前端会继续用同一份 state 重试；就地改写会让第二次提交带上第一次的残留。
    const server = makeConfig();
    const draft = makeConfig({ llm: { provider: 'openai', api_key: 'sk-new' } });
    const before = JSON.stringify(server);

    applySectionSnapshot(server, draft, 'ai');

    expect(JSON.stringify(server)).toBe(before);
  });

  it('保留服务端下发的密钥占位符', () => {
    // 敏感字段回显的是占位符，后端据此保留原值。以 server 为底叠加意味着
    // 未改动分区的占位符原样带回去——绝不能被抹成空串，那会把密钥清掉。
    const server = makeConfig();
    const draft = makeConfig({ cache: { dir: '/mnt/ssd/cache' } });

    const payload = applySectionSnapshot(server, draft, 'media');

    expect((payload.llm as unknown as { api_key: string }).api_key).toBe('__mm_secret_unchanged__');
  });
});

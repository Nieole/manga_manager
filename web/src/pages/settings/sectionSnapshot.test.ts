/**
 * 本文件守卫两条不变量：
 * 1. 保存某分区必须只提交该分区字段，其他分区未保存的草稿不得被带出——否则会在用户毫无
 *    提示的情况下，把没打算提交的字段一并写进后端；
 * 2. 脏标记与保存所依据的字段清单必须同源，否则会漂移成「脏标记说没改、保存却写出去了」。
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

describe('保存后的刷新同样只覆盖本分区', () => {
  it('保存 AI 之后，资料库分区的草稿不被服务端配置抹掉', () => {
    // fetchConfig(savedSection) 的语义：以手上的草稿为底，只把刚保存的那个分区
    // 换成服务端的新值。少了这一半，「只保存本分区」就只做了一半——
    // 保存 AI 会顺手把资料库分区没保存的草稿整片抹掉，侧边栏脏点一起熄灭，全程无提示。
    const draft = makeConfig({
      scanner: {
        workers: 99, // library 分区的草稿，用户还没保存
        scan_profile: 'fast_scan',
        archive_pool_size: 8,
        thumbnail_format: 'webp',
        waifu2x_path: '',
        realcugan_path: '',
        max_ai_concurrency: 1,
      },
      llm: { provider: 'openai', api_key: 'sk-saved' },
    });
    const fromServer = makeConfig({ llm: { provider: 'openai', api_key: '__mm_secret_unchanged__' } });

    const next = applySectionSnapshot(draft, fromServer, 'ai');

    expect((next.scanner as unknown as { workers: number }).workers).toBe(99);
    expect((next.llm as unknown as { api_key: string }).api_key).toBe('__mm_secret_unchanged__');
  });

  it('连接分区只带出 protocols', () => {
    // 连接分区提交必须只带出 protocols，不能以活的草稿整份为基底——
    // 否则「在资料库分区改了没保存、切到连接页拨一下 OPDS」会把那份改动一并写库。
    const server = makeConfig({ protocols: { opds: { enabled: false }, mihon: { enabled: false } } });
    const draft = makeConfig({
      protocols: { opds: { enabled: true }, mihon: { enabled: false } },
      llm: { provider: 'openai', api_key: 'sk-draft' },
    });

    const payload = applySectionSnapshot(server, draft, 'connections');

    expect((payload.protocols as unknown as { opds: { enabled: boolean } }).opds.enabled).toBe(true);
    expect(payload.llm).toEqual(server.llm);
  });
});

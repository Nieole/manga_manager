/**
 * 业务说明：本文件守卫「日志/运维页的文案全部走 i18n」。
 *
 * 该页的性能看板此前 7 处中文硬编码在 JSX 里，同文件其余文案却都走 t()——
 * 属于漏改而非有意。后果是切到英文界面时那一块仍显示中文。
 *
 * 这条用例读源码而不是渲染组件：渲染只能证明「当前这次渲染」的文案对，
 * 而新加一行硬编码同样不会被渲染用例发现。扫源码才能把「不许再硬编码」这条约束钉住。
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

import { messages as enUS } from './locales/en-US';
import { messages as zhCN } from './locales/zh-CN';

const LOGS_SOURCE = fileURLToPath(new URL('../pages/Logs.tsx', import.meta.url));

// stripComments 先剥块注释再剥行注释：本仓注释一律中文，不剥掉的话全是误报。
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\/\/[^\n]*/g, '');
}

const CJK = /[一-鿿]/;

describe('Logs 页文案', () => {
  it('源码里没有硬编码的中文', () => {
    const lines = stripComments(readFileSync(LOGS_SOURCE, 'utf8')).split('\n');
    const offending = lines
      .map((line, index) => ({ line: line.trim(), no: index + 1 }))
      .filter((entry) => CJK.test(entry.line))
      .map((entry) => `${entry.no}: ${entry.line}`);
    expect(offending).toEqual([]);
  });

  it('性能看板的 7 个词条在两份语言表里都有', () => {
    const keys = [
      'logs.perf.title',
      'logs.perf.avgResponse',
      'logs.perf.p95',
      'logs.perf.cacheHitRate',
      'logs.perf.ioWait',
      'logs.perf.archiveOpens',
      'logs.perf.totalBytes',
    ];
    for (const key of keys) {
      expect(zhCN[key], `zh-CN 缺 ${key}`).toBeTruthy();
      expect(enUS[key], `en-US 缺 ${key}`).toBeTruthy();
    }
  });

  it('两份语言表的 key 集合完全一致', () => {
    const zhKeys = Object.keys(zhCN).sort();
    const enKeys = Object.keys(enUS).sort();
    expect(zhKeys).toEqual(enKeys);
  });
});

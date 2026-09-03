/**
 * 守卫英文词条的复数形态：凡是带 {{count}} 的 en-US 词条都必须写成 `one=…|other=…`，
 * 例外只能来自下面两张有名有姓的清单——新增词条漏写模板会在这里变红，而不是等到界面上
 * 出现「1 books」才被用户发现。中文无复数变形，反向守卫它不许出现复数模板。
 */

import { beforeAll, describe, expect, it } from 'vitest';
import { loadLocaleMessages, translateInLocale } from './LocaleProvider';
import { messages as enUS } from './locales/en-US';
import { messages as zhCN } from './locales/zh-CN';

// 计数后面跟的是英语里单复数同形的名词（series），换不换形态都一样。
const INVARIANT_NOUN_KEYS = [
  'common.seriesCount',
  'collections.snapshotPreview.truncated',
  'readingLists.itemCount',
  'home.pagination.totalSeries',
  'bulkEdit.description',
  'bulkEdit.success',
  'home.selection.addToCollectionSuccess',
  'home.transfer.summary',
  'settingsTags.deleteDescription',
  'addToCollection.description',
];

// 计数后面根本没有随数变形的词：纯数值（滑块与设置项的当前值）、形容词或过去分词收尾。
const NO_INFLECTION_KEYS = [
  'dedup.removeSelected',
  'collections.smartChip.more',
  'metadataReviews.diffChanged',
  'metadataReviews.toast.applied',
  'metadataReviews.toast.rejected',
  'home.toolbar.selectedCount',
  'home.smartFilters.defaultName',
  'home.smartFilters.chipPageSize',
  'home.filters.moreHidden',
  'home.filters.expandMore',
  'home.selection.selectedCount',
  'home.selection.currentPageCount',
  'series.selection.selectedCount',
  'series.sidePanel.badge.metadata',
  'series.metadataReview.pendingCount',
  'dashboard.reviewPending.label',
  'offlineShelf.health.queue',
  'offlineShelf.health.cache',
  'franchise.graph.truncated',
  'settings.library.scanWorkers',
  'settings.library.archivePoolSize',
  'settings.library.archiveOpenConcurrency',
  'settings.library.coverConcurrency',
  'settings.library.hashConcurrency',
  'settings.library.scanConcurrency',
  'settings.ai.timeout',
  'settings.ai.timeoutCurrent',
  'settings.media.aiConcurrency',
  'settings.connections.requests.slow',
  'logs.perf.archiveOpens',
];

const EXEMPT_KEYS = new Set([...INVARIANT_NOUN_KEYS, ...NO_INFLECTION_KEYS]);

const PLURAL_FORM_PREFIX = /^(zero|one|two|few|many|other)=/;

// 与 selectPluralForm 同一套判定：所有段都带分类前缀才算模板，少一段前缀就整条按普通文案处理。
function parsePluralForms(template: string) {
  if (!template.includes('|')) return null;
  const forms = new Map<string, string>();
  for (const segment of template.split('|')) {
    const prefix = PLURAL_FORM_PREFIX.exec(segment);
    if (!prefix) return null;
    forms.set(prefix[1], segment.slice(prefix[0].length));
  }
  return forms;
}

function countKeys(catalog: Record<string, string>) {
  return Object.keys(catalog).filter((key) => catalog[key].includes('{{count}}'));
}

describe('en-US 带 {{count}} 的词条', () => {
  const keys = countKeys(enUS);

  it('数量与豁免清单对得上，防止清单悄悄失效', () => {
    expect(keys.length).toBeGreaterThan(0);
    expect(keys.filter((key) => EXEMPT_KEYS.has(key)).sort()).toEqual([...EXEMPT_KEYS].sort());
  });

  it.each(keys.filter((key) => !EXEMPT_KEYS.has(key)))('%s 是合法的复数模板', (key) => {
    const forms = parsePluralForms(enUS[key]);
    expect(forms, `${key} 没写成 one=…|other=… 模板：${enUS[key]}`).not.toBeNull();
    expect(forms?.has('one'), `${key} 缺 one 段`).toBe(true);
    expect(forms?.has('other'), `${key} 缺 other 段`).toBe(true);
  });

  it.each(keys.filter((key) => !EXEMPT_KEYS.has(key)))('%s 的单复数两段确实不同', (key) => {
    const forms = parsePluralForms(enUS[key]);
    expect(forms?.get('one')).not.toBe(forms?.get('other'));
  });

  it.each([...EXEMPT_KEYS])('豁免的 %s 仍存在且确实不是复数模板', (key) => {
    expect(enUS[key], `${key} 已不在词条表里，请从豁免清单删掉`).toBeTruthy();
    expect(parsePluralForms(enUS[key]), `${key} 已经写成模板了，请从豁免清单删掉`).toBeNull();
  });
});

describe('zh-CN 不写复数模板', () => {
  it('中文没有单复数变形，任何一条写成模板都是误抄', () => {
    const templated = Object.keys(zhCN).filter((key) => parsePluralForms(zhCN[key]) !== null);
    expect(templated).toEqual([]);
  });
});

describe('n=1 时的真实渲染', () => {
  beforeAll(async () => {
    // 断言的是真实词条的渲染结果，词条表要先进 translateInLocale 的缓存。
    await loadLocaleMessages('en-US');
  });

  // 同一张卡上下两行、同一个数据点的两处文案，曾经一个用模板一个没用，于是「1 day」紧挨着「1 days」。
  it('统计页连续天数的两行一致', () => {
    expect(translateInLocale('en-US', 'stats.streak.days', { count: 1 })).toBe('1 day');
    expect(translateInLocale('en-US', 'stats.streak.longest', { count: 1 })).toBe('Longest streak 1 day');
  });

  it('仪表盘活跃天数的两处一致', () => {
    expect(translateInLocale('en-US', 'dashboard.activity.summary', { count: 1 })).toBe('Active 1 day in the last 7');
    expect(translateInLocale('en-US', 'dashboard.stats.activeDays7', { count: 1 })).toBe('Active 1 day in the last 7');
  });

  it('藏书计数不再说 1 books', () => {
    expect(translateInLocale('en-US', 'common.books', { count: 1 })).toBe('1 book');
    expect(translateInLocale('en-US', 'common.itemCount', { count: 1 })).toBe('1 item');
    expect(translateInLocale('en-US', 'offlineShelf.listCount', { count: 1 })).toBe('1 book');
    expect(translateInLocale('en-US', 'dedup.removed', { count: 1 })).toBe('Removed 1 book');
    expect(translateInLocale('en-US', 'home.selection.markReadSuccess', { count: 1 })).toBe('Marked 1 book read');
  });

  it('n>1 时照旧是复数', () => {
    expect(translateInLocale('en-US', 'common.books', { count: 2 })).toBe('2 books');
    expect(translateInLocale('en-US', 'stats.streak.longest', { count: 0 })).toBe('Longest streak 0 days');
    expect(translateInLocale('en-US', 'dashboard.stats.activeDays7', { count: 7 })).toBe('Active 7 days in the last 7');
  });
});

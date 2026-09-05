/**
 * 守卫英文词条在 n=1 时的渲染：判据是渲染结果里有没有「1 <可数名词复数>」，与变量叫不叫 count
 * 无关，新词条无论用什么变量名都默认落进来。带 {{count}} 的另有一层更强的整条模板要求。
 * 两种例外都只能来自本文件里有名有姓的清单。中文反向守卫不许出现任何变形语法。
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
  it('行内的 `{{名字#one=…}}` 变形同样不该出现在中文里', () => {
    const inflected = Object.keys(zhCN).filter((key) => /\{\{[^}]*#/.test(zhCN[key]));
    expect(inflected).toEqual([]);
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

  it('资料库卡片上只有一本书的系列不说 1 books', () => {
    expect(translateInLocale('en-US', 'home.seriesCountsBooksOnly', { books: 1 })).toBe('1 book');
    expect(translateInLocale('en-US', 'home.seriesCountsBooksOnly', { books: 4 })).toBe('4 books');
    expect(translateInLocale('en-US', 'home.seriesCountsWithVolumes', { volumes: 1, books: 1 })).toBe('1 vol · 1 book');
  });

  it('一条文案里的两个计数各自变形', () => {
    expect(translateInLocale('en-US', 'dedup.summary', { groups: 1, books: 1 })).toBe('1 group, 1 book total');
    expect(translateInLocale('en-US', 'dedup.summary', { groups: 2, books: 5 })).toBe('2 groups, 5 books total');
    expect(translateInLocale('en-US', 'series.header.volumeSummary', { books: 1, pages: 30 })).toBe('1 book · 30 pages');
  });

  it('比值按分母定形态，分母是格式化后的字符串也算得出来', () => {
    expect(translateInLocale('en-US', 'dashboard.readingProgress.summary', { read: 0, total: 1 })).toBe('Read 0 / 1 book');
    expect(translateInLocale('en-US', 'dashboard.readingProgress.summary', { read: 1, total: 5 })).toBe('Read 1 / 5 books');
    expect(translateInLocale('en-US', 'dashboard.readingProgress.pages', { pages: '1' })).toBe('1 page in total');
    expect(translateInLocale('en-US', 'dashboard.readingProgress.pages', { pages: '1,024' })).toBe('1,024 pages in total');
    expect(translateInLocale('en-US', 'series.continue.resume', { volume: 'Vol.1', page: 1, total: '?' }))
      .toBe('Resume: Vol.1 (1 / ? pages)');
  });

  it('n>1 时照旧是复数', () => {
    expect(translateInLocale('en-US', 'common.books', { count: 2 })).toBe('2 books');
    expect(translateInLocale('en-US', 'stats.streak.longest', { count: 0 })).toBe('Longest streak 0 days');
    expect(translateInLocale('en-US', 'dashboard.stats.activeDays7', { count: 7 })).toBe('Active 7 days in the last 7');
  });
});

// 以 s 结尾却不是「被计数的名词」的词：series 单复数同形，其余是不可数名词、限定词或第三人称动词。
// 这是通用的英语事实，不针对某条词条，所以按词收在这里而不是按 key 登记。
const NOT_A_COUNTED_NOUN = new Set(['series', 'progress', 'status', 'this', 'was', 'is', 'has', 'needs']);

// 渲染出「1 …s」却不该改的词条：填 1 的那个变量根本不是计数。每条都要写清为什么。
const RENDERED_PLURAL_EXEMPTIONS: Record<string, string> = {
  'task.hint.rebuild_book_hashes': '{{label}} 是 KOReader 索引方式的名字，不是计数',
};

function interpolationNames(template: string) {
  return [...template.matchAll(/\{\{\s*([^}]+?)\s*\}\}/g)].map((match) => match[1].split('#')[0].trim());
}

// 交给真的渲染器，守卫才不会和 LocaleProvider 的实现走岔；count 一并填上以选中模板的 one 段。
function renderWithOnes(key: string) {
  const params: Record<string, number> = { count: 1 };
  for (const name of interpolationNames(enUS[key])) {
    params[name] = 1;
  }
  return translateInLocale('en-US', key, params);
}

// 只认字面的 1 后面最多三个词，遇到标点或另一个数字就停：「1 progress records」这种插了定语的也算。
function pluralAfterCount(text: string) {
  for (const match of text.matchAll(/\b1\s+([A-Za-z][A-Za-z\s]*)/g)) {
    for (const word of match[1].trim().split(/\s+/).slice(0, 3)) {
      const lower = word.toLowerCase();
      if (NOT_A_COUNTED_NOUN.has(lower)) {
        continue;
      }
      if (/[^s]s$/.test(lower)) {
        return word;
      }
    }
  }
  return null;
}

describe('en-US 词条在 n=1 时的渲染', () => {
  beforeAll(async () => {
    await loadLocaleMessages('en-US');
  });

  it('把所有插值变量填 1 之后不会渲染出「1 <复数名词>」', () => {
    const hits = Object.keys(enUS)
      .filter((key) => pluralAfterCount(renderWithOnes(key)) !== null)
      .map((key) => `${key} => ${renderWithOnes(key)}`);
    const registered = Object.keys(RENDERED_PLURAL_EXEMPTIONS).map((key) => `${key} => ${renderWithOnes(key)}`);
    expect(hits.sort()).toEqual(registered.sort());
  });

  it.each(Object.keys(RENDERED_PLURAL_EXEMPTIONS))('登记过的 %s 仍存在且仍会命中', (key) => {
    expect(enUS[key], `${key} 已不在词条表里，请从登记清单删掉`).toBeTruthy();
    expect(pluralAfterCount(renderWithOnes(key)), `${key} 不再命中，请从登记清单删掉`).not.toBeNull();
  });
});

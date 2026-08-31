import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import {
  getClientLocale,
  loadLocaleMessages,
  normalizeAppLocale,
  translateInLocale,
} from './LocaleProvider';
import { messages as enUSMessages } from './locales/en-US';
import { messages as zhCNMessages } from './locales/zh-CN';

describe('normalizeAppLocale', () => {
  it('passes through exact supported locales', () => {
    expect(normalizeAppLocale('zh-CN')).toBe('zh-CN');
    expect(normalizeAppLocale('en-US')).toBe('en-US');
  });

  it('maps any zh-* variant to zh-CN', () => {
    expect(normalizeAppLocale('zh')).toBe('zh-CN');
    expect(normalizeAppLocale('zh-TW')).toBe('zh-CN');
    expect(normalizeAppLocale('zh-Hant')).toBe('zh-CN');
  });

  it('maps any en-* variant to en-US', () => {
    expect(normalizeAppLocale('en')).toBe('en-US');
    expect(normalizeAppLocale('en-GB')).toBe('en-US');
  });

  it('is case-insensitive for the prefix match (non-exact input)', () => {
    // "EN-us" is not an exact SUPPORTED_LOCALES entry, so it goes through the lowercased prefix path.
    expect(normalizeAppLocale('EN-us')).toBe('en-US');
    expect(normalizeAppLocale('ZH')).toBe('zh-CN');
  });

  it('trims whitespace before matching', () => {
    expect(normalizeAppLocale('  zh-CN  ')).toBe('zh-CN');
    expect(normalizeAppLocale('  en  ')).toBe('en-US');
  });

  it('falls back to the default locale for unknown / empty / nullish input', () => {
    expect(normalizeAppLocale('fr-FR')).toBe('zh-CN');
    expect(normalizeAppLocale('')).toBe('zh-CN');
    expect(normalizeAppLocale('   ')).toBe('zh-CN');
    expect(normalizeAppLocale(null)).toBe('zh-CN');
    expect(normalizeAppLocale(undefined)).toBe('zh-CN');
  });
});

describe('getClientLocale', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns the default locale when there is no window (SSR / node)', () => {
    // No window stub in this test -> typeof window === "undefined" branch.
    expect(getClientLocale()).toBe('zh-CN');
  });

  it('prefers a stored locale over the navigator language', () => {
    vi.stubGlobal('window', {
      localStorage: { getItem: () => 'en-US' },
      navigator: { language: 'zh-CN' },
    });
    expect(getClientLocale()).toBe('en-US');
  });

  it('normalizes the stored locale value', () => {
    vi.stubGlobal('window', {
      localStorage: { getItem: () => 'en-GB' },
      navigator: { language: 'zh-CN' },
    });
    expect(getClientLocale()).toBe('en-US');
  });

  it('falls back to navigator.language when nothing is stored', () => {
    vi.stubGlobal('window', {
      localStorage: { getItem: () => null },
      navigator: { language: 'en-US' },
    });
    expect(getClientLocale()).toBe('en-US');
  });

  it('treats an empty stored string as "not stored" and uses navigator', () => {
    vi.stubGlobal('window', {
      localStorage: { getItem: () => '' },
      navigator: { language: 'en-AU' },
    });
    expect(getClientLocale()).toBe('en-US');
  });

  it('falls back to the default locale for an unknown navigator language', () => {
    vi.stubGlobal('window', {
      localStorage: { getItem: () => null },
      navigator: { language: 'fr-FR' },
    });
    expect(getClientLocale()).toBe('zh-CN');
  });
});

describe('translateInLocale / fillTemplate substitution', () => {
  // A key that is absent from every catalog is returned verbatim by getTemplate, so we can
  // exercise fillTemplate by passing the template string itself as the (missing) key.
  it('substitutes a single {{placeholder}}', () => {
    expect(translateInLocale('en-US', 'Hello {{name}}', { name: 'World' })).toBe('Hello World');
  });

  it('substitutes multiple placeholders', () => {
    expect(translateInLocale('en-US', '{{a}} and {{b}}', { a: '1', b: '2' })).toBe('1 and 2');
  });

  it('renders a missing variable as an empty string', () => {
    expect(translateInLocale('en-US', '{{a}}-{{b}}', { a: 'x' })).toBe('x-');
  });

  it('renders null / undefined variables as empty strings', () => {
    expect(translateInLocale('en-US', '[{{x}}]', { x: null })).toBe('[]');
    expect(translateInLocale('en-US', '[{{x}}]', { x: undefined })).toBe('[]');
  });

  it('stringifies number and boolean values', () => {
    expect(translateInLocale('en-US', 'n={{n}} b={{b}}', { n: 5, b: true })).toBe('n=5 b=true');
    expect(translateInLocale('en-US', 'zero={{z}}', { z: 0 })).toBe('zero=0');
  });

  it('trims whitespace inside the braces before looking up the variable', () => {
    expect(translateInLocale('en-US', 'Hi {{  name  }}', { name: 'Z' })).toBe('Hi Z');
  });

  it('leaves a template without placeholders untouched', () => {
    expect(translateInLocale('en-US', 'plain text', { unused: 'y' })).toBe('plain text');
  });

  it('returns the template unchanged when no params are supplied', () => {
    // fillTemplate short-circuits (returns template) when params is undefined.
    expect(translateInLocale('en-US', 'Hello {{name}}')).toBe('Hello {{name}}');
  });
});

describe('translateInLocale catalog fallback order', () => {
  it('resolves current-locale, then default-locale, then the raw key', async () => {
    // Nothing cached yet: an unknown key comes back verbatim.
    expect(translateInLocale('en-US', 'totally.missing.key')).toBe('totally.missing.key');

    // Load only the default locale (zh-CN). en-US is not cached, so lookups for en-US fall
    // through to the default catalog.
    await loadLocaleMessages('zh-CN');
    expect(translateInLocale('en-US', 'common.none')).toBe('暂无');

    // Now load en-US: the current locale's catalog wins over the default one.
    await loadLocaleMessages('en-US');
    expect(translateInLocale('en-US', 'common.none')).toBe('None');
    expect(translateInLocale('zh-CN', 'common.none')).toBe('暂无');

    // A key present in neither catalog still returns the raw key.
    expect(translateInLocale('en-US', 'still.not.a.real.key')).toBe('still.not.a.real.key');
  });
});

describe('复数模板：英文按 Intl.PluralRules 选形态', () => {
  beforeAll(async () => {
    // 这一组断言的是真实词条的渲染结果，两份词条表都要进缓存。
    await loadLocaleMessages('zh-CN');
    await loadLocaleMessages('en-US');
  });

  it('count=1 时英文用单数', () => {
    expect(translateInLocale('en-US', 'settings.validationIssues', { count: 1 })).toBe('1 issue needs attention');
    expect(translateInLocale('en-US', 'series.content.bookCount', { count: 1 })).toBe('1 book');
    expect(translateInLocale('en-US', 'series.content.pageCount', { count: 1 })).toBe('1 page');
    expect(translateInLocale('en-US', 'dashboard.stats.booksReadValue', { count: 1 })).toBe('1 book');
    expect(translateInLocale('en-US', 'stats.streak.days', { count: 1 })).toBe('1 day');
    expect(translateInLocale('en-US', 'dashboard.activity.summary', { count: 1 })).toBe('Active 1 day in the last 7');
    expect(translateInLocale('en-US', 'metadataReviews.fieldCount', { count: 1 })).toBe('1 field');
    expect(translateInLocale('en-US', 'home.toolbar.resultCount', { count: 1 })).toBe('1 result in this library');
    expect(translateInLocale('en-US', 'taskBubble.running', { count: 1 })).toBe('1 task running');
  });

  it('count=0 与 count>1 时英文用复数', () => {
    expect(translateInLocale('en-US', 'series.content.bookCount', { count: 0 })).toBe('0 books');
    expect(translateInLocale('en-US', 'series.content.bookCount', { count: 2 })).toBe('2 books');
    expect(translateInLocale('en-US', 'settings.validationIssues', { count: 0 })).toBe('0 issues need attention');
    expect(translateInLocale('en-US', 'settings.validationIssues', { count: 3 })).toBe('3 issues need attention');
    expect(translateInLocale('en-US', 'stats.streak.days', { count: 7 })).toBe('7 days');
    expect(translateInLocale('en-US', 'taskBubble.running', { count: 2 })).toBe('2 tasks running');
  });

  it('中文不受影响：同一条词条在任何计数下都是单一形态', () => {
    expect(translateInLocale('zh-CN', 'series.content.bookCount', { count: 0 })).toBe('0 话');
    expect(translateInLocale('zh-CN', 'series.content.bookCount', { count: 1 })).toBe('1 话');
    expect(translateInLocale('zh-CN', 'series.content.bookCount', { count: 2 })).toBe('2 话');
    expect(translateInLocale('zh-CN', 'settings.validationIssues', { count: 1 })).toBe('存在 1 个待修正项');
    expect(translateInLocale('zh-CN', 'taskBubble.running', { count: 1 })).toBe('1 个任务进行中');
  });
});

describe('复数模板的分段解析', () => {
  // 这一组用未落词条的 key：getTemplate 原样返回 key 本身，于是模板字符串可以直接写在断言里。
  it('段序无关，按分类名取形态而不是按位置', () => {
    expect(translateInLocale('en-US', 'other={{count}} books|one={{count}} book', { count: 1 })).toBe('1 book');
    expect(translateInLocale('en-US', 'other={{count}} books|one={{count}} book', { count: 5 })).toBe('5 books');
  });

  it('当前语言用不到的分类照样能写，取不到就回落到 other', () => {
    // few 是英语没有的分类，count=3 在英语下算出 other。
    expect(translateInLocale('en-US', 'one=a|few=b|other=c', { count: 3 })).toBe('c');
    // 缺 one 段时 count=1 也回落到 other，而不是报错或吐出整条模板。
    expect(translateInLocale('en-US', 'few=b|other=c', { count: 1 })).toBe('c');
  });

  it('含字面 | 但缺分类前缀的文案不被拆开', () => {
    expect(translateInLocale('en-US', 'a|b', { count: 1 })).toBe('a|b');
    expect(translateInLocale('en-US', 'one=a|b', { count: 1 })).toBe('one=a|b');
  });

  it('count 不是有效数字时取 other', () => {
    // 千分位格式化后的字符串（Dashboard 会这么传）与缺参数都落在这一支。
    expect(translateInLocale('en-US', 'one={{count}} day|other={{count}} days', { count: '1,000' })).toBe('1,000 days');
    expect(translateInLocale('en-US', 'one=day|other=days', {})).toBe('days');
    expect(translateInLocale('en-US', 'one=day|other=days')).toBe('days');
  });

  it('格式化成字符串的 count 仍按数值分类', () => {
    // Dashboard 传的是 formatNumber 的结果，一位数时是纯数字串。
    expect(translateInLocale('en-US', 'one={{count}} day|other={{count}} days', { count: '1' })).toBe('1 day');
  });
});

describe('两份词条表的 key 集合', () => {
  // 复数形态写在词条值里而不是 key 后缀，正是为了让这条约束不受影响。
  it('zh-CN 与 en-US 完全一致', () => {
    expect(Object.keys(enUSMessages).sort()).toEqual(Object.keys(zhCNMessages).sort());
  });
});

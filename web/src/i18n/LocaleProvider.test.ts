import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  getClientLocale,
  loadLocaleMessages,
  normalizeAppLocale,
  translateInLocale,
} from './LocaleProvider';

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

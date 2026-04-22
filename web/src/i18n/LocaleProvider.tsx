import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import axios from 'axios';
import { formatDistanceToNow } from 'date-fns';
import { enUS, zhCN } from 'date-fns/locale';
import { DEFAULT_LOCALE, SUPPORTED_LOCALES, messages, type AppLocale } from './messages';

const LOCALE_STORAGE_KEY = 'manga_manager_locale';

type TranslationParams = Record<string, string | number | boolean | null | undefined>;

interface I18nContextValue {
  locale: AppLocale;
  locales: readonly AppLocale[];
  setLocale: (locale: AppLocale | string) => void;
  t: (key: string, params?: TranslationParams) => string;
  formatDateTime: (value: string | number | Date | null | undefined, options?: Intl.DateTimeFormatOptions) => string;
  formatNumber: (value: number | null | undefined, options?: Intl.NumberFormatOptions) => string;
  formatRelativeTime: (value: string | number | Date | null | undefined) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

function isSupportedLocale(locale: string): locale is AppLocale {
  return (SUPPORTED_LOCALES as readonly string[]).includes(locale);
}

export function normalizeAppLocale(locale?: string | null): AppLocale {
  const trimmed = String(locale ?? '').trim();
  if (isSupportedLocale(trimmed)) {
    return trimmed;
  }
  const lower = trimmed.toLowerCase();
  if (lower.startsWith('zh')) {
    return 'zh-CN';
  }
  if (lower.startsWith('en')) {
    return 'en-US';
  }
  return DEFAULT_LOCALE as AppLocale;
}

export function getClientLocale(): AppLocale {
  if (typeof window === 'undefined') {
    return DEFAULT_LOCALE as AppLocale;
  }
  const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY);
  if (stored) {
    return normalizeAppLocale(stored);
  }
  return normalizeAppLocale(window.navigator.language);
}

function fillTemplate(template: string, params?: TranslationParams) {
  if (!params) {
    return template;
  }
  return template.replace(/\{\{\s*([^}]+?)\s*\}\}/g, (_, key: string) => {
    const value = params[key.trim()];
    return value == null ? '' : String(value);
  });
}

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<AppLocale>(() => getClientLocale());

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);
    }
    if (typeof document !== 'undefined') {
      document.documentElement.lang = locale;
    }
    axios.defaults.headers.common['X-App-Locale'] = locale;
    axios.defaults.headers.common['Accept-Language'] = locale;
  }, [locale]);

  const setLocale = (nextLocale: AppLocale | string) => {
    setLocaleState(normalizeAppLocale(nextLocale));
  };

  const value = useMemo<I18nContextValue>(() => {
    const currentMessages = messages[locale];
    const fallbackMessages = messages[DEFAULT_LOCALE as AppLocale];

    const t = (key: string, params?: TranslationParams) => {
      const template = currentMessages[key] ?? fallbackMessages[key] ?? key;
      return fillTemplate(template, params);
    };

    const formatDateTime = (
      value: string | number | Date | null | undefined,
      options: Intl.DateTimeFormatOptions = {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      },
    ) => {
      if (value == null || value === '') {
        return t('common.none');
      }
      const date = value instanceof Date ? value : new Date(value);
      if (Number.isNaN(date.getTime())) {
        return String(value);
      }
      return new Intl.DateTimeFormat(locale, options).format(date);
    };

    const formatNumber = (value: number | null | undefined, options?: Intl.NumberFormatOptions) => {
      if (value == null || Number.isNaN(value)) {
        return t('common.none');
      }
      return new Intl.NumberFormat(locale, options).format(value);
    };

    const formatRelativeTime = (value: string | number | Date | null | undefined) => {
      if (value == null || value === '') {
        return t('common.none');
      }
      const date = value instanceof Date ? value : new Date(value);
      if (Number.isNaN(date.getTime())) {
        return String(value);
      }
      return formatDistanceToNow(date, {
        addSuffix: true,
        locale: locale === 'en-US' ? enUS : zhCN,
      });
    };

    return {
      locale,
      locales: SUPPORTED_LOCALES,
      setLocale,
      t,
      formatDateTime,
      formatNumber,
      formatRelativeTime,
    };
  }, [locale]);

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n() {
  const context = useContext(I18nContext);
  if (!context) {
    throw new Error('useI18n must be used within LocaleProvider');
  }
  return context;
}

export function translateInLocale(locale: AppLocale, key: string, params?: TranslationParams) {
  const currentMessages = messages[locale];
  const fallbackMessages = messages[DEFAULT_LOCALE as AppLocale];
  const template = currentMessages[key] ?? fallbackMessages[key] ?? key;
  return fillTemplate(template, params);
}

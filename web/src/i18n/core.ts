export const DEFAULT_LOCALE = 'zh-CN';

export const SUPPORTED_LOCALES = ['zh-CN', 'en-US'] as const;

export type AppLocale = (typeof SUPPORTED_LOCALES)[number];

export type MessageCatalog = Record<string, string>;

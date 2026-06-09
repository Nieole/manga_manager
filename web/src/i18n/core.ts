/**
 * 业务说明：本文件是业务实现，属于前端国际化资源层，负责维护中文、英文等界面文案和业务状态描述。
 * 它把后端状态、前端操作和领域术语转换为用户可理解的本地化文本。
 * 维护时应保证 key 稳定、占位符一致、业务术语统一，并避免修改造成页面缺文案。
 */

export const DEFAULT_LOCALE = 'zh-CN';

export const SUPPORTED_LOCALES = ['zh-CN', 'en-US'] as const;

export type AppLocale = (typeof SUPPORTED_LOCALES)[number];

export type MessageCatalog = Record<string, string>;

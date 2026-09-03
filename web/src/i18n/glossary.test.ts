/**
 * 守中文词条只用 CONTEXT.md 的规范词：禁用词一旦出现在面向用户那一层就变红。
 * 只收判定确定的那几个——「建议」「候选」这类要看上下文（AI 分组给出的合集成员建议
 * 本来就叫候选），交给评审，不在这里一刀切。
 */

import { describe, expect, it } from 'vitest';
import { messages as zhCN } from './locales/zh-CN';

// 每条都是 CONTEXT.md 里写明的 _Avoid_ 词，右侧是它该让位给谁。
const BANNED_TERMS: Array<[string, string]> = [
  ['书库', '资料库'],
  ['资源库', '资料库'],
  ['智能合集', '智能书架'],
  ['动态合集', '智能书架'],
  ['进行中', '活动态（三种状态的合称，不是「运行中」）'],
];

describe('zh-CN 词条的用词', () => {
  it.each(BANNED_TERMS)('不出现禁用词「%s」，该写「%s」', (banned, replacement) => {
    const hits = Object.keys(zhCN).filter((key) => zhCN[key].includes(banned));
    expect(hits, `这些词条把「${replacement}」写成了「${banned}」：${hits.join(', ')}`).toEqual([]);
  });
});

describe('提案与候选没有用反', () => {
  // 提案是「某个系列的元数据该改成什么」；AI 分组说的是「合集该怎么分」，那是候选不是提案。
  it('审核中心把元数据说成提案、把 AI 分组说成候选', () => {
    expect(zhCN['reviewCenter.description']).toContain('元数据提案');
    expect(zhCN['reviewCenter.description']).toContain('AI 分组候选');
  });

  it('元数据审核页与系列详情的对照列都用「提案」', () => {
    expect(zhCN['metadataReviews.description']).toContain('元数据提案');
    expect(zhCN['series.metadataReview.proposed']).toBe('提案值');
  });
});

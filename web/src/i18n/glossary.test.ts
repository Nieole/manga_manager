/**
 * 守中文词条只用 CONTEXT.md 的规范词：禁用词一旦出现在面向用户那一层就变红。
 * 判据是正则而不是子串，中间插得下字的复合词写成带间隔的形态——「动态规则合集」正是这样漏过去的。
 * 同一个字在别的语境下本来就对的（作品群的「宇宙」对上系列关系的「同宇宙」），按词条登记进 allow。
 * 「建议」「候选」这类整词都要看上下文的不进表，一刀切会误伤，交给评审。
 */

import { describe, expect, it } from 'vitest';
import { messages as zhCN } from './locales/zh-CN';

interface BannedTerm {
  pattern: RegExp;
  wrong: string;
  right: string;
  allow?: Record<string, string>;
}

// 每条都是 CONTEXT.md 里写明的 _Avoid_ 词，right 是它该让位给谁。
const BANNED_TERMS: BannedTerm[] = [
  { pattern: /书库/, wrong: '书库', right: '资料库' },
  { pattern: /资源库/, wrong: '资源库', right: '资料库' },
  { pattern: /外部[^，。、；]{0,2}目录/, wrong: '外部目录', right: '外部库' },
  { pattern: /远端库/, wrong: '远端库', right: '外部库' },
  { pattern: /智能[^，。、；]{0,3}合集/, wrong: '智能合集', right: '智能书架' },
  { pattern: /动态[^，。、；]{0,3}合集/, wrong: '动态合集', right: '智能书架' },
  {
    pattern: /宇宙/,
    wrong: '宇宙',
    right: '作品群',
    allow: {
      'readingLists.formDescription': '「同宇宙作品」说的是系列之间的关系，不是推导出来的作品群',
      'series.relations.description': '同上，这一条讲的就是关系类型',
      'series.relations.type.same_universe': '关系类型名，对应 same_universe',
      'series.relations.type.alternate_story': '关系类型名，对应 alternate_story',
    },
  },
  { pattern: /进行中/, wrong: '进行中', right: '活动态（三种状态的合称，不是「运行中」）' },
];

describe('zh-CN 词条的用词', () => {
  it.each(BANNED_TERMS.map((term) => [term.wrong, term.right, term] as const))(
    '不出现禁用词「%s」，该写「%s」',
    (wrong, right, term) => {
      const hits = Object.keys(zhCN).filter((key) => term.pattern.test(zhCN[key]));
      const allowed = Object.keys(term.allow ?? {});
      expect(hits.sort(), `这些词条把「${right}」写成了「${wrong}」：${hits.join(', ')}`).toEqual(allowed.sort());
    },
  );
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

describe('任务的层级叫作用域', () => {
  // 「范围」在搜索与覆盖率那边是对的中文，整词禁掉会误伤，所以只在任务作用域这一族词条上守。
  it('日志与任务筛选里的 scope 都写作用域', () => {
    const scopeKeys = Object.keys(zhCN).filter((key) => /^(logs\.taskScope|task\.scope)\./.test(key));
    expect(scopeKeys.length).toBeGreaterThan(0);
    expect(scopeKeys.filter((key) => zhCN[key].includes('范围'))).toEqual([]);
    expect(zhCN['logs.taskScope.all']).toBe('全部作用域');
  });
});

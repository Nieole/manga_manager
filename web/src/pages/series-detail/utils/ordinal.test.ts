import { describe, expect, it } from 'vitest';
import { compareOrdinalLabels } from './ordinal';

describe('compareOrdinalLabels', () => {
  // 回归：带年份前缀的日文卷标签，应按「巻」序数(1,2,3)排序，而不是被首个数字(年份)带偏。
  it('sorts Japanese volume labels by the 巻 ordinal, not the leading year', () => {
    const sorted = ['2020年 第3巻', '2018年 第2巻', '2019年 第1巻'].sort(compareOrdinalLabels);
    expect(sorted).toEqual(['2019年 第1巻', '2018年 第2巻', '2020年 第3巻']);
  });

  it('sorts Chinese 卷 labels by the ordinal', () => {
    const sorted = ['第3卷', '第10卷', '第2卷'].sort(compareOrdinalLabels);
    expect(sorted).toEqual(['第2卷', '第3卷', '第10卷']);
  });

  // 数字序数：10 应排在 2 之后（数值序，而非字典序）。
  it('orders numeric chapter ordinals numerically', () => {
    expect(compareOrdinalLabels('第2話', '第10話')).toBeLessThan(0);
  });
});

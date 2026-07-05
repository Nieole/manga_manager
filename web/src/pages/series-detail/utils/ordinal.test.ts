import { describe, expect, it } from 'vitest';
import { compareBooksForDisplay, compareOrdinalLabels } from './ordinal';
import type { Book } from '../types';

function makeBook(fields: Partial<Book> & { id: number }): Book {
  return {
    name: `Book ${fields.id}`,
    library_id: 1,
    volume: '',
    page_count: 0,
    ...fields,
  };
}

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

  it('parses Chinese numerals with unit words (十/二十/十一)', () => {
    const sorted = ['第二十卷', '第十一卷', '第九卷', '第十卷'].sort(compareOrdinalLabels);
    expect(sorted).toEqual(['第九卷', '第十卷', '第十一卷', '第二十卷']);
  });

  it('handles chapter zero and decimal (interstitial) ordinals', () => {
    expect(compareOrdinalLabels('第0話', '第1話')).toBeLessThan(0);
    // 1.5 话是插话，应排在 1 与 2 之间。
    expect(compareOrdinalLabels('第1話', '第1.5話')).toBeLessThan(0);
    expect(compareOrdinalLabels('第1.5話', '第2話')).toBeLessThan(0);
  });

  it('sorts labels carrying an ordinal ahead of labels without one', () => {
    // an!=null, bn==null -> a first.
    expect(compareOrdinalLabels('第2卷', 'Bonus')).toBeLessThan(0);
    expect(compareOrdinalLabels('Bonus', '第2卷')).toBeGreaterThan(0);
  });

  it('falls back to numeric-aware locale compare when neither has an ordinal', () => {
    expect(compareOrdinalLabels('Alpha', 'Beta')).toBeLessThan(0);
    expect(compareOrdinalLabels('Same', 'Same')).toBe(0);
  });

  it('breaks ties on equal ordinals with a stable text compare', () => {
    expect(compareOrdinalLabels('第2巻A', '第2巻B')).toBeLessThan(0);
  });
});

describe('compareBooksForDisplay', () => {
  it('orders by volume ordinal first (v2 before v10, numerically)', () => {
    const a = makeBook({ id: 1, volume: 'v2' });
    const b = makeBook({ id: 2, volume: 'v10' });
    expect(compareBooksForDisplay(a, b)).toBeLessThan(0);
    expect(compareBooksForDisplay(b, a)).toBeGreaterThan(0);
  });

  it('falls back to the name ordinal when volumes tie', () => {
    const a = makeBook({ id: 1, volume: '', name: '第1話' });
    const b = makeBook({ id: 2, volume: '', name: '第2話' });
    expect(compareBooksForDisplay(a, b)).toBeLessThan(0);
  });

  it('sorts a mixed volume list into ascending ordinal order', () => {
    const books = [
      makeBook({ id: 1, volume: '第10巻' }),
      makeBook({ id: 2, volume: '第2巻' }),
      makeBook({ id: 3, volume: '第1巻' }),
    ];
    const order = [...books].sort(compareBooksForDisplay).map((b) => b.volume);
    expect(order).toEqual(['第1巻', '第2巻', '第10巻']);
  });
});

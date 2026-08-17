// 守住批量结果里 conflict 与 failed 的分野：被别人抢先处理掉的条目不算失败、也不把
// 汇总提示置成错误色。混回 failed 会让用户为一堆并没有坏掉的东西看到红色提示。

import { describe, expect, it } from 'vitest';
import {
  accumulateBulkOutcome,
  bulkToastSeverity,
  emptyBulkOutcome,
  mergeBulkOutcomes,
} from './metadataReviewBulk';

describe('accumulateBulkOutcome', () => {
  it('folds applied and rejected into a single done count', () => {
    const totals = accumulateBulkOutcome(emptyBulkOutcome(), { applied: [1, 2], rejected: [3] });
    expect(totals.done).toBe(3);
  });

  it('keeps conflict out of failed', () => {
    const totals = accumulateBulkOutcome(emptyBulkOutcome(), { conflict: [1, 2], failed: [3] });
    expect(totals.conflict).toBe(2);
    expect(totals.failed).toBe(1);
  });

  it('treats missing buckets as zero', () => {
    expect(accumulateBulkOutcome(emptyBulkOutcome(), {})).toEqual(emptyBulkOutcome());
  });

  it('accumulates across pages without mutating the input', () => {
    const first = accumulateBulkOutcome(emptyBulkOutcome(), { applied: [1], conflict: [2] });
    const second = accumulateBulkOutcome(first, { partial: [3], skipped: [4], conflict: [5] });
    expect(second).toEqual({ done: 1, partial: 1, skipped: 1, conflict: 2, failed: 0 });
    expect(first).toEqual({ done: 1, partial: 0, skipped: 0, conflict: 1, failed: 0 });
  });
});

describe('mergeBulkOutcomes', () => {
  it('sums the apply side and the reject side bucket by bucket', () => {
    const applied = { done: 2, partial: 1, skipped: 0, conflict: 3, failed: 1 };
    const rejected = { done: 4, partial: 0, skipped: 2, conflict: 1, failed: 0 };
    expect(mergeBulkOutcomes(applied, rejected)).toEqual({
      done: 6,
      partial: 1,
      skipped: 2,
      conflict: 4,
      failed: 1,
    });
  });
});

describe('bulkToastSeverity', () => {
  it('stays success when everything was preempted', () => {
    expect(bulkToastSeverity({ done: 0, partial: 0, skipped: 0, conflict: 9, failed: 0 })).toBe('success');
  });

  it('turns to error only when something actually failed', () => {
    expect(bulkToastSeverity({ done: 0, partial: 0, skipped: 0, conflict: 9, failed: 1 })).toBe('error');
  });

  it('is success when nothing failed', () => {
    expect(bulkToastSeverity(emptyBulkOutcome())).toBe('success');
  });
});

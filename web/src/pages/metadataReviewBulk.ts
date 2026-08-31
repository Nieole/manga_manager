/** MetadataBulkResponse 是批量应用/拒绝的响应形状；每个桶都可能缺席（后端对空桶省略字段）。 */
export interface MetadataBulkResponse {
  applied?: number[];
  rejected?: number[];
  partial?: number[];
  skipped?: number[];
  conflict?: number[];
  failed?: number[];
}

/** BulkOutcomeTotals 是汇总提示要的计数，桶与 MetadataBulkResponse 一一对应。 */
export interface BulkOutcomeTotals {
  /** done 合并了 applied 与 rejected —— 两个端点各自只会填其中一个。 */
  done: number;
  partial: number;
  skipped: number;
  conflict: number;
  failed: number;
}

export const emptyBulkOutcome = (): BulkOutcomeTotals => ({
  done: 0,
  partial: 0,
  skipped: 0,
  conflict: 0,
  failed: 0,
});

const count = (bucket?: number[]) => bucket?.length || 0;

/** mergeBulkOutcomes 逐桶相加，用于把应用侧与拒绝侧的计数合成一条提示。 */
export function mergeBulkOutcomes(a: BulkOutcomeTotals, b: BulkOutcomeTotals): BulkOutcomeTotals {
  return {
    done: a.done + b.done,
    partial: a.partial + b.partial,
    skipped: a.skipped + b.skipped,
    conflict: a.conflict + b.conflict,
    failed: a.failed + b.failed,
  };
}

/** accumulateBulkOutcome 把一页响应折进累计值，返回新对象。 */
export function accumulateBulkOutcome(totals: BulkOutcomeTotals, res: MetadataBulkResponse): BulkOutcomeTotals {
  return mergeBulkOutcomes(totals, {
    done: count(res.applied) + count(res.rejected),
    // partial：写入了一部分提案，但整条审核仍留在收件箱里等着处理
    //（fill_empty 模式筛掉的、或已被锁定的字段）。不能并进 done，
    // 否则用户会以为这些条目已经消失了，刷新后又看到它们还在。
    partial: count(res.partial),
    skipped: count(res.skipped),
    conflict: count(res.conflict),
    failed: count(res.failed),
  });
}

/**
 * bulkToastSeverity 只看 failed。
 *
 * 被抢先、被跳过、只应用了一部分都不是错误——让它们把提示置成红色，用户会去找一个
 * 并不存在的故障。
 */
export function bulkToastSeverity(totals: BulkOutcomeTotals): 'error' | 'success' {
  return totals.failed > 0 ? 'error' : 'success';
}

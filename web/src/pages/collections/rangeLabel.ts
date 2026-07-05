// buildBoundedRangeLabel 生成区间标签：双边用「a–b」，单边用「≥a」/「≤b」，都空则空串。
// 修复：此前只要有一边就输出连字符，导致「★ 8–」尾部悬空、「★ –6」前导 dash。
export function buildBoundedRangeLabel(
  min: number | null | undefined,
  max: number | null | undefined,
  suffix = '',
): string {
  const hasMin = min != null;
  const hasMax = max != null;
  if (hasMin && hasMax) return `${min}${suffix}–${max}${suffix}`;
  if (hasMin) return `≥${min}${suffix}`;
  if (hasMax) return `≤${max}${suffix}`;
  return '';
}

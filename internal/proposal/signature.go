// 本文件是提案去重的比对口径：两份提案「逐字相同」是什么意思。

package proposal

import (
	"strconv"
	"strings"

	"manga-manager/internal/database"
)

func normalizeValue(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func draftSignature(changes []fieldDraft, sourceID int64) map[string]string {
	signature := make(map[string]string, len(changes)+1)
	for _, change := range changes {
		signature[change.Name] = normalizeValue(change.Current) + "\x00" + normalizeValue(change.Proposed)
	}
	// 把来源条目 ID 纳入签名：不同来源（如 Bangumi 不同 subject）即使字段 diff 相同
	// 也应视为不同候选，分别入队，避免复用旧提案时丢弃用户选中条目的 source_url。
	signature["\x00source_id"] = strconv.FormatInt(sourceID, 10)
	return signature
}

// rowsSignature 给已入库的字段行算签名。
//
// locked 是**当前**的锁定集，不是行上那个入队瞬间的快照：新一轮刮削产出的差异已经把当前
// 锁定的字段筛掉了，这边若不同口径地筛，「入队后用户又加了锁」就会让两边签名永远对不上，
// 于是每次刮削都为同一个系列再堆一条几乎相同的提案。
func rowsSignature(fields []database.MetadataReviewField, sourceID int64, locked map[string]bool) map[string]string {
	signature := make(map[string]string, len(fields)+1)
	for _, field := range fields {
		if locked[field.FieldName] {
			continue
		}
		signature[field.FieldName] = normalizeValue(field.CurrentValue) + "\x00" + normalizeValue(field.ProposedValue)
	}
	signature["\x00source_id"] = strconv.FormatInt(sourceID, 10)
	return signature
}

func signaturesEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		if right[key] != leftValue {
			return false
		}
	}
	return true
}

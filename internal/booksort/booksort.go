package booksort

import (
	"database/sql"
	"regexp"
	"strconv"
	"strings"

	"manga-manager/internal/database"
)

var (
	arabicOrdinalRegexp = regexp.MustCompile(`第?\s*(\d+(\.\d+)?)\s*(话|話|回|章|卷|集|册|冊|期|部)`)
	arabicNumberRegexp  = regexp.MustCompile(`\d+(\.\d+)?`)
	chineseNumberRegexp = regexp.MustCompile(`第?\s*([零〇一二两三四五六七八九十百千万萬壹贰貳叁參肆伍陆陸柒捌玖拾佰仟]+)\s*(话|話|回|章|卷|集|册|冊|期|部)`)
)

// ExtractSortNumber returns the first chapter-like number in a book or volume label.
// It supports both Arabic numbers and common Chinese ordinal forms such as 第一话、第二话、第十一话.
func ExtractSortNumber(label string) (float64, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return 0, false
	}
	if match := arabicOrdinalRegexp.FindStringSubmatch(label); len(match) >= 2 {
		if value, err := strconv.ParseFloat(match[1], 64); err == nil {
			return value, true
		}
	}
	if match := chineseNumberRegexp.FindStringSubmatch(label); len(match) >= 2 {
		if value, ok := parseChineseInteger(match[1]); ok {
			return float64(value), true
		}
	}
	if match := arabicNumberRegexp.FindString(label); match != "" {
		if value, err := strconv.ParseFloat(match, 64); err == nil {
			return value, true
		}
	}
	return 0, false
}

func EffectiveBookSortNumber(book database.Book) (float64, bool) {
	if value, ok := ExtractSortNumber(book.Name); ok {
		return value, true
	}
	if book.Title.Valid {
		if value, ok := ExtractSortNumber(book.Title.String); ok {
			return value, true
		}
	}
	if book.Number.Valid {
		if value, err := strconv.ParseFloat(strings.TrimSpace(book.Number.String), 64); err == nil {
			return value, true
		}
		if value, ok := ExtractSortNumber(book.Number.String); ok {
			return value, true
		}
	}
	return nullableSortNumber(book.SortNumber)
}

func nullableSortNumber(value sql.NullFloat64) (float64, bool) {
	if !value.Valid {
		return 0, false
	}
	return value.Float64, true
}

func CompareBooks(a, b database.Book) int {
	if cmp := CompareLabels(a.Volume, b.Volume); cmp != 0 {
		return cmp
	}
	aSort, aOK := EffectiveBookSortNumber(a)
	bSort, bOK := EffectiveBookSortNumber(b)
	if aOK && bOK && aSort != bSort {
		if aSort < bSort {
			return -1
		}
		return 1
	}
	if aOK != bOK {
		if aOK {
			return -1
		}
		return 1
	}
	return CompareLabels(a.Name, b.Name)
}

func CompareLabels(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if strings.EqualFold(a, b) {
		return 0
	}
	aNumber, aOK := ExtractSortNumber(a)
	bNumber, bOK := ExtractSortNumber(b)
	if aOK && bOK && aNumber != bNumber {
		if aNumber < bNumber {
			return -1
		}
		return 1
	}
	if aOK != bOK {
		if aOK {
			return -1
		}
		return 1
	}
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	if al < bl {
		return -1
	}
	if al > bl {
		return 1
	}
	if a < b {
		return -1
	}
	return 1
}

func parseChineseInteger(input string) (int, bool) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, false
	}
	total := 0
	section := 0
	current := 0
	seen := false
	for _, r := range input {
		if digit, ok := chineseDigitValue(r); ok {
			current = digit
			seen = true
			continue
		}
		unit, ok := chineseUnitValue(r)
		if !ok {
			return 0, false
		}
		seen = true
		if unit == 10000 {
			section += current
			if section == 0 {
				section = 1
			}
			total += section * unit
			section = 0
			current = 0
			continue
		}
		if current == 0 {
			current = 1
		}
		section += current * unit
		current = 0
	}
	if !seen {
		return 0, false
	}
	return total + section + current, true
}

func chineseDigitValue(r rune) (int, bool) {
	switch r {
	case '零', '〇':
		return 0, true
	case '一', '壹':
		return 1, true
	case '二', '两', '贰', '貳':
		return 2, true
	case '三', '叁', '參':
		return 3, true
	case '四', '肆':
		return 4, true
	case '五', '伍':
		return 5, true
	case '六', '陆', '陸':
		return 6, true
	case '七', '柒':
		return 7, true
	case '八', '捌':
		return 8, true
	case '九', '玖':
		return 9, true
	default:
		return 0, false
	}
}

func chineseUnitValue(r rune) (int, bool) {
	switch r {
	case '十', '拾':
		return 10, true
	case '百', '佰':
		return 100, true
	case '千', '仟':
		return 1000, true
	case '万', '萬':
		return 10000, true
	default:
		return 0, false
	}
}

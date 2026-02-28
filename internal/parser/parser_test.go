package parser

import (
	"testing"
)

func TestNaturalCompare(t *testing.T) {
	tests := []struct {
		a, b     string
		expected bool
	}{
		// 1. 自然序测试
		{"1.jpg", "2.jpg", true},
		{"2.jpg", "10.jpg", true},
		{"01.jpg", "1.jpg", true},

		// 2. 封面关键字优先测试 (跨目录层级)
		{"cover.jpg", "001.jpg", true},
		{"封面.jpg", "001.jpg", true},
		{"001.jpg", "front.png", false},
		{"Cover/001.jpg", "Ad.jpg", true},    // 子目录封面优于根目录非封面
		{"Scans/00.jpg", "01.jpg", true},     // Scans 目录优于根目录
		{"A/Cover/01.jpg", "B/01.jpg", true}, // 同级目录，Cover 优先

		// 3. 排除关键字测试
		{"cover_back.jpg", "001.jpg", false},
		{"001.jpg", "ad.jpg", true},

		// 4. 深度优先 (同为封面或同非封面)
		{"cover.jpg", "data/cover.jpg", true},
		{"a/001.jpg", "b/c/001.jpg", true},

		// 5. 综合场景
		{"p000.jpg", "001.jpg", false}, // p000 应该排在 001 之后（如果没有 cover 关键字的情况下按文件名排）
	}

	for _, tt := range tests {
		got := naturalCompare(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("naturalCompare(%q, %q) = %v; want %v", tt.a, tt.b, got, tt.expected)
		}
	}
}

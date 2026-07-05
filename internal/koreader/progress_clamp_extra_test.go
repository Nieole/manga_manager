// 业务说明：本文件是业务回归测试，覆盖 KOReader 进度上报的百分比夹取（[0,1]）。
// 设备可能上报越界百分比，服务端必须先夹取再持久化，避免污染书页进度投影。

package koreader

import (
	"context"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// TestSaveProgressClampsPercentage 复用 newTestService，验证 SaveProgress 把越界百分比夹取到 [0,1]。
func TestSaveProgressClampsPercentage(t *testing.T) {
	service, store, _ := newTestService(t, config.KOReaderMatchModeBinaryHash)
	ctx := context.Background()

	if _, err := store.CreateKOReaderAccount(ctx, database.CreateKOReaderAccountParams{
		Username: "reader",
		SyncKey:  "secret-key",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateKOReaderAccount failed: %v", err)
	}
	creds := Credentials{Username: "reader", Key: HashKey("secret-key")}

	cases := []struct {
		name  string
		doc   string
		input float64
		want  float64
	}{
		{"above-one", "doc-high", 1.5, 1.0},
		{"below-zero", "doc-low", -0.5, 0.0},
		{"in-range", "doc-mid", 0.42, 0.42},
	}
	for _, tc := range cases {
		result, err := service.SaveProgress(ctx, creds, ProgressPayload{
			Document:   tc.doc,
			Progress:   "/body/DocFragment[1]",
			Percentage: tc.input,
			Device:     "Kobo",
			DeviceID:   "device-a",
		})
		if err != nil {
			t.Fatalf("%s SaveProgress failed: %v", tc.name, err)
		}
		if result.Record.Percentage != tc.want {
			t.Fatalf("%s: stored percentage = %v, want %v", tc.name, result.Record.Percentage, tc.want)
		}
	}
}

package api

import (
	"manga-manager/internal/application"
	"time"
)

const cleanupRunHistoryTaskKey = application.CleanupRunHistoryTaskKey

func retentionAge(days int) time.Duration { return application.RetentionAge(days) }

package tools

import (
	"context"
	"time"
)

func handleGetTodayDate(_ context.Context, _ string) string {
	return time.Now().Format(time.RFC3339)
}

package tools

import (
	"context"
	"time"
)

func handleGetTodayDate(_ context.Context, _ string) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}

package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gbchd/todo/internal/service/todo"
)

// parseDate parses a literal YYYY-MM-DD date or a relative shorthand
// (today, tomorrow, +Nd, +Nw), relative to now.
func parseDate(s string, now time.Time) (time.Time, error) {
	switch s {
	case "today":
		return truncateDate(now), nil
	case "tomorrow":
		return truncateDate(now).AddDate(0, 0, 1), nil
	}
	if rest, ok := strings.CutPrefix(s, "+"); ok {
		if n, ok := strings.CutSuffix(rest, "d"); ok {
			if days, err := strconv.Atoi(n); err == nil {
				return truncateDate(now).AddDate(0, 0, days), nil
			}
		}
		if n, ok := strings.CutSuffix(rest, "w"); ok {
			if weeks, err := strconv.Atoi(n); err == nil {
				return truncateDate(now).AddDate(0, 0, weeks*7), nil
			}
		}
	}
	t, err := time.Parse(todo.DateLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYY-MM-DD, today, tomorrow, +Nd, +Nw)", s)
	}
	return t, nil
}

func truncateDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(todo.DateLayout)
}

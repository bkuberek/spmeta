package model

import "fmt"

// FormatDuration formats milliseconds as m:ss below one hour and h:mm:ss from
// one hour up. Negative values format as 0:00; partial seconds are dropped.
func FormatDuration(ms int64) string {
	total := max(ms, 0) / 1000
	h, m, s := total/3600, total/60%60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

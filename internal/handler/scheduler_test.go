package handler

import (
	"testing"
	"time"
)

func TestZoomReminderTime(t *testing.T) {
	loc := time.FixedZone("Asia/Almaty", 5*60*60)
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"monday 20:00", time.Date(2026, 6, 22, 20, 0, 30, 0, loc), true},
		{"thursday 20:00", time.Date(2026, 6, 25, 20, 0, 0, 0, loc), true},
		{"monday 19:59", time.Date(2026, 6, 22, 19, 59, 59, 0, loc), false},
		{"monday 20:01", time.Date(2026, 6, 22, 20, 1, 0, 0, loc), false},
		{"wednesday 20:00", time.Date(2026, 6, 24, 20, 0, 0, 0, loc), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, got := zoomReminderTime(tc.now)
			if got != tc.want {
				t.Fatalf("zoomReminderTime() = %v, want %v", got, tc.want)
			}
		})
	}
}

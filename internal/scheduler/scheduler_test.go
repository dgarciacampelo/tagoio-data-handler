package scheduler

import (
	"testing"
	"time"
)

func TestNextRun(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]struct {
		now  time.Time
		want time.Time
	}{
		"later today": {
			now:  time.Date(2026, 10, 22, 10, 0, 0, 0, madrid),
			want: time.Date(2026, 10, 22, 21, 15, 0, 0, madrid),
		},
		"exactly now runs tomorrow": {
			now:  time.Date(2026, 10, 22, 21, 15, 0, 0, madrid),
			want: time.Date(2026, 10, 23, 21, 15, 0, 0, madrid),
		},
		"from UTC across midnight in Madrid": {
			now:  time.Date(2026, 10, 22, 22, 30, 0, 0, time.UTC), // 00:30 on the 23rd in Madrid
			want: time.Date(2026, 10, 23, 21, 15, 0, 0, madrid),
		},
		"over the DST change": {
			now:  time.Date(2026, 10, 24, 22, 0, 0, 0, madrid), // Clocks go back on the 25th
			want: time.Date(2026, 10, 25, 21, 15, 0, 0, madrid),
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := NextRun(tt.now, 21, 15, madrid)
			if !got.Equal(tt.want) {
				t.Fatalf("NextRun = %v; want %v", got, tt.want)
			}
		})
	}
}

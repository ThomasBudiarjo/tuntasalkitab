package handlers

import "testing"

func TestCalculateStreak(t *testing.T) {
	tests := []struct {
		name      string
		completed map[int]bool
		today     int
		want      int
	}{
		{
			name:      "includes today when completed",
			completed: map[int]bool{7: true, 8: true, 9: true},
			today:     9,
			want:      3,
		},
		{
			name:      "preserves yesterday streak before today is completed",
			completed: map[int]bool{7: true, 8: true},
			today:     9,
			want:      2,
		},
		{
			name:      "stops at a missed day",
			completed: map[int]bool{5: true, 7: true, 8: true, 9: true},
			today:     9,
			want:      3,
		},
		{
			name:      "returns zero without a current streak",
			completed: map[int]bool{5: true},
			today:     9,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateStreak(tt.completed, tt.today); got != tt.want {
				t.Fatalf("calculateStreak() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCountMissedDays(t *testing.T) {
	completed := map[int]bool{1: true, 3: true, 5: true, 6: true}
	if got, want := countMissedDays(completed, 6), 2; got != want {
		t.Fatalf("countMissedDays() = %d, want %d", got, want)
	}
}

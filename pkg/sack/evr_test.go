package sack

import "testing"

func TestEVRCmp(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{
		{"1.0-1", "1.0-1", 0},
		{"1.0-1", "1.0-2", -1},
		{"1.10-1", "1.9-1", 1},
		{"1:1.0-1", "2.0-1", 1},
		{"0:1.0-1", "1.0-1", 0},
		{"1.0~rc1-1", "1.0-1", -1},
		{"1.0^git1-1", "1.0-1", 1},
		{"1.0^git1-1", "1.0.1-1", -1},
		{"1.0a-1", "1.0-1", 1},
		{"1.0-1.fc45", "1.0-1.fc44", 1},
		{"2.0.01-1", "2.0.1-1", 0},
		{"5.3.15-2.fc45", "5.3.9-1.fc45", 1},
		{"1.0.a-1", "1.0.1-1", -1},
	} {
		if got := EVRCmp(tt.a, tt.b); got != tt.want {
			t.Errorf("EVRCmp(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
		if got := EVRCmp(tt.b, tt.a); got != -tt.want {
			t.Errorf("EVRCmp(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
		}
	}
}

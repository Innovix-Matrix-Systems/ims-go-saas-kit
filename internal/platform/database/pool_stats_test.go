package database

import "testing"

func TestPoolBusy(t *testing.T) {
	cases := []struct {
		active, max int32
		want        bool
	}{
		{0, 20, false},
		{15, 20, false},
		{16, 20, true},
		{20, 20, true},
		{1, 1, true},
		{0, 0, false},
	}
	for _, c := range cases {
		if got := poolBusy(c.active, c.max); got != c.want {
			t.Errorf("poolBusy(%d, %d) = %v, want %v", c.active, c.max, got, c.want)
		}
	}
}

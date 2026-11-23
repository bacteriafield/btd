package rpmdb

import (
	"os"
	"testing"
)

func TestVarint(t *testing.T) {
	for _, tt := range []struct {
		in   []byte
		want int64
		n    int
	}{
		{[]byte{0x05}, 5, 1},
		{[]byte{0x81, 0x00}, 128, 2},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, -1, 9},
		{[]byte{0x81}, 0, 0}, // truncated
	} {
		if got, n := varint(tt.in); got != tt.want || n != tt.n {
			t.Errorf("varint(%x) = %d, %d; want %d, %d", tt.in, got, n, tt.want, tt.n)
		}
	}
}

// TestInstalled reads every installed package of this system's rpmdb.
func TestInstalled(t *testing.T) {
	if _, err := os.Stat(Path); err != nil {
		t.Skip("no rpmdb")
	}
	db, err := Open(Path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hnums, err := db.Find("Name", func(string) bool { return true })
	if err != nil || len(hnums) == 0 {
		t.Fatalf("Find = %d packages, %v", len(hnums), err)
	}
	for _, h := range hnums {
		p, err := db.Package(h)
		if err != nil {
			t.Fatal(err)
		}
		if p.Name == "" || p.Version == "" {
			t.Fatalf("package %d: no name or version: %+v", h, p)
		}
	}
}

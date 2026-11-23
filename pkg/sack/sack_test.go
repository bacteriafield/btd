package sack

import "testing"

// TestLoad reads this system's dnf5 cache and rpmdb, loading repositories
// in parallel (run it with -race). It skips without a usable cache; the
// compatibility tests in package benchmark fail in that case instead.
func TestLoad(t *testing.T) {
	s, err := Load()
	if err != nil {
		t.Skipf("no usable dnf5 cache: %v", err)
	}
	if len(s.Search([]string{"bash"})) == 0 {
		t.Error("Search(bash) found nothing")
	}
	res, err := s.Info([]string{"bash"})
	if err != nil || len(res.Installed) == 0 {
		t.Errorf("Info(bash) = %+v, %v; want bash installed", res, err)
	}
}

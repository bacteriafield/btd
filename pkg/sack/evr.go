package sack

import "strings"

// EVRCmp compares two "[epoch:]version[-release]" strings as rpm does:
// epoch numerically, then version, then release with rpmvercmp. A missing
// epoch is 0.
func EVRCmp(a, b string) int {
	ea, va, ra := SplitEVR(a)
	eb, vb, rb := SplitEVR(b)
	if c := rpmvercmp(ea, eb); c != 0 {
		return c
	}
	if c := rpmvercmp(va, vb); c != 0 {
		return c
	}
	return rpmvercmp(ra, rb)
}

// SplitEVR splits "[epoch:]version[-release]"; the epoch defaults to "0".
func SplitEVR(evr string) (epoch, version, release string) {
	epoch = "0"
	if i := strings.IndexByte(evr, ':'); i >= 0 {
		epoch, evr = evr[:i], evr[i+1:]
	}
	version = evr
	if i := strings.LastIndexByte(evr, '-'); i >= 0 {
		version, release = evr[:i], evr[i+1:]
	}
	return epoch, version, release
}

// rpmvercmp is rpm's version segment comparison (lib/rpmvercmp.c): the
// strings are split into runs of digits and of letters, compared pairwise,
// numbers numerically and above letters; "~" sorts before anything, even
// the end of the string ("1.0~rc1" < "1.0"), and "^" after the end but
// before anything else ("1.0" < "1.0^git1" < "1.0.1").
func rpmvercmp(a, b string) int {
	if a == b {
		return 0
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for i < len(a) && !isAlnum(a[i]) && a[i] != '~' && a[i] != '^' {
			i++
		}
		for j < len(b) && !isAlnum(b[j]) && b[j] != '~' && b[j] != '^' {
			j++
		}

		if at(a, i) == '~' || at(b, j) == '~' {
			if at(a, i) != '~' {
				return 1
			}
			if at(b, j) != '~' {
				return -1
			}
			i, j = i+1, j+1
			continue
		}
		if at(a, i) == '^' || at(b, j) == '^' {
			switch {
			case i == len(a):
				return -1
			case j == len(b):
				return 1
			case a[i] != '^':
				return 1
			case b[j] != '^':
				return -1
			}
			i, j = i+1, j+1
			continue
		}
		if i == len(a) || j == len(b) {
			break
		}

		// Take the next run of the same kind from both strings.
		isNum := isDigit(a[i])
		run := isAlpha
		if isNum {
			run = isDigit
		}
		si, sj := i, j
		for i < len(a) && run(a[i]) {
			i++
		}
		for j < len(b) && run(b[j]) {
			j++
		}
		if sj == j { // different kinds: numbers are newer than letters
			if isNum {
				return 1
			}
			return -1
		}

		x, y := a[si:i], b[sj:j]
		if isNum {
			x, y = strings.TrimLeft(x, "0"), strings.TrimLeft(y, "0")
			if len(x) != len(y) {
				if len(x) > len(y) {
					return 1
				}
				return -1
			}
		}
		if c := strings.Compare(x, y); c != 0 {
			return c
		}
	}
	switch {
	case i == len(a) && j == len(b):
		return 0
	case i == len(a):
		return -1
	}
	return 1
}

func at(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isAlnum(c byte) bool { return isDigit(c) || isAlpha(c) }

// Package menu prints numbered results and parses which ones the user picks.
package menu

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/leonelquinteros/gotext"
)

// Item is one menu entry; Source is shown as its [label].
type Item struct {
	Source, Name, Version, Description string
}

// Print numbers items from 1, most relevant first. With bottomUp the list is
// printed reversed so number 1 sits right above the prompt.
func Print(w io.Writer, items []Item, bottomUp, singleLine bool) {
	for i := range items {
		if bottomUp {
			i = len(items) - 1 - i
		}
		it := items[i]
		head := strings.Join(strings.Fields(fmt.Sprintf("%d [%s] %s %s", i+1, it.Source, it.Name, it.Version)), " ")
		switch {
		case it.Description == "":
			fmt.Fprintln(w, head)
		case singleLine:
			fmt.Fprintf(w, "%s - %s\n", head, it.Description)
		default:
			fmt.Fprintf(w, "%s\n    %s\n", head, it.Description)
		}
	}
}

// ParseSelection turns input such as "1 3 2-4" into 0-based indexes of a
// menu with n items, in input order and without duplicates.
func ParseSelection(input string, n int) ([]int, error) {
	var picked []int
	seen := map[int]bool{}
	for _, field := range strings.FieldsFunc(input, func(r rune) bool { return unicode.IsSpace(r) || r == ',' }) {
		lo, hi, isRange := strings.Cut(field, "-")
		if !isRange {
			hi = lo
		}
		from, errFrom := strconv.Atoi(lo)
		to, errTo := strconv.Atoi(hi)
		if errFrom != nil || errTo != nil || from < 1 || to > n || from > to {
			return nil, errors.New(gotext.Get("invalid selection: %s", field))
		}
		for num := from; num <= to; num++ {
			if !seen[num] {
				seen[num] = true
				picked = append(picked, num-1)
			}
		}
	}
	return picked, nil
}

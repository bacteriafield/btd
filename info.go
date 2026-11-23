package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/leonelquinteros/gotext"

	"github.com/bacteriafield/btd/pkg/dnf"
	"github.com/bacteriafield/btd/pkg/sack"
	"github.com/bacteriafield/btd/pkg/settings"
)

// nativeInfo is `btd info <spec>...` answered from dnf5's cache, with the
// output of `dnf5 info` (libdnf5-cli/output/packageinfo.cpp). It returns
// false when dnf5 has to answer instead: options given, specs other than
// package names or globs, or no usable cache.
func nativeInfo(cfg *settings.Configuration, specs []string) (int, bool) {
	for _, spec := range specs {
		if strings.HasPrefix(spec, "-") {
			return 0, false
		}
	}
	s := dnf.Native(cfg)
	if s == nil {
		return 0, false
	}
	res, err := s.Info(specs)
	if err != nil {
		cfg.Debugf("running dnf5 info: %v", err)
		return 0, false
	}

	loc := detectLocale()
	tr := newDNF5Translator(loc)
	if len(res.Installed) == 0 && len(res.Available) == 0 {
		if len(s.Repos) == 0 {
			fmt.Fprintln(os.Stderr, tr.dnf5.Get("No matches found: no repositories are enabled."))
		} else {
			fmt.Fprintln(os.Stderr, tr.dnf5.Get("No matches found."))
		}
		return 1, true
	}
	for _, spec := range res.Unmatched {
		fmt.Fprintln(os.Stderr, strings.Replace(tr.dnf5.Get(`No matches found for "{}".`), "{}", spec, 1))
	}

	out := newInfoWriter(os.Stdout, tr, s)
	out.utf8 = loc.utf8
	out.print(res)
	return 0, true
}

// dnfLocale is the locale dnf5 ends up with: it calls setlocale(LC_ALL, "")
// and falls back to C.UTF-8 when that fails, which happens when a locale
// in the environment is not installed.
type dnfLocale struct {
	utf8      bool // the character set is UTF-8 (else ASCII)
	translate bool // messages are translated
}

func detectLocale() dnfLocale {
	get := func(category string) string {
		for _, env := range []string{"LC_ALL", category, "LANG"} {
			if v := os.Getenv(env); v != "" {
				return v
			}
		}
		return ""
	}
	isC := func(v string) bool { return v == "" || v == "C" || v == "POSIX" }
	ctype, messages := get("LC_CTYPE"), get("LC_MESSAGES")
	for _, v := range []string{ctype, messages} {
		if !isC(v) && !localeInstalled(v) {
			return dnfLocale{utf8: true}
		}
	}
	// ponytail: any non-C locale is taken as UTF-8; legacy charsets such as
	// pt_BR.ISO-8859-1 would print differently.
	return dnfLocale{utf8: !isC(ctype), translate: !isC(messages)}
}

// localeInstalled reports whether glibc can load locale (pt_BR.UTF-8):
// Fedora's langpacks install /usr/lib/locale/<lang>.<codeset> directories,
// glibc-all-langpacks an archive holding all of them.
func localeInstalled(locale string) bool {
	if _, err := os.Stat("/usr/lib/locale/locale-archive"); err == nil {
		return true
	}
	name, codeset, ok := strings.Cut(locale, ".")
	if ok { // glibc normalizes the codeset: UTF-8 -> utf8
		codeset, _, _ = strings.Cut(codeset, "@")
		name += "." + strings.ToLower(strings.ReplaceAll(codeset, "-", ""))
	}
	_, err := os.Stat("/usr/lib/locale/" + name)
	return err == nil
}

// dnf5Translator holds dnf5's own message catalogs, so btd's labels come
// out in the same language as dnf5's.
type dnf5Translator struct {
	dnf5, cli gotext.Translator // the "dnf5" and "libdnf5-cli" domains
}

func newDNF5Translator(loc dnfLocale) dnf5Translator {
	return dnf5Translator{dnf5: catalog("dnf5", loc), cli: catalog("libdnf5-cli", loc)}
}

// catalog loads dnf5's translations for domain, from the system locale
// directory dnf5 installs them to; without one, messages stay in English.
func catalog(domain string, loc dnfLocale) gotext.Translator {
	mo := gotext.NewMo()
	if !loc.translate {
		return mo
	}
	lang := language()
	for _, l := range []string{lang, strings.SplitN(lang, "_", 2)[0]} {
		file := "/usr/share/locale/" + l + "/LC_MESSAGES/" + domain + ".mo"
		if _, err := os.Stat(file); err == nil {
			mo.ParseFile(file)
			break
		}
	}
	return mo
}

// infoWriter prints packages like libsmartcols prints dnf5's info tables:
// a key column as wide as its longest key, " : ", then the value; on a
// terminal, values longer than the rest of the line are cut into chunks of
// that width (not at word boundaries) and the Name line is colored.
type infoWriter struct {
	w     io.Writer
	tr    dnf5Translator
	width int  // terminal width, 0 when not writing to a terminal
	utf8  bool // the locale's character set, see safeEncode
	color struct {
		on                 bool
		upgrade, reinstall string
	}
	installed map[string]string // name.arch -> EVR, to color available packages
}

func newInfoWriter(w *os.File, tr dnf5Translator, s *sack.Sack) *infoWriter {
	iw := &infoWriter{w: w, tr: tr, width: termWidth(w), installed: map[string]string{}}
	switch s.Option("color") {
	case "always":
		iw.color.on = true
	case "never":
		iw.color.on = false
	default:
		iw.color.on = iw.width > 0
	}
	iw.color.upgrade = colorEscape(s.Option("color_list_available_upgrade"), "bold,blue")
	iw.color.reinstall = colorEscape(s.Option("color_list_available_reinstall"), "bold,green")
	return iw
}

func (iw *infoWriter) print(res *sack.InfoResult) {
	for _, p := range res.Installed {
		na := p.Name + "." + p.Arch
		if cur, ok := iw.installed[na]; !ok || sack.EVRCmp(p.EVR, cur) > 0 {
			iw.installed[na] = p.EVR
		}
	}

	// Sections are separated by a blank line, and so are packages.
	sep := false
	section := func(heading string, n int, pkg func(i int)) {
		if n == 0 {
			return
		}
		if sep {
			fmt.Fprintln(iw.w)
		}
		fmt.Fprintln(iw.w, heading)
		for i := range n {
			if i > 0 {
				fmt.Fprintln(iw.w)
			}
			pkg(i)
		}
		sep = true
	}
	section(iw.tr.dnf5.Get("Installed packages"), len(res.Installed), func(i int) {
		p := res.Installed[i]
		iw.table(p.Name+"."+p.Arch, p.EVR, [][2]string{
			{"Name", p.Name},
			{"Epoch", fmt.Sprint(p.Epoch)},
			{"Version", p.Version},
			{"Release", p.Release},
			{"Architecture", p.Arch},
			{"Installed size", formatSize(p.Size)},
			{"Source", p.SourceRPM},
			{"From repository", p.FromRepo},
			{"Summary", p.Summary},
			{"URL", p.URL},
			{"License", p.License},
			{"Description", p.Description},
			{"Vendor", p.Vendor},
		})
	})
	section(iw.tr.dnf5.Get("Available packages"), len(res.Available), func(i int) {
		p := res.Available[i]
		iw.table(p.Name()+"."+p.Arch(), p.EVR(), [][2]string{
			{"Name", p.Name()},
			{"Epoch", p.Epoch()},
			{"Version", p.Version()},
			{"Release", p.Release()},
			{"Architecture", p.Arch()},
			{"Download size", formatSize(p.DownloadSize())},
			{"Installed size", formatSize(p.InstallSize())},
			{"Source", p.SourceRPM()},
			{"Repository", p.Repo.ID},
			{"Summary", p.Summary()},
			{"URL", p.URL()},
			{"License", p.License()},
			{"Description", p.Description()},
			{"Vendor", p.Vendor()},
		})
	})
}

// table prints one package; rows are (untranslated key, value). Source
// packages have no "Source" row; a description takes one row per line.
func (iw *infoWriter) table(na, evr string, rows [][2]string) {
	type row struct{ key, value string }
	var lines []row
	for _, r := range rows {
		key := iw.tr.cli.Get(r[0])
		switch {
		case r[0] == "Source" && strings.HasSuffix(na, ".src"):
		case r[0] == "Description":
			for i, line := range strings.Split(r[1], "\n") {
				if i > 0 {
					key = ""
				}
				lines = append(lines, row{key, safeEncode(line, iw.utf8)})
			}
		default:
			lines = append(lines, row{key, safeEncode(r[1], iw.utf8)})
		}
	}

	keyWidth := 0
	for _, l := range lines {
		keyWidth = max(keyWidth, utf8.RuneCountInString(l.key))
	}
	sep := iw.tr.cli.Get(" : ")
	valueWidth := iw.width - keyWidth - utf8.RuneCountInString(sep)

	for i, l := range lines {
		chunks := []string{l.value}
		if iw.width > 0 && valueWidth > 0 {
			chunks = chunk(l.value, valueWidth)
		}
		for j, c := range chunks {
			key := l.key
			if j > 0 {
				key = ""
			}
			line := key + strings.Repeat(" ", keyWidth-utf8.RuneCountInString(key)) + sep + c
			if i == 0 && iw.color.on { // the Name line
				line = iw.colorFor(na, evr) + line + "\033[0m"
			}
			fmt.Fprintln(iw.w, line)
		}
	}
}

// safeEncode escapes what libsmartcols does not print as is (util-linux's
// mbs_safe_encode): control characters, "\x" itself, and bytes that are not
// printable characters in the locale's character set, as \xNN. With an
// ASCII locale (C), that is every byte above 0x7f.
func safeEncode(s string, utf8Locale bool) string {
	var b strings.Builder
	esc := func(c byte) { fmt.Fprintf(&b, "\\x%02x", c) }
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c < 0x20 || c == 0x7f || c == '\\' && i+1 < len(s) && s[i+1] == 'x':
			esc(c)
			i++
		case c < 0x80:
			b.WriteByte(c)
			i++
		case !utf8Locale:
			esc(c)
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n <= 1 {
				esc(c)
				i++
				continue
			}
			if r < 0xa0 { // C1 control characters are not printable
				for j := range n {
					esc(s[i+j])
				}
			} else {
				b.WriteString(s[i : i+n])
			}
			i += n
		}
	}
	return b.String()
}

// colorFor is dnf5's PkgColorizer for info: packages are compared with the
// installed version of the same name and arch; equal is "reinstall", newer
// is "upgrade", anything else has no color.
func (iw *infoWriter) colorFor(na, evr string) string {
	if cur, ok := iw.installed[na]; ok {
		switch c := sack.EVRCmp(evr, cur); {
		case c == 0:
			return iw.color.reinstall
		case c > 0:
			return iw.color.upgrade
		}
	}
	return "\033[m" // what libsmartcols prints for an empty color
}

// colorEscape turns a dnf color option ("bold,blue") into escape sequences.
func colorEscape(opt, def string) string {
	if opt == "" {
		opt = def
	}
	if opt[0] < 'a' || opt[0] > 'z' { // already an escape sequence
		return opt
	}
	codes := map[string]string{
		"bold": "1", "dim": "2", "underline": "4", "blink": "5", "reverse": "7",
		"black": "30", "red": "31", "green": "32", "brown": "33", "yellow": "33",
		"blue": "34", "magenta": "35", "cyan": "36", "white": "37", "gray": "90",
	}
	var b strings.Builder
	for c := range strings.SplitSeq(opt, ",") {
		if code, ok := codes[c]; ok {
			b.WriteString("\033[" + code + "m")
		}
	}
	return b.String()
}

// chunk cuts s into pieces of at most n characters.
func chunk(s string, n int) []string {
	r := []rune(s)
	if len(r) <= n {
		return []string{s}
	}
	var out []string
	for len(r) > n {
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return append(out, string(r))
}

// formatSize is libdnf5-cli's format_size_aligned: 1024-based units, one
// decimal, the unit right-aligned to 3 characters ("8.3 MiB", "512.0   B").
func formatSize(n uint64) string {
	v := float32(n) // dnf5 computes in float
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	i := 0
	for v > 999 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %3s", v, units[i])
}

// termWidth returns the width of the terminal f writes to, 0 if it is not
// a terminal.
func termWidth(f *os.File) int {
	var ws struct{ rows, cols, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0
	}
	if ws.cols == 0 {
		return 80
	}
	return int(ws.cols)
}

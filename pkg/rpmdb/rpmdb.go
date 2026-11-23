// Package rpmdb reads the installed packages from rpm's sqlite database
// (/usr/lib/sysimage/rpm/rpmdb.sqlite) without librpm.
//
// rpm keeps one row per package in the Packages table, holding the
// package's header blob, plus index tables (Name, Providename, ...) that map
// a key to the header number (hnum) of the packages that have it.
package rpmdb

import (
	"encoding/binary"
	"errors"
	"slices"
)

// Path is where Fedora keeps the rpmdb (rpm's %_dbpath since Fedora 36).
const Path = "/usr/lib/sysimage/rpm/rpmdb.sqlite"

// DB is an open rpmdb.
type DB struct {
	sql    *sqliteDB
	tables map[string]uint32 // table name -> b-tree root page
}

// Open opens the rpmdb at path read-only.
func Open(path string) (*DB, error) {
	sql, err := openSQLite(path)
	if err != nil {
		return nil, err
	}
	tables, err := sql.tables()
	if err == nil && tables["Packages"] == 0 {
		err = errors.New("rpmdb: no Packages table")
	}
	if err != nil {
		sql.close()
		return nil, err
	}
	return &DB{sql: sql, tables: tables}, nil
}

// Close closes the database file.
func (db *DB) Close() error { return db.sql.close() }

// Find returns the header numbers of the packages with a key in the index
// table (Name, Providename, ...) for which match returns true.
func (db *DB) Find(table string, match func(key string) bool) ([]uint32, error) {
	root, ok := db.tables[table]
	if !ok {
		return nil, errors.New("rpmdb: no table " + table)
	}
	var hnums []uint32
	var rerr error
	err := db.sql.scan(root, func(_ int64, payload []byte) bool {
		cols, err := record(payload) // key, hnum, idx
		if err != nil || len(cols) < 2 {
			rerr = errSQLiteCorrupt
			return false
		}
		if match(string(cols[0].b)) && !slices.Contains(hnums, uint32(cols[1].i)) {
			hnums = append(hnums, uint32(cols[1].i))
		}
		return true
	})
	if err == nil {
		err = rerr
	}
	return hnums, err
}

// Package is an installed package, as read from its header.
type Package struct {
	Name, Version, Release, Arch string
	Epoch                        uint32 // 0 when the package has none
	Summary, Description         string
	URL, License, Vendor         string
	SourceRPM                    string
	Size                         uint64 // installed size in bytes
	Provides                     []Dep
}

// Dep is a dependency: Name, and for versioned ones Flags (rpm's
// RPMSENSE_* bits) and Version.
type Dep struct {
	Name, Version string
	Flags         uint32
}

// Equal is the flags of an "= version" dependency (RPMSENSE_EQUAL).
const Equal = 8

// Package reads the package with header number hnum.
func (db *DB) Package(hnum uint32) (*Package, error) {
	payload, err := db.sql.lookup(db.tables["Packages"], int64(hnum))
	if err != nil {
		return nil, err
	}
	cols, err := record(payload) // hnum (stored as NULL: it is the rowid), blob
	if err != nil || len(cols) < 2 {
		return nil, errSQLiteCorrupt
	}
	h, err := parseHeader(cols[1].b)
	if err != nil {
		return nil, err
	}

	p := &Package{
		Name:        h.str(tagName),
		Version:     h.str(tagVersion),
		Release:     h.str(tagRelease),
		Arch:        h.str(tagArch),
		Summary:     h.str(tagSummary),
		Description: h.str(tagDescription),
		URL:         h.str(tagURL),
		License:     h.str(tagLicense),
		Vendor:      h.str(tagVendor),
		SourceRPM:   h.str(tagSourceRPM),
	}
	if e, ok := h.num(tagEpoch); ok {
		p.Epoch = uint32(e)
	}
	// LONGSIZE replaces SIZE for packages of 4 GiB and more.
	if size, ok := h.num(tagLongSize); ok {
		p.Size = size
	} else {
		p.Size, _ = h.num(tagSize)
	}

	names, versions, flags := h.strs(tagProvideName), h.strs(tagProvideVersion), h.nums(tagProvideFlags)
	for i, name := range names {
		d := Dep{Name: name}
		if i < len(versions) && i < len(flags) {
			d.Version, d.Flags = versions[i], flags[i]
		}
		p.Provides = append(p.Provides, d)
	}
	return p, nil
}

// Header tags btd reads (rpmtag.h).
const (
	tagName           = 1000
	tagVersion        = 1001
	tagRelease        = 1002
	tagEpoch          = 1003
	tagSummary        = 1004
	tagDescription    = 1005
	tagSize           = 1009
	tagVendor         = 1011
	tagLicense        = 1014
	tagURL            = 1020
	tagArch           = 1022
	tagSourceRPM      = 1044
	tagProvideName    = 1047
	tagProvideFlags   = 1112
	tagProvideVersion = 1113
	tagLongSize       = 5009
)

// Header data types (rpmTagType).
const (
	typeInt8        = 2
	typeInt16       = 3
	typeInt32       = 4
	typeInt64       = 5
	typeString      = 6
	typeStringArray = 8
	typeI18NString  = 9
)

// header is an rpm header as stored in the rpmdb: entry count and data
// size, then 16-byte index entries (tag, type, offset, count), then the
// data they point into.
type header struct {
	index []byte
	data  []byte
}

func parseHeader(blob []byte) (header, error) {
	if len(blob) < 8 {
		return header{}, errSQLiteCorrupt
	}
	il, dl := int(binary.BigEndian.Uint32(blob)), int(binary.BigEndian.Uint32(blob[4:]))
	if il < 0 || dl < 0 || il > len(blob)/16 || 8+16*il+dl > len(blob) {
		return header{}, errSQLiteCorrupt
	}
	return header{index: blob[8 : 8+16*il], data: blob[8+16*il : 8+16*il+dl]}, nil
}

// entry finds tag: its type, where its data starts and how many values.
func (h header) entry(tag uint32) (typ uint32, off, count int, ok bool) {
	for i := 0; i+16 <= len(h.index); i += 16 {
		if binary.BigEndian.Uint32(h.index[i:]) == tag {
			typ = binary.BigEndian.Uint32(h.index[i+4:])
			off = int(int32(binary.BigEndian.Uint32(h.index[i+8:])))
			count = int(binary.BigEndian.Uint32(h.index[i+12:]))
			return typ, off, count, off >= 0 && off <= len(h.data)
		}
	}
	return 0, 0, 0, false
}

// strs returns a string, string array or i18n string tag; i18n strings
// hold one translation per header locale, the first being the original.
func (h header) strs(tag uint32) []string {
	typ, off, count, ok := h.entry(tag)
	if !ok || (typ != typeString && typ != typeStringArray && typ != typeI18NString) {
		return nil
	}
	var out []string
	for range count {
		end := slices.Index(h.data[off:], 0)
		if end < 0 {
			break
		}
		out = append(out, string(h.data[off:off+end]))
		off += end + 1
	}
	return out
}

func (h header) str(tag uint32) string {
	if s := h.strs(tag); len(s) > 0 {
		return s[0]
	}
	return ""
}

// nums returns an integer tag of any width.
func (h header) nums(tag uint32) []uint32 {
	typ, off, count, ok := h.entry(tag)
	if !ok {
		return nil
	}
	var out []uint32
	for i := range count {
		switch typ {
		case typeInt8:
			if off+i < len(h.data) {
				out = append(out, uint32(h.data[off+i]))
			}
		case typeInt16:
			if off+2*i+2 <= len(h.data) {
				out = append(out, uint32(binary.BigEndian.Uint16(h.data[off+2*i:])))
			}
		case typeInt32:
			if off+4*i+4 <= len(h.data) {
				out = append(out, binary.BigEndian.Uint32(h.data[off+4*i:]))
			}
		}
	}
	return out
}

func (h header) num(tag uint32) (uint64, bool) {
	if typ, off, _, ok := h.entry(tag); ok && typ == typeInt64 && off+8 <= len(h.data) {
		return binary.BigEndian.Uint64(h.data[off:]), true
	}
	if n := h.nums(tag); len(n) > 0 {
		return uint64(n[0]), true
	}
	return 0, false
}

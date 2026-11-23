package rpmdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// sqliteDB reads tables from an SQLite 3 file: just enough of the file
// format (https://www.sqlite.org/fileformat2.html) to walk table b-trees,
// which is all the rpmdb needs. No SQL, no indexes, no writes.
//
// Pages are read with ReadAt as needed instead of mapping the file: the
// rpmdb of a desktop is hundreds of MB, of which a query touches a few
// pages, and rpm may write to it while btd reads.
type sqliteDB struct {
	f        *os.File
	pageSize int
	usable   int // page size minus the bytes reserved at the end of each page
	cache    map[uint32][]byte
}

var errSQLiteCorrupt = errors.New("rpmdb: corrupt sqlite file")

func openSQLite(path string) (*sqliteDB, error) {
	// A non-empty write-ahead log holds changes not yet in the main file;
	// reading the main file alone would miss them.
	// ponytail: such reads fall back to dnf5; read the WAL frames if that
	// turns out to be common (rpm checkpoints it when it closes the db).
	if st, err := os.Stat(path + "-wal"); err == nil && st.Size() > 0 {
		return nil, errors.New("rpmdb: write-ahead log not empty")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, 100)
	if _, err := io.ReadFull(f, hdr); err != nil {
		f.Close()
		return nil, err
	}
	if string(hdr[:16]) != "SQLite format 3\x00" {
		f.Close()
		return nil, errors.New("rpmdb: not an sqlite file")
	}
	pageSize := int(binary.BigEndian.Uint16(hdr[16:]))
	if pageSize == 1 {
		pageSize = 65536
	}
	if pageSize < 512 || pageSize&(pageSize-1) != 0 {
		f.Close()
		return nil, errSQLiteCorrupt
	}
	return &sqliteDB{
		f:        f,
		pageSize: pageSize,
		usable:   pageSize - int(hdr[20]),
		cache:    map[uint32][]byte{},
	}, nil
}

func (d *sqliteDB) close() error { return d.f.Close() }

// page returns page n; pages are numbered from 1.
func (d *sqliteDB) page(n uint32) ([]byte, error) {
	if p, ok := d.cache[n]; ok {
		return p, nil
	}
	if n == 0 {
		return nil, errSQLiteCorrupt
	}
	p := make([]byte, d.pageSize)
	if _, err := d.f.ReadAt(p, int64(n-1)*int64(d.pageSize)); err != nil {
		return nil, fmt.Errorf("rpmdb: reading page %d: %w", n, err)
	}
	d.cache[n] = p
	return p, nil
}

// btreePage is a parsed b-tree page header.
type btreePage struct {
	data  []byte
	kind  byte   // 0x05 interior table, 0x0d leaf table (index pages are not used here)
	cells []int  // offsets of the cells in data
	right uint32 // interior pages: the right-most child
}

func (d *sqliteDB) btree(n uint32) (btreePage, error) {
	data, err := d.page(n)
	if err != nil {
		return btreePage{}, err
	}
	h := 0
	if n == 1 { // page 1 starts with the 100-byte file header
		h = 100
	}
	if h+8 > len(data) {
		return btreePage{}, errSQLiteCorrupt
	}
	p := btreePage{data: data, kind: data[h]}
	ncells := int(binary.BigEndian.Uint16(data[h+3:]))
	ptrs := h + 8
	switch p.kind {
	case 0x0d:
	case 0x05:
		p.right = binary.BigEndian.Uint32(data[h+8:])
		ptrs = h + 12
	default:
		return btreePage{}, fmt.Errorf("rpmdb: unexpected page type %#x", p.kind)
	}
	if ptrs+2*ncells > len(data) {
		return btreePage{}, errSQLiteCorrupt
	}
	p.cells = make([]int, ncells)
	for i := range p.cells {
		p.cells[i] = int(binary.BigEndian.Uint16(data[ptrs+2*i:]))
		if p.cells[i] >= len(data) {
			return btreePage{}, errSQLiteCorrupt
		}
	}
	return p, nil
}

// scan calls fn with each row of the table whose b-tree starts at page
// root, in rowid order, until fn returns false.
func (d *sqliteDB) scan(root uint32, fn func(rowid int64, payload []byte) bool) error {
	_, err := d.walk(root, fn, 0)
	return err
}

func (d *sqliteDB) walk(n uint32, fn func(int64, []byte) bool, depth int) (bool, error) {
	if depth > 64 { // a loop in a corrupt file
		return false, errSQLiteCorrupt
	}
	p, err := d.btree(n)
	if err != nil {
		return false, err
	}
	if p.kind == 0x05 {
		// Interior cell: 4-byte left child, then a rowid key we don't need.
		for _, c := range p.cells {
			if c+4 > len(p.data) {
				return false, errSQLiteCorrupt
			}
			if more, err := d.walk(binary.BigEndian.Uint32(p.data[c:]), fn, depth+1); !more || err != nil {
				return more, err
			}
		}
		return d.walk(p.right, fn, depth+1)
	}
	for _, c := range p.cells {
		rowid, payload, err := d.leafCell(p.data, c)
		if err != nil {
			return false, err
		}
		if !fn(rowid, payload) {
			return false, nil
		}
	}
	return true, nil
}

// lookup returns the payload of row rowid of the table at root.
func (d *sqliteDB) lookup(root uint32, rowid int64) ([]byte, error) {
	n := root
	for range 64 { // bounded: a loop in a corrupt file must not hang btd
		p, err := d.btree(n)
		if err != nil {
			return nil, err
		}
		if p.kind == 0x0d {
			for _, c := range p.cells {
				id, payload, err := d.leafCell(p.data, c)
				if err != nil {
					return nil, err
				}
				if id == rowid {
					return payload, nil
				}
			}
			return nil, fmt.Errorf("rpmdb: row %d not found", rowid)
		}
		// The left child of the first cell whose key is >= rowid holds it;
		// past every key, the right-most child does.
		n = p.right
		for _, c := range p.cells {
			key, _ := varint(p.data[min(c+4, len(p.data)):])
			if rowid <= key {
				n = binary.BigEndian.Uint32(p.data[c:])
				break
			}
		}
	}
	return nil, errSQLiteCorrupt
}

// leafCell parses a table leaf cell: payload size, rowid, the local part of
// the payload, then, for big payloads, the first overflow page.
func (d *sqliteDB) leafCell(page []byte, c int) (rowid int64, payload []byte, err error) {
	size, n := varint(page[c:])
	c += n
	rowid, n = varint(page[c:])
	c += n
	if size < 0 || n == 0 {
		return 0, nil, errSQLiteCorrupt
	}

	// How much of the payload is on this page (fileformat2 §1.6).
	u := d.usable
	maxLocal := u - 35
	local := int(size)
	if local > maxLocal {
		minLocal := (u-12)*32/255 - 23
		local = minLocal + (int(size)-minLocal)%(u-4)
		if local > maxLocal {
			local = minLocal
		}
	}
	if c+local > len(page) {
		return 0, nil, errSQLiteCorrupt
	}
	if local == int(size) {
		return rowid, page[c : c+local], nil
	}

	// The rest is in a chain of overflow pages: 4-byte next page, then data.
	payload = make([]byte, 0, size)
	payload = append(payload, page[c:c+local]...)
	if c+local+4 > len(page) {
		return 0, nil, errSQLiteCorrupt
	}
	next := binary.BigEndian.Uint32(page[c+local:])
	for len(payload) < int(size) {
		ovf, err := d.readOverflow(next)
		if err != nil {
			return 0, nil, err
		}
		next = binary.BigEndian.Uint32(ovf)
		chunk := ovf[4:u]
		if rest := int(size) - len(payload); len(chunk) > rest {
			chunk = chunk[:rest]
		}
		payload = append(payload, chunk...)
	}
	return rowid, payload, nil
}

// readOverflow reads an overflow page without caching it: a package header
// spans hundreds of them and none is read twice.
func (d *sqliteDB) readOverflow(n uint32) ([]byte, error) {
	if n == 0 {
		return nil, errSQLiteCorrupt
	}
	p := make([]byte, d.pageSize)
	if _, err := d.f.ReadAt(p, int64(n-1)*int64(d.pageSize)); err != nil {
		return nil, fmt.Errorf("rpmdb: reading page %d: %w", n, err)
	}
	return p, nil
}

// varint decodes an SQLite varint: big-endian, 7 bits per byte with the
// high bit set on all but the last, except a 9th byte keeps all 8 bits.
// It returns the number of bytes read, 0 if b ends first.
func varint(b []byte) (v int64, n int) {
	var x uint64
	for i := 0; i < 9 && i < len(b); i++ {
		if i == 8 {
			return int64(x<<8 | uint64(b[i])), 9
		}
		x = x<<7 | uint64(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			return int64(x), i + 1
		}
	}
	return 0, 0
}

// value is a column of a record: an integer, or the bytes of a text or
// blob; NULLs are zero values.
type value struct {
	i int64
	b []byte
}

// record decodes an SQLite record (fileformat2 §2.1): a header of serial
// types, one per column, then the column values.
func record(payload []byte) ([]value, error) {
	hsize, n := varint(payload)
	if n == 0 || hsize < int64(n) || hsize > int64(len(payload)) {
		return nil, errSQLiteCorrupt
	}
	var cols []value
	body := int(hsize)
	for h := n; h < int(hsize); {
		st, n := varint(payload[h:])
		if n == 0 {
			return nil, errSQLiteCorrupt
		}
		h += n
		var v value
		size := 0
		switch {
		case st == 0, st == 8, st == 9: // NULL, 0, 1
			v.i = max(st-8, 0)
		case st >= 1 && st <= 6: // big-endian signed integers of 1-8 bytes
			size = [...]int{0, 1, 2, 3, 4, 6, 8}[st]
		case st == 7: // float, not used by rpm
			size = 8
		case st >= 12: // blob (even) or text (odd)
			size = int((st - 12) / 2)
		default:
			return nil, errSQLiteCorrupt
		}
		if body+size > len(payload) {
			return nil, errSQLiteCorrupt
		}
		raw := payload[body : body+size]
		body += size
		switch {
		case st >= 1 && st <= 6:
			x := int64(int8(raw[0])) // sign-extend the first byte
			for _, c := range raw[1:] {
				x = x<<8 | int64(c)
			}
			v.i = x
		case st >= 12:
			v.b = raw
		}
		cols = append(cols, v)
	}
	return cols, nil
}

// tables returns the root page of every table, from the sqlite_schema
// table at page 1 (columns: type, name, tbl_name, rootpage, sql).
func (d *sqliteDB) tables() (map[string]uint32, error) {
	roots := map[string]uint32{}
	var rerr error
	err := d.scan(1, func(_ int64, payload []byte) bool {
		cols, err := record(payload)
		if err != nil {
			rerr = err
			return false
		}
		if len(cols) >= 4 && string(cols[0].b) == "table" {
			roots[string(cols[1].b)] = uint32(cols[3].i)
		}
		return true
	})
	if err == nil {
		err = rerr
	}
	return roots, err
}

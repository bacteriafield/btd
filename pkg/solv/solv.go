// Package solv reads the .solv files that libsolv writes and dnf5 keeps as
// its metadata cache (/var/cache/libdnf5/<repo>-<hash>/solv/<repo>.solv).
//
// Loading those files is most of what dnf5 does before it can answer any
// query. This is a read-only port of libsolv's repo_add_solv
// (src/repo_solv.c, libsolv 0.7.x, file versions 8 and 9) that skips what
// btd does not need: no pool, no hashing of every string and dependency,
// no solver data. Name, arch, evr and vendor are decoded for every package
// while the file is read; anything else (summary, description, sizes,
// provides...) is decoded on demand for the packages a query matched.
//
// File layout, in order (all counts are big-endian u32):
//
//	"SOLV" version numid numrel numdir numsolv numkeys numschemata flags
//	[userdata]           dnf5 stores the repomd.xml checksum here
//	string pool          prefix-compressed, NUL-separated
//	relations            name/evr/flags triples: "foo >= 1.0"
//	dirs                 directory tree of the file lists
//	keys                 (name, type, size, storage) of every attribute
//	schemata             lists of keys: which attributes an entry has
//	incore data          main schema, then one record per package
//	vertical pages       big strings (descriptions...), compressed pages
package solv

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"
)

// Id indexes the file's string pool; an Id at or above the number of
// strings is a relation ("name op evr", see File.DepName), as in libsolv.
type Id uint32

// Solvable is one package of the repository.
type Solvable struct {
	Name, Arch, EVR, Vendor Id

	schema uint32 // which keys the package has, in storage order
	off    uint32 // where its key data starts in File.incore
}

// File is a parsed .solv file. Nothing changes after Parse, so its methods
// are safe for concurrent use.
type File struct {
	Userdata  []byte
	Solvables []Solvable

	data    []byte   // the whole file
	strs    []byte   // expanded string pool, strings separated by NUL
	strOff  []uint32 // strOff[id] is where string id starts in strs
	numid   uint32   // number of strings; Ids from here on are relations
	rels    []rel
	keys    []key
	schemas [][]uint32 // schema id -> key indexes
	incore  []byte     // the incore data blob (a slice of data)

	// Vertical data: big values stored apart from the incore data, in
	// 32 KiB pages that may be compressed. vertOff[key] is where the key's
	// data starts in the concatenation of all pages.
	vertOff []uint32
	pages   []page
}

// rel is a dependency with a version, "name op evr" (e.g. "foo >= 1.0"),
// minus the evr, which btd never needs. flags is libsolv's: 1 greater,
// 2 equal, 4 less; 16 and up are rich dependency operators, where name is
// itself a dependency.
type rel struct {
	name  Id
	flags uint8
}

// key is an attribute: a name ("solvable:summary"), how its values are
// encoded (type) and where they live (storage).
type key struct {
	name    string
	typ     keyType
	size    uint32 // CONSTANT value, CONSTANTID id, or vertical data size
	storage uint32
	role    uint8 // which Solvable field it fills while parsing, if any
}

// Key roles: the attributes decoded for every package while parsing; 0 is
// none.
const (
	roleName uint8 = iota + 1
	roleArch
	roleEVR
	roleVendor
)

var roles = map[string]uint8{
	"solvable:name": roleName, "solvable:arch": roleArch,
	"solvable:evr": roleEVR, "solvable:vendor": roleVendor,
}

type keyType uint8

const (
	typeVoid keyType = iota
	typeConstant
	typeConstantID
	typeID
	typeNum
	typeDir
	typeStr
	typeBinary
	typeIDArray
	typeRelIDArray
	typeDirStrArray
	typeDirNumNumArray
	typeFixArray
	typeFlexArray
	typeMD5
	typeSHA1
	typeSHA224
	typeSHA256
	typeSHA384
	typeSHA512
	typeDeleted
)

// keyTypes maps libsolv's type names (knownid.h), which the file stores as
// strings, to keyType.
var keyTypes = map[string]keyType{
	"repokey:type:void":           typeVoid,
	"repokey:type:constant":       typeConstant,
	"repokey:type:constantid":     typeConstantID,
	"repokey:type:id":             typeID,
	"repokey:type:num":            typeNum,
	"repokey:type:dir":            typeDir,
	"repokey:type:str":            typeStr,
	"repokey:type:binary":         typeBinary,
	"repokey:type:idarray":        typeIDArray,
	"repokey:type:relidarray":     typeRelIDArray,
	"repokey:type:dirstrarray":    typeDirStrArray,
	"repokey:type:dirnumnumarray": typeDirNumNumArray,
	"repokey:type:fixarray":       typeFixArray,
	"repokey:type:flexarray":      typeFlexArray,
	"repokey:type:md5":            typeMD5,
	"repokey:type:sha1":           typeSHA1,
	"repokey:type:sha224":         typeSHA224,
	"repokey:type:sha256":         typeSHA256,
	"repokey:type:sha384":         typeSHA384,
	"repokey:type:sha512":         typeSHA512,
	"repokey:type:deleted":        typeDeleted,
}

// checksumSize is the byte size of the checksum types.
var checksumSize = map[keyType]int{
	typeMD5: 16, typeSHA1: 20, typeSHA224: 28, typeSHA256: 32, typeSHA384: 48, typeSHA512: 64,
}

// Key storage kinds (libsolv's KEY_STORAGE_*). The fourth one, a separate
// id array block, is never written by libsolv 0.7 and is rejected.
const (
	storageSolvable = 1 // old files: same as incore
	storageIncore   = 2 // in the incore data
	storageVertical = 3 // incore holds (offset, length) into the pages
)

// Header flags (libsolv's SOLV_FLAG_*).
const (
	flagPrefixPool   = 4
	flagSizeBytes    = 8
	flagUserdata     = 16
	flagIDArrayBlock = 32
)

const pageSize = 1 << 15 // REPOPAGE_BLOBSIZE

// page is one vertical data page: where it is in the file and whether it
// is compressed.
type page struct {
	off, size  uint32
	compressed bool
}

var errCorrupt = errors.New("solv: corrupt file")

// Parse reads a .solv file held in data, which must not change afterwards:
// strings returned by File point into it.
func Parse(data []byte) (*File, error) {
	f := &File{data: data}
	r := reader{buf: data}

	if r.u32() != 'S'<<24|'O'<<16|'L'<<8|'V' {
		return nil, errors.New("solv: not a solv file")
	}
	if v := r.u32(); v != 8 && v != 9 {
		return nil, fmt.Errorf("solv: unsupported version %d", v)
	}
	numid, numrel, numdir := r.u32(), r.u32(), r.u32()
	numsolv, numkeys, numschemata := r.u32(), r.u32(), r.u32()
	flags := r.u32()
	if numid >= 1<<29 || numrel >= 1<<29 || numdir >= 1<<29 || numsolv >= 1<<29 ||
		numkeys >= 1<<29 || numschemata >= 1<<29 {
		return nil, errCorrupt
	}
	// libsolv's writer always prefixes the strings and stores sizes in bytes,
	// and never writes the idarray block (USE_IDARRAYBLOCK is undefined in
	// repo_write.c). ponytail: other files make callers fall back to dnf5.
	if flags&flagIDArrayBlock != 0 || flags&flagSizeBytes == 0 || flags&flagPrefixPool == 0 {
		return nil, errors.New("solv: unsupported file flags")
	}
	f.numid = numid

	if flags&flagUserdata != 0 {
		n := r.u32()
		f.Userdata = r.bytes(int(n))
	}

	if err := f.readStrings(&r); err != nil {
		return nil, err
	}

	// Relations: name, evr and flags; name and evr may point to strings or
	// to earlier relations (rich dependencies nest).
	f.rels = make([]rel, numrel)
	for i := range f.rels {
		name, evr := r.id(), r.id()
		if name >= numid+uint32(i) || evr >= numid+uint32(i) {
			return nil, errCorrupt
		}
		f.rels[i] = rel{name: Id(name), flags: r.u8()}
	}

	// Dirs are only used by file lists, which btd does not read; parse
	// them to get past them. Entries 0 and 1 (root and "/") are implicit.
	for i := uint32(2); i < numdir; i++ {
		if r.id() >= numid { // a parent marker, followed by the component
			i++
			r.id()
		}
	}

	f.keys = make([]key, numkeys)
	for i := 1; i < int(numkeys); i++ {
		name, typ := r.id(), r.id()
		k := &f.keys[i]
		k.name = f.Str(Id(name))
		t, ok := keyTypes[f.Str(Id(typ))]
		if !ok {
			return nil, fmt.Errorf("solv: unknown key type %q", f.Str(Id(typ)))
		}
		k.typ = t
		k.size = r.id()
		k.storage = r.id()
		k.role = roles[k.name]
		if k.storage == storageSolvable {
			k.storage = storageIncore
		}
		if k.storage != storageIncore && k.storage != storageVertical {
			return nil, fmt.Errorf("solv: unsupported key storage %d", k.storage)
		}
	}

	// Schemata: each is an id array of key indexes; schema 0 is empty.
	r.id() // total size of all schemata, only needed to preallocate in C
	f.schemas = make([][]uint32, numschemata)
	for i := 1; i < int(numschemata); i++ {
		s := r.idArray()
		for _, k := range s {
			if k >= numkeys {
				return nil, errCorrupt
			}
		}
		f.schemas[i] = s
	}

	r.id() // maxsize: the largest record, a buffer size hint for C
	allsize := r.id()
	f.incore = r.bytes(int(allsize))

	if err := f.readPages(&r); err != nil {
		return nil, err
	}
	if r.err {
		return nil, errCorrupt
	}
	if err := f.readIncore(numsolv); err != nil {
		return nil, err
	}
	return f, nil
}

// readStrings expands the string pool. Each string is stored as the number
// of bytes it shares with the previous string plus the rest, NUL-terminated;
// strings are sorted, so this saves a lot.
func (f *File) readStrings(r *reader) error {
	sizeid := r.u32()
	if sizeid >= 0xf0000000 {
		return errCorrupt
	}
	packed := r.bytes(int(r.u32()))
	if r.err {
		return errCorrupt
	}
	f.strOff = make([]uint32, f.numid+1)
	f.strs = make([]byte, sizeid)
	dest, prev, p := 0, 0, 0
	for i := uint32(1); i < f.numid; i++ {
		if p >= len(packed) {
			return errCorrupt
		}
		same := int(packed[p])
		p++
		end := bytes.IndexByte(packed[p:], 0)
		if end < 0 || dest+same+end+1 > len(f.strs) || same > dest-prev {
			return errCorrupt
		}
		copy(f.strs[dest:], f.strs[prev:prev+same])
		copy(f.strs[dest+same:], packed[p:p+end+1])
		p += end + 1
		f.strOff[i] = uint32(dest)
		prev = dest
		dest += same + end + 1
	}
	if dest != len(f.strs) {
		return errCorrupt
	}
	f.strOff[f.numid] = uint32(len(f.strs))
	return nil
}

// readPages records where the vertical pages are; they are decompressed
// only when a vertical value is read.
func (f *File) readPages(r *reader) error {
	f.vertOff = make([]uint32, len(f.keys))
	total := uint32(0)
	for i := 1; i < len(f.keys); i++ {
		if f.keys[i].storage == storageVertical {
			f.vertOff[i] = total
			total += f.keys[i].size
		}
	}
	if total == 0 {
		return nil
	}
	if r.u32() != pageSize {
		return errors.New("solv: unsupported page size")
	}
	f.pages = make([]page, (total+pageSize-1)/pageSize)
	for i := range f.pages {
		n := r.u32()
		size := n >> 1
		if size > pageSize {
			return errCorrupt
		}
		f.pages[i] = page{off: uint32(r.pos), size: size, compressed: n&1 != 0}
		r.bytes(int(size))
	}
	return nil
}

// readIncore walks the incore data once: the main schema's keys, and in
// them the package array (repository:solvables), recording where each
// package's data starts and decoding its name, arch, evr and vendor.
func (f *File) readIncore(numsolv uint32) error {
	r := reader{buf: f.incore}
	main := r.id()
	if main >= uint32(len(f.schemas)) {
		return errCorrupt
	}
	for _, k := range f.schemas[main] {
		key := &f.keys[k]
		if key.name != "repository:solvables" || key.typ != typeFlexArray {
			f.skip(&r, k)
			continue
		}
		n := r.id()
		if n != numsolv {
			return errCorrupt
		}
		f.Solvables = make([]Solvable, n)
		for i := range f.Solvables {
			s := &f.Solvables[i]
			s.schema = r.id()
			if s.schema >= uint32(len(f.schemas)) {
				return errCorrupt
			}
			s.off = uint32(r.pos)
			for _, k := range f.schemas[s.schema] {
				key := &f.keys[k]
				var dst *Id
				switch key.role {
				case roleName:
					dst = &s.Name
				case roleArch:
					dst = &s.Arch
				case roleEVR:
					dst = &s.EVR
				case roleVendor:
					dst = &s.Vendor
				}
				switch {
				case dst != nil && key.typ == typeID:
					*dst = Id(r.id())
				case dst != nil && key.typ == typeConstantID:
					*dst = Id(key.size)
				default:
					f.skip(&r, k)
				}
			}
			if r.err {
				return errCorrupt
			}
		}
	}
	if r.err {
		return errCorrupt
	}
	return nil
}

// skip moves r past the value of key k (libsolv's data_skip).
func (f *File) skip(r *reader, k uint32) {
	key := &f.keys[k]
	if key.storage == storageVertical { // just (offset, length) here
		r.id()
		r.id()
		return
	}
	switch key.typ {
	case typeVoid, typeConstant, typeConstantID, typeDeleted:
	case typeID, typeNum, typeDir:
		r.skipVarint()
	case typeStr:
		r.skipStr()
	case typeBinary:
		r.bytes(int(r.id()))
	case typeIDArray, typeRelIDArray:
		// Elements end with a byte without 0x80; the last one also lacks 0x40.
		for r.pos < len(r.buf) && r.buf[r.pos]&0xc0 != 0 {
			r.pos++
		}
		r.pos++
	case typeDirStrArray: // (dir, string) pairs, eof flag on the dir
		for {
			_, eof := r.idEOF()
			r.skipStr()
			if eof || r.err {
				return
			}
		}
	case typeDirNumNumArray: // (dir, num, num) triples, eof flag on the last num
		for {
			r.id()
			r.id()
			if _, eof := r.idEOF(); eof || r.err {
				return
			}
		}
	case typeFixArray, typeFlexArray:
		// n entries; a fixarray has one schema for all of them, a flexarray
		// one per entry.
		n := r.id()
		if n == 0 {
			return
		}
		schema := r.id()
		for e := uint32(0); e < n && !r.err; e++ {
			if e > 0 && key.typ == typeFlexArray {
				schema = r.id()
			}
			if schema >= uint32(len(f.schemas)) {
				r.err = true
				return
			}
			for _, sk := range f.schemas[schema] {
				f.skip(r, sk)
			}
		}
	default:
		if n, ok := checksumSize[key.typ]; ok {
			r.bytes(n)
		} else {
			r.err = true
		}
	}
}

// Str returns string id. The string points into the file's data.
func (f *File) Str(id Id) string {
	if id == 0 || uint32(id) >= f.numid {
		return ""
	}
	start, end := f.strOff[id], f.strOff[id+1]-1 // minus the NUL
	if start >= end {
		return ""
	}
	return unsafe.String(&f.strs[start], end-start)
}

// DepName returns the name a dependency id is about: the string itself, or
// the name of a "name op evr" relation. Rich dependencies return "".
func (f *File) DepName(id Id) string {
	if uint32(id) >= f.numid {
		i := uint32(id) - f.numid
		if i >= uint32(len(f.rels)) || f.rels[i].flags > 7 {
			return ""
		}
		id = f.rels[i].name
	}
	return f.Str(id)
}

// find walks package s's data up to the key called name and returns a
// reader positioned at its value and the key's index, or false if s has no
// such key.
func (f *File) find(s *Solvable, name string) (reader, uint32, bool) {
	r := reader{buf: f.incore, pos: int(s.off)}
	for _, k := range f.schemas[s.schema] {
		if f.keys[k].name == name {
			return r, k, !r.err
		}
		f.skip(&r, k)
	}
	return r, 0, false
}

// LookupStr returns the string attribute name (e.g. "solvable:summary") of
// package s: a plain or vertical string, or a pool string Id.
func (f *File) LookupStr(s *Solvable, name string) string {
	r, k, ok := f.find(s, name)
	if !ok {
		return ""
	}
	key := &f.keys[k]
	if key.storage == storageVertical {
		return f.vertical(k, &r)
	}
	switch key.typ {
	case typeStr:
		return r.str()
	case typeID:
		return f.Str(Id(r.id()))
	case typeConstantID:
		return f.Str(Id(key.size))
	}
	return ""
}

// LookupNum returns the number attribute name of package s, 0 if missing.
func (f *File) LookupNum(s *Solvable, name string) uint64 {
	r, k, ok := f.find(s, name)
	if !ok {
		return 0
	}
	key := &f.keys[k]
	switch key.typ {
	case typeNum:
		return r.num()
	case typeConstant:
		return uint64(key.size)
	}
	return 0
}

// LookupVoid reports whether package s has the attribute name with a void
// value, which libsolv uses as a flag ("sourcename is the same as name").
func (f *File) LookupVoid(s *Solvable, name string) bool {
	_, k, ok := f.find(s, name)
	return ok && f.keys[k].typ == typeVoid
}

// LookupDeps returns a dependency attribute ("solvable:provides") of
// package s, without the markers libsolv inserts in some of them.
func (f *File) LookupDeps(s *Solvable, name string) []Id {
	r, k, ok := f.find(s, name)
	if !ok {
		return nil
	}
	var deps []Id
	switch f.keys[k].typ {
	case typeIDArray:
		for !r.err {
			id, eof := r.idEOF()
			deps = append(deps, Id(id))
			if eof {
				break
			}
		}
	case typeRelIDArray:
		// Sorted ids stored as deltas (+1); a 0 is a marker that restarts
		// the deltas (e.g. between provides and file provides).
		old := uint32(0)
		for !r.err {
			x, eof := r.idEOF()
			if x == 0 {
				old = 0
			} else {
				old += x - 1
				deps = append(deps, Id(old))
			}
			if eof {
				break
			}
		}
	}
	if r.err {
		return nil
	}
	return deps
}

// vertical reads a vertical string: its (offset, length) are in the incore
// data, the bytes in the pages after vertOff of its key.
func (f *File) vertical(k uint32, r *reader) string {
	off, n := r.id(), r.id()
	if r.err || n == 0 || off+n > f.keys[k].size {
		return ""
	}
	start := f.vertOff[k] + off
	var out []byte
	for p := start / pageSize; p <= (start+n-1)/pageSize; p++ {
		data, err := f.page(p)
		if err != nil {
			return ""
		}
		out = append(out, data...)
	}
	b := out[start%pageSize:]
	if uint32(len(b)) < n {
		return ""
	}
	b = b[:n]
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// page returns vertical page p, decompressed.
func (f *File) page(p uint32) ([]byte, error) {
	if p >= uint32(len(f.pages)) {
		return nil, errCorrupt
	}
	pg := f.pages[p]
	raw := f.data[pg.off : pg.off+pg.size]
	if !pg.compressed {
		return raw, nil
	}
	out := make([]byte, pageSize)
	n, err := decompress(raw, out)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// decompress undoes libsolv's page compression (repopage.c), an LZ77
// variant: literal runs and back references of a few encodings, picked by
// the high bits of each command byte. Unlike the C version, every read and
// write is bounds checked (CVE-2026-48864 was this function trusting its
// input).
func decompress(in, out []byte) (int, error) {
	o := 0
	for i := 0; i < len(in); {
		first := int(in[i])
		i++
		var off, n int
		need := func(k int) bool { return i+k <= len(in) }
		switch first >> 4 {
		case 0, 1, 2, 3, 4, 5, 6, 7: // 0LLLLLLL: one literal byte
			if o >= len(out) {
				return 0, errCorrupt
			}
			out[o] = byte(first)
			o++
			continue
		case 8, 9: // 100lllll: l+1 literal bytes follow
			l := first&31 + 1
			if !need(l) || o+l > len(out) {
				return 0, errCorrupt
			}
			copy(out[o:], in[i:i+l])
			i += l
			o += l
			continue
		case 10, 11: // 101oolll <8o>: short reference
			if !need(1) {
				return 0, errCorrupt
			}
			off = (first&0x18)<<5 | int(in[i])
			i++
			n = first&7 + 2
		case 12, 13: // 110lllll <8o>
			if !need(1) {
				return 0, errCorrupt
			}
			off = int(in[i])
			i++
			n = first&31 + 10
		case 14: // 1110llll <8o> <8o>
			if !need(2) {
				return 0, errCorrupt
			}
			off = int(in[i]) | int(in[i+1])<<8
			i += 2
			n = first&15 + 3
		case 15: // 11110lll <8l> <8o> <8o> or 11111lll <8l> <8o> <8o> <8o>
			if f := first & 15; f >= 8 {
				if !need(4) {
					return 0, errCorrupt
				}
				n = ((f-8)<<8 | int(in[i])) + 5
				off = int(in[i+1]) | int(in[i+2])<<8 | int(in[i+3])<<16
				i += 4
			} else {
				if !need(3) {
					return 0, errCorrupt
				}
				n = (f<<8 | int(in[i])) + 19
				off = int(in[i+1]) | int(in[i+2])<<8
				i += 3
			}
		}
		src := o - (off + 1)
		if src < 0 || o+n > len(out) {
			return 0, errCorrupt
		}
		for j := range n { // byte by byte: source and destination may overlap
			out[o+j] = out[src+j]
		}
		o += n
	}
	return o, nil
}

// reader decodes libsolv's encodings from buf. Errors are sticky: after
// running out of data every read returns zero and err stays set.
type reader struct {
	buf []byte
	pos int
	err bool
}

func (r *reader) u8() uint8 {
	if r.pos >= len(r.buf) {
		r.err = true
		return 0
	}
	r.pos++
	return r.buf[r.pos-1]
}

func (r *reader) u32() uint32 {
	if r.pos+4 > len(r.buf) {
		r.err = true
		return 0
	}
	r.pos += 4
	return binary.BigEndian.Uint32(r.buf[r.pos-4:])
}

func (r *reader) bytes(n int) []byte {
	if n < 0 || r.pos+n > len(r.buf) {
		r.err = true
		return nil
	}
	r.pos += n
	return r.buf[r.pos-n : r.pos]
}

// id reads a varint: 7 bits per byte, most significant first, 0x80 set on
// every byte but the last.
func (r *reader) id() uint32 {
	return uint32(r.num())
}

func (r *reader) num() uint64 {
	var x uint64
	for r.pos < len(r.buf) {
		c := r.buf[r.pos]
		r.pos++
		if c&0x80 == 0 {
			return x<<7 | uint64(c)
		}
		x = x<<7 | uint64(c&0x7f)
	}
	r.err = true
	return 0
}

func (r *reader) skipVarint() {
	for r.pos < len(r.buf) && r.buf[r.pos]&0x80 != 0 {
		r.pos++
	}
	r.pos++
	if r.pos > len(r.buf) {
		r.err = true
	}
}

// idEOF reads an id array element: like id, but the last byte keeps 6
// bits, and its 0x40 bit tells whether more elements follow.
func (r *reader) idEOF() (x uint32, eof bool) {
	for r.pos < len(r.buf) {
		c := uint32(r.buf[r.pos])
		r.pos++
		if c&0x80 == 0 {
			return x<<6 | c&0x3f, c&0x40 == 0
		}
		x = x<<7 | c&0x7f
	}
	r.err = true
	return 0, true
}

// idArray reads a whole id array; a lone 0 is the empty array.
func (r *reader) idArray() []uint32 {
	var ids []uint32
	for !r.err {
		x, eof := r.idEOF()
		if eof && x == 0 && len(ids) == 0 {
			return nil
		}
		ids = append(ids, x)
		if eof {
			break
		}
	}
	return ids
}

func (r *reader) skipStr() {
	end := bytes.IndexByte(r.buf[min(r.pos, len(r.buf)):], 0)
	if end < 0 {
		r.err = true
		r.pos = len(r.buf)
		return
	}
	r.pos += end + 1
}

func (r *reader) str() string {
	end := bytes.IndexByte(r.buf[min(r.pos, len(r.buf)):], 0)
	if end < 0 {
		r.err = true
		r.pos = len(r.buf)
		return ""
	}
	s := string(r.buf[r.pos : r.pos+end])
	r.pos += end + 1
	return s
}

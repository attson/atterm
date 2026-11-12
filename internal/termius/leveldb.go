package termius

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/golang/snappy"
)

const maxData = 256 << 20
const maxValue = 32 << 20

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

type cursor struct {
	b []byte
	p int
}

func (c *cursor) take(n int) ([]byte, error) {
	if n < 0 || n > len(c.b)-c.p {
		return nil, errors.New("truncated database record")
	}
	b := c.b[c.p : c.p+n]
	c.p += n
	return b, nil
}
func (c *cursor) num() (uint64, error) {
	v, n := binary.Uvarint(c.b[c.p:])
	if n <= 0 {
		return 0, errors.New("invalid database integer")
	}
	c.p += n
	return v, nil
}
func (c *cursor) slice() ([]byte, error) {
	n, e := c.num()
	if e != nil {
		return nil, e
	}
	if n > maxValue {
		return nil, errors.New("database value exceeds limit")
	}
	return c.take(int(n))
}
func checksum(b []byte) uint32 {
	x := crc32.Checksum(b, castagnoli)
	return (x>>15 | x<<17) + 0xa282ead8
}
func decompress(b []byte) ([]byte, error) {
	n, e := snappy.DecodedLen(b)
	if e != nil {
		return nil, errors.New("invalid Snappy block")
	}
	if n > maxValue {
		return nil, errors.New("expanded database block exceeds limit")
	}
	return snappy.Decode(nil, b)
}

// physicalRecords validates WAL checksums before exposing complete logical records.
func physicalRecords(b []byte) ([][]byte, error) {
	var out [][]byte
	var fragment []byte
	assembling := false
	for p := 0; p < len(b); {
		remain := 32768 - p%32768
		if remain < 7 {
			p += min(remain, len(b)-p)
			continue
		}
		if len(b)-p < 7 {
			return nil, errors.New("incomplete database log; close Termius and retry")
		}
		h := b[p : p+7]
		n := int(binary.LittleEndian.Uint16(h[4:6]))
		kind := h[6]
		if bytes.Equal(h, make([]byte, 7)) {
			p += min(remain, len(b)-p)
			continue
		}
		if n > remain-7 || n > len(b)-p-7 {
			return nil, errors.New("incomplete database log; close Termius and retry")
		}
		payload := b[p+7 : p+7+n]
		checked := append([]byte{kind}, payload...)
		if checksum(checked) != binary.LittleEndian.Uint32(h[:4]) {
			return nil, errors.New("database log checksum mismatch")
		}
		switch kind {
		case 1:
			if assembling {
				return nil, errors.New("invalid log fragment sequence")
			}
			out = append(out, payload)
		case 2:
			if assembling {
				return nil, errors.New("invalid log fragment sequence")
			}
			fragment = append([]byte(nil), payload...)
			assembling = true
		case 3, 4:
			if !assembling || len(fragment)+n > maxData {
				return nil, errors.New("invalid log fragment")
			}
			fragment = append(fragment, payload...)
			if kind == 4 {
				out = append(out, fragment)
				fragment = nil
				assembling = false
			}
		default:
			return nil, errors.New("unknown database log record")
		}
		p += 7 + n
	}
	if assembling {
		return nil, errors.New("incomplete database log; close Termius and retry")
	}
	return out, nil
}

type dbRecord struct {
	key, value []byte
	seq        uint64
	deleted    bool
}
type dbView map[string]dbRecord

func (v dbView) put(k, b []byte, seq uint64, kind byte) error {
	if kind > 1 {
		return errors.New("unknown LevelDB value type")
	}
	if old, ok := v[string(k)]; ok && old.seq >= seq {
		return nil
	}
	if len(v) >= 1000000 {
		return errors.New("too many database records")
	}
	v[string(k)] = dbRecord{key: k, value: b, seq: seq, deleted: kind == 0}
	return nil
}

type manifest struct {
	tables    map[uint64]bool
	log, prev uint64
}

func parseManifest(b []byte) (manifest, error) {
	m := manifest{tables: map[uint64]bool{}}
	records, e := physicalRecords(b)
	if e != nil {
		return m, e
	}
	for _, r := range records {
		c := cursor{b: r}
		for c.p < len(c.b) {
			tag, e := c.num()
			if e != nil {
				return m, e
			}
			switch tag {
			case 1:
				_, e = c.slice()
			case 2:
				m.log, e = c.num()
			case 9:
				m.prev, e = c.num()
			case 3, 4:
				_, e = c.num()
			case 5:
				if _, e = c.num(); e == nil {
					_, e = c.slice()
				}
			case 6, 7:
				if _, e = c.num(); e != nil {
					return m, e
				}
				var number uint64
				number, e = c.num()
				if e != nil {
					return m, e
				}
				if tag == 6 {
					delete(m.tables, number)
				} else {
					if _, e = c.num(); e == nil {
						_, e = c.slice()
					}
					if e == nil {
						_, e = c.slice()
					}
					m.tables[number] = true
				}
			default:
				return m, errors.New("unsupported LevelDB manifest version")
			}
			if e != nil {
				return m, e
			}
		}
	}
	return m, nil
}

type blockHandle struct{ offset, size uint64 }

func readHandle(c *cursor) (blockHandle, error) {
	o, e := c.num()
	if e != nil {
		return blockHandle{}, e
	}
	n, e := c.num()
	return blockHandle{o, n}, e
}
func tableBlock(file []byte, h blockHandle) ([]byte, error) {
	if h.offset > uint64(len(file)) || h.size > maxValue || h.size+5 > uint64(len(file))-h.offset {
		return nil, errors.New("invalid table block bounds")
	}
	b := file[h.offset : h.offset+h.size+5]
	n := int(h.size)
	if checksum(b[:n+1]) != binary.LittleEndian.Uint32(b[n+1:]) {
		return nil, errors.New("table block checksum mismatch")
	}
	switch b[n] {
	case 0:
		return b[:n], nil
	case 1:
		return decompress(b[:n])
	default:
		return nil, errors.New("unsupported table compression")
	}
}
func blockEntries(b []byte, visit func([]byte, []byte) error) error {
	if len(b) < 4 {
		return errors.New("invalid table block")
	}
	count := uint64(binary.LittleEndian.Uint32(b[len(b)-4:]))
	if count == 0 || count*4+4 > uint64(len(b)) {
		return errors.New("invalid table restart array")
	}
	c := cursor{b: b[:len(b)-4-int(count)*4]}
	var previous []byte
	for c.p < len(c.b) {
		shared, e := c.num()
		if e != nil {
			return e
		}
		fresh, e := c.num()
		if e != nil {
			return e
		}
		n, e := c.num()
		if e != nil {
			return e
		}
		if shared > uint64(len(previous)) || fresh > maxValue || n > maxValue {
			return errors.New("invalid table entry")
		}
		tail, e := c.take(int(fresh))
		if e != nil {
			return e
		}
		value, e := c.take(int(n))
		if e != nil {
			return e
		}
		key := append(append([]byte(nil), previous[:int(shared)]...), tail...)
		if e = visit(key, value); e != nil {
			return e
		}
		previous = key
	}
	return nil
}
func readTable(b []byte, v dbView) error {
	if len(b) < 48 || binary.LittleEndian.Uint64(b[len(b)-8:]) != 0xdb4775248b80fb57 {
		return errors.New("invalid LevelDB table footer")
	}
	c := cursor{b: b[len(b)-48 : len(b)-8]}
	if _, e := readHandle(&c); e != nil {
		return e
	}
	idx, e := readHandle(&c)
	if e != nil {
		return e
	}
	index, e := tableBlock(b, idx)
	if e != nil {
		return e
	}
	return blockEntries(index, func(_, value []byte) error {
		c := cursor{b: value}
		h, e := readHandle(&c)
		if e != nil {
			return e
		}
		block, e := tableBlock(b, h)
		if e != nil {
			return e
		}
		return blockEntries(block, func(k, b []byte) error {
			if len(k) < 8 {
				return errors.New("invalid internal database key")
			}
			tag := binary.LittleEndian.Uint64(k[len(k)-8:])
			return v.put(k[:len(k)-8], b, tag>>8, byte(tag))
		})
	})
}
func readWAL(b []byte, v dbView) error {
	records, e := physicalRecords(b)
	if e != nil {
		return e
	}
	for _, r := range records {
		if len(r) < 12 {
			return errors.New("invalid database write batch")
		}
		seq := binary.LittleEndian.Uint64(r[:8])
		count := binary.LittleEndian.Uint32(r[8:12])
		c := cursor{b: r, p: 12}
		if count > 1000000 {
			return errors.New("database batch exceeds limit")
		}
		for i := uint32(0); i < count; i++ {
			tag, e := c.take(1)
			if e != nil {
				return e
			}
			k, e := c.slice()
			if e != nil {
				return e
			}
			var value []byte
			if tag[0] == 1 {
				value, e = c.slice()
				if e != nil {
					return e
				}
			}
			if e = v.put(k, value, seq+uint64(i), tag[0]); e != nil {
				return e
			}
		}
		if c.p != len(c.b) {
			return errors.New("invalid database write batch length")
		}
	}
	return nil
}

// readDatabase never opens LevelDB for writing: Chromium's custom comparator
// and recovery process must not run against a user's source profile.
func readDatabase(dir string) (dbView, error) {
	current, e := os.ReadFile(filepath.Join(dir, "CURRENT"))
	if e != nil {
		return nil, errors.New("cannot read Termius CURRENT file")
	}
	name := strings.TrimSpace(string(current))
	if !strings.HasPrefix(name, "MANIFEST-") || filepath.Base(name) != name {
		return nil, errors.New("invalid database manifest name")
	}
	type stamp struct {
		name string
		size int64
		mod  int64
	}
	var stamps []stamp
	total := 0
	read := func(name string) ([]byte, error) {
		p := filepath.Join(dir, name)
		s, e := os.Lstat(p)
		if e != nil {
			return nil, e
		}
		if !s.Mode().IsRegular() || s.Size() > maxData-int64(total) {
			return nil, errors.New("database exceeds size limit or contains non-regular files")
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil, e
		}
		total += len(b)
		if total > maxData {
			return nil, errors.New("database exceeds size limit")
		}
		stamps = append(stamps, stamp{name, s.Size(), s.ModTime().UnixNano()})
		return b, nil
	}
	b, e := read(name)
	if e != nil {
		return nil, fmt.Errorf("read Termius manifest: %w", e)
	}
	m, e := parseManifest(b)
	if e != nil {
		return nil, e
	}
	v := dbView{}
	numbers := make([]uint64, 0, len(m.tables))
	for n := range m.tables {
		numbers = append(numbers, n)
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	for _, n := range numbers {
		name := fmt.Sprintf("%06d.ldb", n)
		if _, e := os.Stat(filepath.Join(dir, name)); os.IsNotExist(e) {
			name = fmt.Sprintf("%06d.sst", n)
		}
		b, e := read(name)
		if e != nil {
			return nil, fmt.Errorf("read Termius table: %w", e)
		}
		if e = readTable(b, v); e != nil {
			return nil, e
		}
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		n, e := strconv.ParseUint(strings.TrimSuffix(entry.Name(), ".log"), 10, 64)
		if e != nil {
			continue
		}
		if n < m.log && n != m.prev {
			continue
		}
		b, e := read(entry.Name())
		if e != nil {
			return nil, e
		}
		if e = readWAL(b, v); e != nil {
			return nil, e
		}
	}
	after, e := os.ReadFile(filepath.Join(dir, "CURRENT"))
	if e != nil || !bytes.Equal(current, after) {
		return nil, errors.New("Termius database changed; close Termius and retry")
	}
	for _, s := range stamps {
		after, e := os.Stat(filepath.Join(dir, s.name))
		if e != nil || after.Size() != s.size || after.ModTime().UnixNano() != s.mod {
			return nil, errors.New("Termius database changed; close Termius and retry")
		}
	}
	return v, nil
}

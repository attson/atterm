package termius

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
)

type tables map[string][]map[string]interface{}

var wantedTables = map[string]bool{"hosts": true, "keys": true, "ssh_identities": true, "ssh_configs": true, "groups": true, "tags": true, "tag_hosts": true, "pf_rules": true, "proxies": true, "host_chains": true}

func utf16String(b []byte, order binary.ByteOrder) (string, error) {
	if len(b)%2 != 0 {
		return "", errors.New("invalid UTF-16 value")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u)), nil
}
func prefix(k []byte) (db, store, index uint64, n int, err error) {
	if len(k) == 0 {
		return 0, 0, 0, 0, errors.New("empty IndexedDB key")
	}
	sizes := []int{int(k[0]>>5) + 1, int(k[0]>>2&7) + 1, int(k[0]&3) + 1}
	p := 1
	ids := make([]uint64, 3)
	for i, size := range sizes {
		if p+size > len(k) {
			return 0, 0, 0, 0, errors.New("truncated IndexedDB key")
		}
		for j := 0; j < size; j++ {
			ids[i] |= uint64(k[p+j]) << uint(j*8)
		}
		p += size
	}
	return ids[0], ids[1], ids[2], p, nil
}
func readTables(view dbView) (tables, error) {
	names := map[uint64]string{}
	for _, r := range view {
		if r.deleted {
			continue
		}
		db, store, idx, p, e := prefix(r.key)
		if e != nil {
			return nil, e
		}
		if db != 0 || store != 0 || idx != 0 || len(r.key) <= p || r.key[p] != 201 {
			continue
		}
		c := cursor{b: r.key, p: p + 1}
		origin, e := c.num()
		if e != nil || origin > uint64(len(c.b)-c.p)/2 {
			return nil, errors.New("invalid IndexedDB origin")
		}
		_, _ = c.take(int(origin) * 2)
		n, e := c.num()
		if e != nil || n > uint64(len(c.b)-c.p)/2 {
			return nil, errors.New("invalid IndexedDB database name")
		}
		raw, _ := c.take(int(n) * 2)
		name, e := utf16String(raw, binary.BigEndian)
		if e != nil {
			return nil, e
		}
		if len(r.value) > 8 {
			return nil, errors.New("invalid IndexedDB database ID")
		}
		id := uint64(0)
		for i, b := range r.value {
			id |= uint64(b) << uint(8*i)
		}
		if wantedTables[name] {
			if old, ok := names[id]; ok && old != name {
				return nil, errors.New("ambiguous IndexedDB database")
			}
			names[id] = name
		}
	}
	if len(names) == 0 {
		return nil, errors.New("no supported Termius tables found")
	}
	// One Termius table is stored in the database's first object store. Other
	// stores may be indexes or historical schema data, not independent hosts.
	stores := map[uint64]uint64{}
	for _, r := range view {
		if r.deleted {
			continue
		}
		db, s, idx, p, _ := prefix(r.key)
		if names[db] == "" || s != 0 || idx != 0 || len(r.key) <= p || r.key[p] != 50 {
			continue
		}
		c := cursor{b: r.key, p: p + 1}
		id, e := c.num()
		if e != nil {
			return nil, e
		}
		kind, e := c.take(1)
		if e != nil {
			return nil, e
		}
		if kind[0] == 0 && (stores[db] == 0 || id < stores[db]) {
			stores[db] = id
		}
	}
	out := tables{}
	keys := make([]string, 0, len(view))
	for k := range view {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := view[k]
		if r.deleted {
			continue
		}
		db, s, idx, _, _ := prefix(r.key)
		name := names[db]
		if name == "" || s == 0 || idx != 1 {
			continue
		}
		if stores[db] == 0 {
			return nil, errors.New("missing Termius object store metadata")
		}
		if s != stores[db] {
			continue
		}
		c := cursor{b: r.value}
		if _, e := c.num(); e != nil {
			return nil, e
		}
		value, e := decodeStoredValue(c.b[c.p:], 0)
		if e != nil {
			return nil, fmt.Errorf("unsupported Termius %s record: %w", name, e)
		}
		row, ok := value.(map[string]interface{})
		if !ok {
			if encrypted, yes := value.(string); yes {
				row = map[string]interface{}{"__encrypted_record": encrypted}
			} else {
				return nil, fmt.Errorf("unexpected Termius %s record type", name)
			}
		}
		out[name] = append(out[name], row)
	}
	return out, nil
}

func decodeStoredValue(b []byte, depth int) (interface{}, error) {
	if depth > 4 {
		return nil, errors.New("too many IndexedDB wrappers")
	}
	c := cursor{b: b}
	tag, e := c.take(1)
	if e != nil || tag[0] != 255 {
		return nil, errors.New("missing serialization header")
	}
	version, e := c.num()
	if e != nil {
		return nil, e
	}
	if version == 17 {
		kind, e := c.take(1)
		if e != nil {
			return nil, e
		}
		if kind[0] != 2 {
			return nil, errors.New("external IndexedDB blobs are not supported")
		}
		raw, e := decompress(c.b[c.p:])
		if e != nil {
			return nil, e
		}
		return decodeStoredValue(raw, depth+1)
	}
	if version >= 21 {
		trailer, e := c.take(13)
		if e != nil || trailer[0] != 0xfe {
			return nil, errors.New("unsupported Blink trailer")
		}
	}
	if c.p < len(b) && b[c.p] == 255 {
		c.p++
		version, e = c.num()
		if e != nil {
			return nil, e
		}
	}
	if version < 13 || version > 16 {
		return nil, fmt.Errorf("unsupported V8 serialization version %d", version)
	}
	d := v8Decoder{cursor: c, active: map[int]bool{}}
	return d.value(0)
}

type v8Decoder struct {
	cursor
	refs   []interface{}
	active map[int]bool
	nodes  int
}

func (d *v8Decoder) peek() byte {
	for d.p < len(d.b) && d.b[d.p] == 0 {
		d.p++
	}
	if d.p == len(d.b) {
		return 0
	}
	return d.b[d.p]
}
func (d *v8Decoder) value(depth int) (interface{}, error) {
	d.nodes++
	if depth > 64 || d.nodes > 200000 {
		return nil, errors.New("serialized object exceeds limit")
	}
	tag := d.peek()
	if tag == 0 {
		return nil, errors.New("incomplete serialized object")
	}
	d.p++
	switch tag {
	case '_', '0', '-':
		return nil, nil
	case 'T':
		return true, nil
	case 'F':
		return false, nil
	case '?':
		if _, e := d.num(); e != nil {
			return nil, e
		}
		return d.value(depth + 1)
	case 'I':
		n, e := d.num()
		if n > math.MaxUint32 {
			return nil, errors.New("invalid serialized integer")
		}
		return int64(n>>1) ^ -int64(n&1), e
	case 'U':
		n, e := d.num()
		return n, e
	case 'N', 'D':
		b, e := d.take(8)
		if e != nil {
			return nil, e
		}
		n := math.Float64frombits(binary.LittleEndian.Uint64(b))
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, errors.New("non-finite serialized number")
		}
		if tag == 'D' {
			d.refs = append(d.refs, n)
		}
		return n, nil
	case 'S', '"', 'c':
		b, e := d.slice()
		if e != nil {
			return nil, e
		}
		if tag == 'c' {
			return utf16String(b, binary.LittleEndian)
		}
		if tag == 'S' {
			return string(b), nil
		}
		r := make([]rune, len(b))
		for i, v := range b {
			r[i] = rune(v)
		}
		return string(r), nil
	case '^':
		id, e := d.num()
		if e != nil || id >= uint64(len(d.refs)) || d.active[int(id)] {
			return nil, errors.New("invalid or cyclic object reference")
		}
		return d.refs[id], nil
	case 'o', 'a', 'A':
		id := len(d.refs)
		var length uint64
		var e error
		object := map[string]interface{}{}
		var array []interface{}
		if tag != 'o' {
			length, e = d.num()
			if e != nil || length > 200000 {
				return nil, errors.New("array exceeds limit")
			}
			array = make([]interface{}, int(length))
			d.refs = append(d.refs, array)
		} else {
			d.refs = append(d.refs, object)
		}
		d.active[id] = true
		defer delete(d.active, id)
		if tag == 'A' {
			for i := range array {
				array[i], e = d.value(depth + 1)
				if e != nil {
					return nil, e
				}
			}
		}
		end := byte('{')
		if tag == 'A' {
			end = '$'
		} else if tag == 'a' {
			end = '@'
		}
		count := uint64(0)
		for d.peek() != end {
			key, e := d.value(depth + 1)
			if e != nil {
				return nil, e
			}
			v, e := d.value(depth + 1)
			if e != nil {
				return nil, e
			}
			count++
			if tag == 'o' {
				switch k := key.(type) {
				case string:
					object[k] = v
				case int64:
					object[strconv.FormatInt(k, 10)] = v
				case uint64:
					object[strconv.FormatUint(k, 10)] = v
				default:
					return nil, errors.New("invalid object property")
				}
			} else {
				index, e := strconv.ParseUint(fmt.Sprint(key), 10, 64)
				if e != nil || index >= length {
					return nil, errors.New("unsupported array property")
				}
				array[index] = v
			}
		}
		d.p++
		expected, e := d.num()
		if e != nil || expected != count {
			return nil, errors.New("object property count mismatch")
		}
		if tag == 'o' {
			return object, nil
		}
		expected, e = d.num()
		if e != nil || expected != length {
			return nil, errors.New("array length mismatch")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unsupported V8 tag 0x%02x", tag)
	}
}

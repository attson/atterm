package ringbuf

import "testing"

func TestTailBytes(t *testing.T) {
	cases := []struct {
		name   string
		chunks [][]byte
		n      int
		want   []byte
	}{
		{"empty buffer", nil, 10, nil},
		{"zero n", [][]byte{[]byte("abc")}, 0, nil},
		{"negative n", [][]byte{[]byte("abc")}, -1, nil},
		{"smaller than buffer", [][]byte{[]byte("hello world")}, 5, []byte("world")},
		{"larger than buffer", [][]byte{[]byte("abc")}, 10, []byte("abc")},
		{"spans multiple chunks", [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}, 4, []byte("cdef")},
		{"exact boundary", [][]byte{[]byte("aa"), []byte("bb")}, 2, []byte("bb")},
		{"crosses chunks unaligned", [][]byte{[]byte("foo"), []byte("bar"), []byte("baz")}, 5, []byte("arbaz")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := New(1024)
			for i, data := range tc.chunks {
				b.Push(Chunk{Seq: uint64(i + 1), Data: data})
			}
			got := b.TailBytes(tc.n)
			if string(got) != string(tc.want) {
				t.Fatalf("TailBytes(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

func TestTailBytes_ReturnsCopy(t *testing.T) {
	b := New(1024)
	b.Push(Chunk{Seq: 1, Data: []byte("hello")})
	got := b.TailBytes(5)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	// Mutating the returned slice must not affect the buffer.
	got[0] = 'x'
	again := b.TailBytes(5)
	if string(again) != "hello" {
		t.Fatalf("buffer mutated: %q", again)
	}
}

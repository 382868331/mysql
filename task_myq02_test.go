// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Task MYQ02 regression test: getSystemVar must return values that are not
// silently overwritten when a later read reuses the connection's shared read
// buffer.

package mysql

import (
	"errors"
	"net"
	"testing"
	"time"
)

// taskMYQ02ChunkedConn is a net.Conn that serves pre-built data chunks, one
// per Read call. This simulates the behavior seen with TLS connections, where
// the server's TLS library typically produces a separate TLS record per write
// and Go's crypto/tls.Read returns one record at a time. Delivering each
// protocol packet through its own Read call guarantees that every new packet
// triggers buffer.fill(), which reuses (and overwrites) the shared read
// buffer.
type taskMYQ02ChunkedConn struct {
	chunks [][]byte
	idx    int // current chunk index
	off    int // offset within current chunk
}

func (c *taskMYQ02ChunkedConn) Read(b []byte) (int, error) {
	if c.idx >= len(c.chunks) {
		return 0, errors.New("no more data")
	}
	n := copy(b, c.chunks[c.idx][c.off:])
	c.off += n
	if c.off >= len(c.chunks[c.idx]) {
		c.idx++
		c.off = 0
	}
	return n, nil
}

func (c *taskMYQ02ChunkedConn) Write(b []byte) (int, error)        { return len(b), nil } // swallow writes (e.g. COM_QUERY)
func (c *taskMYQ02ChunkedConn) Close() error                       { return nil }
func (c *taskMYQ02ChunkedConn) LocalAddr() net.Addr                { return nil }
func (c *taskMYQ02ChunkedConn) RemoteAddr() net.Addr               { return nil }
func (c *taskMYQ02ChunkedConn) SetDeadline(_ time.Time) error      { return nil }
func (c *taskMYQ02ChunkedConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *taskMYQ02ChunkedConn) SetWriteDeadline(_ time.Time) error { return nil }

// taskMYQ02Packet wraps a payload in a MySQL protocol packet header.
func taskMYQ02Packet(seq byte, payload []byte) []byte {
	pkt := make([]byte, 4+len(payload))
	pkt[0] = byte(len(payload))
	pkt[1] = byte(len(payload) >> 8)
	pkt[2] = byte(len(payload) >> 16)
	pkt[3] = seq
	copy(pkt[4:], payload)
	return pkt
}

// TestTaskMYQ02GetSystemVarValueSurvivesSecondRead verifies that the value
// returned by a getSystemVar call is not corrupted by a subsequent getSystemVar
// call that reuses the connection's shared read buffer.
//
// Two system variables are read back to back. Each protocol packet is
// delivered through its own Read call, so the trailing EOF of the first
// response (and every packet of the second response) forces buffer.fill() to
// overwrite the memory that the first row value points into. The value
// returned by the first call must still be intact after the second read.
func TestTaskMYQ02GetSystemVarValueSurvivesSecondRead(t *testing.T) {
	// Protocol response for: SELECT @@var_one → "11111111"
	// Sequence numbers start at 1 (client sent COM_QUERY as seq 0).
	//
	//   seq 1: column count = 1
	//   seq 2: column definition (minimal valid)
	//   seq 3: EOF (end of column defs)
	//   seq 4: row data — length-encoded string "11111111"
	//   seq 5: EOF (end of rows)

	colCountPkt := taskMYQ02Packet(1, []byte{0x01})

	colDef := []byte{
		0x03, 'd', 'e', 'f', // catalog = "def"
		0x00, // schema = ""
		0x00, // table = ""
		0x00, // org_table = ""
		0x09, // name length = 9
		'@', '@', 'v', 'a', 'r', '_', 'o', 'n', 'e',
		0x00,       // org_name = ""
		0x0c,       // length of fixed fields
		0x3f, 0x00, // charset = 63 (binary)
		0x14, 0x00, 0x00, 0x00, // column_length = 20
		0x0f,       // type = FIELD_TYPE_VARCHAR
		0x00, 0x00, // flags
		0x00,       // decimals
		0x00, 0x00, // filler
	}
	colDefPkt := taskMYQ02Packet(2, colDef)

	eof1 := taskMYQ02Packet(3, []byte{0xfe, 0x00, 0x00, 0x02, 0x00})

	// Row: length-encoded string "11111111" (8 bytes → length prefix 0x08)
	rowPkt := taskMYQ02Packet(4, []byte{0x08, '1', '1', '1', '1', '1', '1', '1', '1'})

	eof2 := taskMYQ02Packet(5, []byte{0xfe, 0x00, 0x00, 0x02, 0x00})

	// Protocol response for: SELECT @@var_two → "22222222"
	colCountPkt2 := taskMYQ02Packet(1, []byte{0x01})

	colDef2 := []byte{
		0x03, 'd', 'e', 'f',
		0x00,
		0x00,
		0x00,
		0x09, // name length = 9
		'@', '@', 'v', 'a', 'r', '_', 't', 'w', 'o',
		0x00,
		0x0c,
		0x3f, 0x00,
		0x14, 0x00, 0x00, 0x00,
		0x0f,
		0x00, 0x00,
		0x00,
		0x00, 0x00,
	}
	colDefPkt2 := taskMYQ02Packet(2, colDef2)

	eof3 := taskMYQ02Packet(3, []byte{0xfe, 0x00, 0x00, 0x02, 0x00})

	// Row: length-encoded string "22222222"
	rowPkt2 := taskMYQ02Packet(4, []byte{0x08, '2', '2', '2', '2', '2', '2', '2', '2'})

	eof4 := taskMYQ02Packet(5, []byte{0xfe, 0x00, 0x00, 0x02, 0x00})

	conn := &taskMYQ02ChunkedConn{chunks: [][]byte{
		colCountPkt, colDefPkt, eof1, rowPkt, eof2,
		colCountPkt2, colDefPkt2, eof3, rowPkt2, eof4,
	}}

	mc := &mysqlConn{
		netConn:          conn,
		buf:              newBuffer(),
		cfg:              NewConfig(),
		closech:          make(chan struct{}),
		maxAllowedPacket: defaultMaxAllowedPacket,
		sequence:         1, // after COM_QUERY (seq 0)
	}

	val1, err := mc.getSystemVar("var_one")
	if err != nil {
		t.Fatalf("first getSystemVar failed: %v", err)
	}

	val2, err := mc.getSystemVar("var_two")
	if err != nil {
		t.Fatalf("second getSystemVar failed: %v", err)
	}

	// After the second read, the first returned value must still be intact.
	const want1 = "11111111"
	if got := string(val1); got != want1 {
		t.Fatalf("value from first getSystemVar corrupted after second read: got %q, want %q", got, want1)
	}

	const want2 = "22222222"
	if got := string(val2); got != want2 {
		t.Fatalf("value from second getSystemVar wrong: got %q, want %q", got, want2)
	}
}

package mysql

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
)

// TestTaskMYQ01InterpolateTypedNilJSONRawMessage is a regression test for the
// typed-nil json.RawMessage interpolation bug: a typed-nil json.RawMessage
// must be interpolated as SQL NULL (just like a nil []byte), not as an empty
// quoted byte-string literal. A non-nil empty json.RawMessage must still be
// interpolated as the empty string literal ''.
func TestTaskMYQ01InterpolateTypedNilJSONRawMessage(t *testing.T) {
	mc := &mysqlConn{
		buf:              newBuffer(),
		maxAllowedPacket: maxPacketSize,
		cfg: &Config{
			InterpolateParams: true,
		},
	}

	// typed-nil json.RawMessage must produce SQL NULL
	q1, err := mc.interpolateParams("SELECT ?", []driver.Value{json.RawMessage(nil)})
	if err != nil {
		t.Fatalf("interpolateParams(typed-nil RawMessage) returned error: %v", err)
	}
	if q1 != "SELECT NULL" {
		t.Errorf("typed-nil json.RawMessage: expected %q, got %q", "SELECT NULL", q1)
	}

	// non-nil empty json.RawMessage must still produce the empty string literal
	q2, err := mc.interpolateParams("SELECT ?", []driver.Value{json.RawMessage([]byte{})})
	if err != nil {
		t.Fatalf("interpolateParams(empty RawMessage) returned error: %v", err)
	}
	if q2 != "SELECT ''" {
		t.Errorf("non-nil empty json.RawMessage: expected %q, got %q", "SELECT ''", q2)
	}
}

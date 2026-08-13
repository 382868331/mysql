package mysql

import (
	"testing"
)

// TestTaskMYQ05ConnectionAttributesRoundTrip is a hidden regression test for
// task 45 (diagnosi_mysql_005).
//
// Problem: ParseDSN supports the "connectionAttributes" parameter and stores it
// into Config.ConnectionAttributes, but FormatDSN does not serialize it back.
// As a result a ParseDSN -> FormatDSN -> ParseDSN round trip silently drops all
// connection attributes.
//
// This test exercises only the public behavior (ParseDSN / FormatDSN) and does
// not depend on any private implementation detail of the fix.
func TestTaskMYQ05ConnectionAttributesRoundTrip(t *testing.T) {
	dsnIn := "foo:bar@tcp(192.168.1.50:3307)/baz?timeout=10s&connectionAttributes=program_name:MySQLGoDriver%2FTest,program_version:1.2.3"

	cfg, err := ParseDSN(dsnIn)
	if err != nil {
		t.Fatalf("ParseDSN(%q) failed: %v", dsnIn, err)
	}
	if cfg.ConnectionAttributes == "" {
		t.Fatalf("ParseDSN(%q) did not populate ConnectionAttributes", dsnIn)
	}

	formatted := cfg.FormatDSN()

	cfg2, err := ParseDSN(formatted)
	if err != nil {
		t.Fatalf("ParseDSN(FormatDSN(cfg)) failed: %v (formatted=%q)", err, formatted)
	}
	if cfg2.ConnectionAttributes != cfg.ConnectionAttributes {
		t.Fatalf("ConnectionAttributes lost in ParseDSN->FormatDSN->ParseDSN round trip: got %q, want %q (formatted=%q)",
			cfg2.ConnectionAttributes, cfg.ConnectionAttributes, formatted)
	}
}

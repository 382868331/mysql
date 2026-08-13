// Hidden regression test for task MYQ04 (diagnosi_mysql_004).
//
// Target phenomenon: when a transaction's connection has already cached a
// real error (connection was closed after a cancel / network / protocol
// failure), Commit and Rollback must return that original cached error.
// The bug being diagnosed: the pre-fix code returns ErrInvalidConn in both
// cases, masking the real network/protocol error stored in the connection.

package mysql

import (
	"errors"
	"testing"
)

// TestTaskMYQ04CommitRollbackReturnsCachedError injects a sentinel cached
// error into a closed transaction connection and asserts that Commit and
// Rollback surface that original error instead of ErrInvalidConn.
func TestTaskMYQ04CommitRollbackReturnsCachedError(t *testing.T) {
	sentinelErr := errors.New("sentinel cached connection error")

	newClosedConnWithCachedErr := func() *mysqlConn {
		mc := &mysqlConn{}
		// Simulate a real failure: the connection is cancelled (which sets
		// the cached error) and then closed, exactly like the production
		// cancel() path (canceled.Set + cleanup -> closed) does.
		mc.canceled.Set(sentinelErr)
		mc.closed.Store(true)
		return mc
	}

	t.Run("Commit", func(t *testing.T) {
		tx := &mysqlTx{mc: newClosedConnWithCachedErr()}
		err := tx.Commit()
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("Commit() = %v, want original cached error %v", err, sentinelErr)
		}
	})

	t.Run("Rollback", func(t *testing.T) {
		tx := &mysqlTx{mc: newClosedConnWithCachedErr()}
		err := tx.Rollback()
		if !errors.Is(err, sentinelErr) {
			t.Fatalf("Rollback() = %v, want original cached error %v", err, sentinelErr)
		}
	})
}

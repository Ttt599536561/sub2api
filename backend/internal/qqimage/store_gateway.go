package qqimage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"
	"time"
)

// Separate from the QQ business transaction lock. One dedicated PostgreSQL
// session owns this two-key advisory lock; no new persistent business table.
const gatewayLockNamespace = 716709
const gatewayLockID = 231250

type postgresGatewayLease struct {
	store    *Store
	conn     *sql.Conn
	pid      int
	released atomic.Bool
}

func (s *Store) AcquireGatewayLease(ctx context.Context) (gatewayLease, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	var pid int
	err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1,$2),pg_backend_pid()`, gatewayLockNamespace, gatewayLockID).Scan(&acquired, &pid)
	if err != nil || !acquired {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrConflict
	}
	return &postgresGatewayLease{store: s, conn: conn, pid: pid}, nil
}
func (l *postgresGatewayLease) Check(ctx context.Context) error {
	if l.released.Load() {
		return ErrConflict
	}
	return l.conn.PingContext(ctx)
}
func (l *postgresGatewayLease) Guard(ctx context.Context) error {
	if l.released.Load() {
		return ErrConflict
	}
	var held bool
	err := l.store.queryer(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=$1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid=$2 AND objid=$3 AND objsubid=2 AND granted)`, l.pid, gatewayLockNamespace, gatewayLockID).Scan(&held)
	if err != nil {
		return err
	}
	if !held {
		return ErrConflict
	}
	return nil
}
func (l *postgresGatewayLease) Release() {
	if l.released.Swap(true) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = l.conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1,$2)`, gatewayLockNamespace, gatewayLockID)
	// Never return a possibly locked/failed session to the shared pool.
	_ = l.conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = l.conn.Close()
}

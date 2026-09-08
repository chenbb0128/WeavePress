package mysqlrepo

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"

	"github.com/chenbb0128/weavepress/server/internal/modules/identity"
)

type fakeResult struct {
	rowsAffected int64
	err          error
}

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.rowsAffected, r.err }

func TestTranslateError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: sql.ErrNoRows, want: identity.ErrUserNotFound},
		{name: "duplicate username", err: &mysql.MySQLError{Number: duplicateEntryErrorNumber}, want: identity.ErrUsernameTaken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translateError(tt.err); !errors.Is(got, tt.want) {
				t.Fatalf("translateError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureAffected(t *testing.T) {
	if err := ensureAffected(fakeResult{rowsAffected: 1}); err != nil {
		t.Fatalf("ensureAffected() error = %v", err)
	}

	if err := ensureAffected(fakeResult{rowsAffected: 0}); !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("ensureAffected() error = %v, want %v", err, identity.ErrUserNotFound)
	}
}

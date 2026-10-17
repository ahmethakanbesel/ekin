package db

import (
	"database/sql/driver"

	"modernc.org/sqlite"

	"ekin/pkg/fold"
)

// The SQL functions every connection has. Registered before any
// connection opens.
func init() {
	// fold(text) is fold.String: listings search titles with
	// fold(title) LIKE '%' || <folded query> || '%'.
	sqlite.MustRegisterDeterministicScalarFunction("fold", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		switch v := args[0].(type) {
		case string:
			return fold.String(v), nil
		case []byte:
			return fold.String(string(v)), nil
		case nil:
			return nil, nil
		default:
			return "", nil
		}
	})
}

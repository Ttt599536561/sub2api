//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

// Settings reads inside an atomic signup must use that transaction's connection
// and see its state, even when no second pooled connection is available.
func TestAffiliateSettingsReadMethodsReuseTransaction(t *testing.T) {
	for _, method := range []string{"Get", "GetValue", "GetMultiple", "GetAll"} {
		t.Run(method, func(t *testing.T) {
			previousMax := integrationDB.Stats().MaxOpenConnections
			integrationDB.SetMaxOpenConns(1)
			t.Cleanup(func() { integrationDB.SetMaxOpenConns(previousMax) })
			tx := testEntTx(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			txCtx := dbent.NewTxContext(ctx, tx)
			key := fmt.Sprintf("affiliate.tx.%s.%d", method, time.Now().UnixNano())
			_, err := tx.Client().ExecContext(txCtx, "INSERT INTO settings(key,value,updated_at) VALUES($1,'uncommitted',NOW())", key)
			require.NoError(t, err)
			repo := NewSettingRepository(integrationEntClient)
			var value string
			switch method {
			case "Get":
				item, readErr := repo.Get(txCtx, key)
				err = readErr
				if item != nil {
					value = item.Value
				}
			case "GetValue":
				value, err = repo.GetValue(txCtx, key)
			case "GetMultiple":
				var values map[string]string
				values, err = repo.GetMultiple(txCtx, []string{key})
				value = values[key]
			case "GetAll":
				var values map[string]string
				values, err = repo.GetAll(txCtx)
				value = values[key]
			}
			require.NoError(t, err, "a settings read must not wait for another pool connection")
			require.Equal(t, "uncommitted", value)
		})
	}
}

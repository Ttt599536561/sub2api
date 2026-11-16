//go:build unit

package repository

import (
	"fmt"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestGroupWelfareDatabaseErrorsRemainActionable(t *testing.T) {
	for _, tc := range []struct{ constraint, reason string }{
		{"welfare_group_referenced", "GROUP_WELFARE_REFERENCED"},
		{"welfare_group_write_requires_read_committed", "GROUP_WRITE_RETRY_REQUIRED"},
	} {
		t.Run(tc.constraint, func(t *testing.T) {
			err := translatePersistenceError(fmt.Errorf("wrapped: %w", &pq.Error{Code: "P0001", Constraint: tc.constraint, Detail: `{"group_id":7,"campaign_count":2,"unused_supplement_count":3,"welfare_subscription_count":4,"welfare_history_count":5,"internal_ignored":100}`}), nil, nil)
			require.Equal(t, 409, infraerrors.Code(err))
			require.Equal(t, tc.reason, infraerrors.Reason(err))
			require.Equal(t, map[string]string{"group_id": "7", "campaign_count": "2", "unused_supplement_count": "3", "welfare_subscription_count": "4", "welfare_history_count": "5"}, infraerrors.FromError(err).Metadata)
		})
	}
}

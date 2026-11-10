package handler

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQQInviteEventNormalization(t *testing.T) {
	e, ok, err := parseQQInviteEvent([]byte(`{"time":1794312345,"self_id":12345,"post_type":"notice","notice_type":"group_increase","sub_type":"invite","group_id":77777,"user_id":88888,"operator_id":99999}`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "12345", e.SelfID)
	require.Equal(t, "99999", e.OperatorID)
	require.Equal(t, "group_increase", e.Kind)
	require.NotEmpty(t, e.EventID)
	same, _, err := parseQQInviteEvent([]byte(`{"operator_id":"99999","user_id":"88888","group_id":"77777","sub_type":"invite","notice_type":"group_increase","post_type":"notice","self_id":"12345","time":1794312345}`))
	require.NoError(t, err)
	require.Equal(t, e.EventID, same.EventID)
}
func TestQQInviteIgnoresUnrelatedMessages(t *testing.T) {
	for _, raw := range []string{`{"post_type":"meta_event","meta_event_type":"heartbeat"}`, `{"post_type":"message","message_type":"group","raw_message":"/领取奖励"}`, `{"post_type":"message","message_type":"private","raw_message":"hello"}`} {
		_, ok, err := parseQQInviteEvent([]byte(raw))
		require.NoError(t, err)
		require.False(t, ok)
	}
}
func TestQQInvitePrivateClaimUsesAuthenticatedMessageSender(t *testing.T) {
	e, ok, err := parseQQInviteEvent([]byte(`{"post_type":"message","message_type":"private","time":1794312345,"message_id":421,"self_id":12345,"user_id":88888,"raw_message":"/领取奖励"}`))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "private_claim", e.Kind)
	require.Equal(t, "88888", e.UserID)
}
func TestQQInviteRejectsAmbiguousIDsAndFutureEvents(t *testing.T) {
	for _, id := range []string{`1.25`, `-1`, `null`, `"00123"`, `"abc"`} {
		_, _, err := parseQQInviteEvent([]byte(`{"time":1794312345,"self_id":12345,"post_type":"notice","notice_type":"group_increase","group_id":77777,"user_id":` + id + `,"sub_type":"invite","operator_id":99999}`))
		require.Error(t, err, id)
	}
	_, _, err := parseQQInviteEvent([]byte(`{"time":9999999999,"self_id":12345,"post_type":"notice","notice_type":"group_increase","group_id":77777,"user_id":88888}`))
	require.Error(t, err)
}

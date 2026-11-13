package handler

import (
	"context"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/napcat"
	"github.com/Wei-Shaw/sub2api/internal/qqinvite"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type inviteBotStub struct {
	state napcat.Status
	sent  int
	err   error
}

func (s *inviteBotStub) Status(context.Context) (napcat.Status, error) { return s.state, nil }
func (s *inviteBotStub) GroupMembers(context.Context, string) ([]string, error) {
	return []string{"123"}, nil
}
func (s *inviteBotStub) SendGroup(context.Context, string, string, string) (string, error) {
	s.sent++
	return "message", s.err
}
func (s *inviteBotStub) SendPrivate(context.Context, string, string) (string, error) {
	s.sent++
	return "message", s.err
}
func TestQQInviteBotCannotSendFromDifferentLoggedInAccount(t *testing.T) {
	s := &inviteBotStub{state: napcat.Status{Online: true, QQ: "99999"}}
	b := qqInviteBotAdapter{client: s, expectedQQ: "12345"}
	_, err := b.SendPrivate(context.Background(), "88888", "code")
	require.ErrorIs(t, err, napcat.ErrOffline)
	require.Zero(t, s.sent)
	err = b.CheckAccount(context.Background())
	require.ErrorIs(t, err, napcat.ErrOffline)
	s.state = napcat.Status{Online: false, QQ: "12345"}
	_, err = b.SendGroup(context.Background(), "77777", "hello", "88888")
	require.ErrorIs(t, err, napcat.ErrOffline)
	require.Zero(t, s.sent)
}
func TestQQInviteBotKeepsUnknownDeliveryOutcome(t *testing.T) {
	s := &inviteBotStub{state: napcat.Status{Online: true, QQ: "12345"}, err: napcat.ErrDeliveryUnknown}
	b := qqInviteBotAdapter{client: s, expectedQQ: "12345"}
	_, err := b.SendPrivate(context.Background(), "88888", "code")
	require.ErrorIs(t, err, qqinvite.ErrDeliveryUnknown)
	require.Equal(t, 1, s.sent)
}

func TestQQInviteBootstrapCannotBlockMainStartup(t *testing.T) {
	t.Setenv("QQ_INVITE_BOT_QQ", "")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("SELECT id,payload,secrets FROM qq_invite_config_versions").WillDelayFor(6 * time.Second).WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "secrets"}))
	started := time.Now()
	runtime := ProvideQQInviteRuntime(db, nil, nil, nil)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, runtime.Stop(ctx))
	}()
	require.Less(t, time.Since(started), 5500*time.Millisecond)
	require.NoError(t, mock.ExpectationsWereMet())
}

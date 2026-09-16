//go:build unit

package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func postmergeResetPointer[T any](value T) *T { return &value }

func TestPostmergeMonthlyGroupCreationRequiresFiniteSubscriptionLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    string
		limit   *float64
		allowed bool
	}{
		{name: "monthly", kind: SubscriptionTypeSubscription, limit: postmergeResetPointer(100.0), allowed: true},
		{name: "standard", kind: SubscriptionTypeStandard, limit: postmergeResetPointer(100.0)},
		{name: "unlimited", kind: SubscriptionTypeSubscription},
		{name: "zero", kind: SubscriptionTypeSubscription, limit: postmergeResetPointer(0.0)},
		{name: "negative unlimited", kind: SubscriptionTypeSubscription, limit: postmergeResetPointer(-1.0)},
		{name: "infinity", kind: SubscriptionTypeSubscription, limit: postmergeResetPointer(math.Inf(1))},
		{name: "nan", kind: SubscriptionTypeSubscription, limit: postmergeResetPointer(math.NaN())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name: "monthly", Platform: PlatformOpenAI, RateMultiplier: 1,
				SubscriptionType: tc.kind, DailyLimitUSD: tc.limit, AllowSubscriptionDayReset: true,
			})
			if !tc.allowed {
				require.ErrorIs(t, err, ErrResetNotAllowed)
				require.Nil(t, repo.created, "invalid permission must not reach persistence")
				return
			}
			require.NoError(t, err)
			require.True(t, group.AllowSubscriptionDayReset)
			require.Equal(t, tc.limit, repo.created.DailyLimitUSD)
		})
	}
}

func TestPostmergeMonthlyGroupUpdatesKeepPermissionAndQuotaConsistent(t *testing.T) {
	for _, tc := range []struct {
		name           string
		input          UpdateGroupInput
		wantError      bool
		wantPermission bool
		wantKind       string
		wantLimit      *float64
	}{
		{name: "unrelated edit preserves permission", input: UpdateGroupInput{Name: "renamed"}, wantPermission: true, wantKind: SubscriptionTypeSubscription, wantLimit: postmergeResetPointer(100.0)},
		{name: "new current limit", input: UpdateGroupInput{DailyLimitUSD: postmergeResetPointer(200.0)}, wantPermission: true, wantKind: SubscriptionTypeSubscription, wantLimit: postmergeResetPointer(200.0)},
		{name: "standard requires explicit disable", input: UpdateGroupInput{SubscriptionType: SubscriptionTypeStandard}, wantError: true},
		{name: "unlimited requires explicit disable", input: UpdateGroupInput{DailyLimitUSD: postmergeResetPointer(-1.0)}, wantError: true},
		{name: "zero cannot retain permission", input: UpdateGroupInput{DailyLimitUSD: postmergeResetPointer(0.0)}, wantError: true},
		{name: "nonfinite cannot retain permission", input: UpdateGroupInput{DailyLimitUSD: postmergeResetPointer(math.Inf(1))}, wantError: true},
		{name: "disable and change billing type", input: UpdateGroupInput{SubscriptionType: SubscriptionTypeStandard, AllowSubscriptionDayReset: postmergeResetPointer(false)}, wantKind: SubscriptionTypeStandard, wantLimit: postmergeResetPointer(100.0)},
		{name: "disable and remove daily limit", input: UpdateGroupInput{DailyLimitUSD: postmergeResetPointer(-1.0), AllowSubscriptionDayReset: postmergeResetPointer(false)}, wantKind: SubscriptionTypeSubscription},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupRepoStubForAdmin{getByID: &Group{
				ID: 42, Name: "monthly", Platform: PlatformOpenAI, Status: StatusActive,
				SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
				AllowSubscriptionDayReset: true, DailyLimitUSD: postmergeResetPointer(100.0),
			}}
			svc := &adminServiceImpl{groupRepo: repo}
			group, err := svc.UpdateGroup(context.Background(), 42, &tc.input)
			if tc.wantError {
				require.ErrorIs(t, err, ErrResetNotAllowed)
				require.Nil(t, repo.updated, "rejected edits must not reach persistence")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantPermission, group.AllowSubscriptionDayReset)
			require.Equal(t, tc.wantKind, group.SubscriptionType)
			require.Equal(t, tc.wantLimit, group.DailyLimitUSD)
			require.Same(t, group, repo.updated)
		})
	}
}

func TestPostmergeMonthlyGroupLimitChangeControlsNextAutomaticReset(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	sub := resetTestSubscription(now)
	sub.DailyUsageUSD, sub.AutoDailyResetEnabled = 100, true
	require.NoError(t, ValidateDailyReset(sub, now, 0, true))
	repo := &groupRepoStubForAdmin{getByID: sub.Group}
	svc := &adminServiceImpl{groupRepo: repo}
	group, err := svc.UpdateGroup(context.Background(), sub.GroupID, &UpdateGroupInput{DailyLimitUSD: postmergeResetPointer(200.0)})
	require.NoError(t, err)
	sub.Group = group
	require.ErrorIs(t, ValidateDailyReset(sub, now, 0, true), ErrResetNoUsage, "the former limit must not trigger another automatic charge")
	require.NoError(t, ValidateDailyReset(sub, now, 0, false), "partial usage remains manually resettable")
	_, err = svc.UpdateGroup(context.Background(), sub.GroupID, &UpdateGroupInput{AllowSubscriptionDayReset: postmergeResetPointer(false)})
	require.NoError(t, err)
	require.ErrorIs(t, ValidateDailyReset(sub, now, 0, true), ErrResetNotAllowed)
	require.True(t, sub.AutoDailyResetEnabled, "group permission edits must not erase user preference")
}

func TestPostmergeDuplicateMonthlyGroupPreservesResetPermission(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			source := &Group{ID: 42, Name: "monthly", Platform: PlatformOpenAI, Status: StatusActive,
				SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
				AllowSubscriptionDayReset: enabled, DailyLimitUSD: postmergeResetPointer(100.0)}
			repo := newDuplicateGroupRepoStub(source)
			svc := &adminServiceImpl{groupRepo: repo, groupDuplicateRepo: repo}
			copy, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:7", "monthly-copy")
			require.NoError(t, err)
			require.Equal(t, enabled, copy.AllowSubscriptionDayReset, "copying monthly configuration must retain its reset permission")
			require.Equal(t, duplicateGroupInactiveStatus, copy.Status, "copy must still require administrator activation")
			require.Equal(t, source.DailyLimitUSD, copy.DailyLimitUSD)
			recovered, err := svc.DuplicateGroup(context.Background(), source.ID, "admin:7", "monthly-copy")
			require.NoError(t, err)
			require.Equal(t, copy.ID, recovered.ID)
			require.Equal(t, enabled, recovered.AllowSubscriptionDayReset)
			require.Len(t, repo.createdFromSources, 1)
		})
	}
}

func TestPostmergeSimpleModeDoesNotMutateMonthlyResetSettings(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{cfg: &config.Config{RunMode: config.RunModeSimple}, groupRepo: repo}
	created, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name: "simple", Platform: PlatformOpenAI, RateMultiplier: 1,
		SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: postmergeResetPointer(100.0), AllowSubscriptionDayReset: true,
	})
	require.NoError(t, err)
	require.Equal(t, SubscriptionTypeStandard, created.SubscriptionType)
	require.False(t, created.AllowSubscriptionDayReset)
	require.Nil(t, created.DailyLimitUSD)
	repo.getByID = &Group{ID: 42, Name: "monthly", Platform: PlatformOpenAI, RateMultiplier: 1,
		SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: postmergeResetPointer(100.0), AllowSubscriptionDayReset: true}
	updated, err := svc.UpdateGroup(context.Background(), 42, &UpdateGroupInput{
		Name: "renamed", SubscriptionType: SubscriptionTypeStandard,
		DailyLimitUSD: postmergeResetPointer(-1.0), AllowSubscriptionDayReset: postmergeResetPointer(false),
	})
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Name)
	require.Equal(t, SubscriptionTypeSubscription, updated.SubscriptionType)
	require.True(t, updated.AllowSubscriptionDayReset)
	require.Equal(t, 100.0, *updated.DailyLimitUSD)
}

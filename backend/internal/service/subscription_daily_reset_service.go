package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type DailyResetOutcome struct {
	OperationID     string
	Replayed        bool
	Subscription    *UserSubscription
	ResetPerformed  bool
	PreferenceSaved bool
	CheckError      string
}

func resetFingerprint(source string, version int64, date string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", source, version, date))))
}

func resetStateSubscription(state *SubscriptionDailyResetState) *UserSubscription {
	if state == nil || state.Subscription == nil {
		return nil
	}
	sub := *state.Subscription
	sub.DailyResetState = state.View()
	if sub.Status == SubscriptionStatusActive && !sub.ExpiresAt.After(state.ServerTime) {
		sub.Status = SubscriptionStatusExpired
	}
	return &sub
}

func (s *SubscriptionService) GetDailyResetState(ctx context.Context, userID, subscriptionID int64) (*UserSubscription, error) {
	if s.dailyResetRepo == nil {
		return nil, ErrResetNotAllowed
	}
	state, err := s.dailyResetRepo.GetState(ctx, userID, subscriptionID)
	if err != nil {
		return nil, err
	}
	return resetStateSubscription(state), nil
}

func (s *SubscriptionService) attachDailyResetStates(ctx context.Context, userID int64, subs []UserSubscription) error {
	if s.dailyResetRepo == nil {
		return nil
	}
	for i := range subs {
		fresh, err := s.GetDailyResetState(ctx, userID, subs[i].ID)
		if err != nil {
			return err
		}
		subs[i] = *fresh
	}
	return nil
}

func (s *SubscriptionService) ResetSubscriptionDaily(ctx context.Context, userID, subscriptionID, version int64, date, operationID string) (*DailyResetOutcome, error) {
	if s.dailyResetRepo == nil {
		return nil, ErrResetNotAllowed
	}
	if version < 0 || len(operationID) < 8 || len(operationID) > 128 || strings.TrimSpace(operationID) != operationID || strings.ContainsAny(operationID, "\r\n\x00") {
		return nil, infraerrors.BadRequest("RESET_INVALID_REQUEST", "a valid operation ID and version are required")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return nil, infraerrors.BadRequest("RESET_INVALID_REQUEST", "a valid observed date is required")
	}
	result, err := s.dailyResetRepo.Apply(ctx, &SubscriptionDailyResetCommand{
		UserID: userID, SubscriptionID: subscriptionID, ExpectedVersion: version, OperationID: operationID,
		ObservedDate: date, Source: "manual", RequestFingerprint: resetFingerprint("manual", version, date),
	})
	if err != nil {
		return nil, err
	}
	return s.dailyResetOutcome(result), nil
}

func (s *SubscriptionService) dailyResetOutcome(result *SubscriptionDailyResetResult) *DailyResetOutcome {
	sub := resetStateSubscription(result.State)
	if sub != nil {
		if err := s.invalidateSubscriptionCaches(sub.UserID, sub.GroupID); err != nil {
			log.Printf("daily reset cache invalidation: %v", err)
		}
	}
	return &DailyResetOutcome{OperationID: result.Event.OperationID, Replayed: result.Replayed, Subscription: sub, ResetPerformed: true}
}

func (s *SubscriptionService) GetDailyResetOperation(ctx context.Context, userID, subscriptionID int64, operationID string) (*DailyResetOutcome, error) {
	if s.dailyResetRepo == nil {
		return nil, ErrResetNotAllowed
	}
	event, err := s.dailyResetRepo.GetOperation(ctx, userID, subscriptionID, operationID)
	if err != nil {
		return nil, err
	}
	sub, err := s.GetDailyResetState(ctx, userID, subscriptionID)
	if err != nil {
		return nil, err
	}
	return &DailyResetOutcome{OperationID: event.OperationID, Replayed: true, Subscription: sub, ResetPerformed: true}, nil
}

func (s *SubscriptionService) SetAutoDailyReset(ctx context.Context, userID, subscriptionID, version int64, enabled bool) (*DailyResetOutcome, error) {
	if s.dailyResetRepo == nil {
		return nil, ErrResetNotAllowed
	}
	state, err := s.dailyResetRepo.SetAutomatic(ctx, userID, subscriptionID, version, enabled)
	if err != nil {
		return nil, err
	}
	out := &DailyResetOutcome{Subscription: resetStateSubscription(state), PreferenceSaved: true}
	if cacheErr := s.invalidateSubscriptionCaches(userID, state.Subscription.GroupID); cacheErr != nil {
		log.Printf("daily reset preference cache invalidation: %v", cacheErr)
	}
	if !enabled {
		return out, nil
	}
	result, err := s.tryAutoDailyResetState(ctx, state)
	if err != nil {
		out.CheckError = "RESET_CHECK_FAILED"
		log.Printf("daily reset immediate check subscription=%d: %v", subscriptionID, err)
		return out, nil
	}
	if result != nil {
		out = s.dailyResetOutcome(result)
		out.PreferenceSaved = true
	}
	return out, nil
}

func (s *SubscriptionService) tryAutoDailyResetState(ctx context.Context, state *SubscriptionDailyResetState) (*SubscriptionDailyResetResult, error) {
	if err := ValidateDailyReset(state.Subscription, state.ServerTime, state.TodayCount, true); err != nil {
		return nil, nil
	}
	sub := state.Subscription
	date := state.View().CountDate
	result, err := s.dailyResetRepo.Apply(ctx, &SubscriptionDailyResetCommand{
		UserID: sub.UserID, SubscriptionID: sub.ID, ExpectedVersion: sub.DailyResetVersion, ObservedDate: date, Source: "automatic",
		OperationID: fmt.Sprintf("auto:%d:%s", sub.DailyResetVersion, date), RequestFingerprint: resetFingerprint("automatic", sub.DailyResetVersion, date),
	})
	if err != nil && infraerrors.Code(err) < 500 {
		return nil, nil
	}
	return result, err
}

func (s *SubscriptionService) TriggerAutoDailyReset(ctx context.Context, subscriptionID int64) error {
	if s.dailyResetRepo == nil {
		return nil
	}
	sub, err := s.userSubRepo.GetByID(ctx, subscriptionID)
	if err != nil {
		return err
	}
	state, err := s.dailyResetRepo.GetState(ctx, sub.UserID, subscriptionID)
	if err != nil {
		return err
	}
	result, err := s.tryAutoDailyResetState(ctx, state)
	if err != nil {
		return err
	}
	if result != nil {
		s.dailyResetOutcome(result)
	}
	return nil
}

func (s *SubscriptionService) startAutoDailyResetScanner() {
	if s.dailyResetRepo == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.autoResetCancel = cancel
	s.autoResetDone = make(chan struct{})
	go func() {
		defer close(s.autoResetDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var afterID int64
		for {
			s.scanAutoDailyResets(ctx, &afterID)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *SubscriptionService) scanAutoDailyResets(ctx context.Context, afterID *int64) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	candidates, err := s.dailyResetRepo.ListAutomaticCandidates(ctx, *afterID, 100)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("daily reset recovery scan: %v", err)
		}
		return
	}
	if len(candidates) == 0 {
		*afterID = 0
		return
	}
	*afterID = candidates[len(candidates)-1].ID
	jobs := make(chan int64)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if err := s.TriggerAutoDailyReset(ctx, id); err != nil && ctx.Err() == nil {
					log.Printf("daily reset recovery subscription=%d: %v", id, err)
				}
			}
		}()
	}
	for _, candidate := range candidates {
		select {
		case jobs <- candidate.ID:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		}
	}
	close(jobs)
	wg.Wait()
	if len(candidates) < 100 {
		*afterID = 0
	}
}

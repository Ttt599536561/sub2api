package service

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrWelfarePaused              = infraerrors.Conflict("WELFARE_PAUSED", "welfare rewards are paused")
	ErrWelfareNotLaunched         = infraerrors.Conflict("WELFARE_NOT_LAUNCHED", "welfare program has not launched")
	ErrWelfareNoTickets           = infraerrors.Conflict("WELFARE_NO_TICKETS", "no lottery tickets available")
	ErrWelfareQuoteStale          = infraerrors.Conflict("WELFARE_QUOTE_STALE", "welfare balance changed; request a new quote")
	ErrWelfareInsufficientBalance = infraerrors.Conflict("WELFARE_INSUFFICIENT_BALANCE", "insufficient welfare balance")
	ErrWelfareIdempotencyConflict = infraerrors.Conflict("WELFARE_IDEMPOTENCY_CONFLICT", "idempotency key was used with a different request")
	ErrWelfareInvalidAmount       = infraerrors.BadRequest("WELFARE_INVALID_AMOUNT", "amount must be a positive decimal with at most two fractional digits")
	ErrWelfareInvalidRequest      = infraerrors.BadRequest("WELFARE_INVALID_REQUEST", "invalid welfare request")
	ErrWelfareAlreadyCheckedIn    = infraerrors.Conflict("WELFARE_ALREADY_CHECKED_IN", "already checked in for this business day")
	ErrWelfareOperationNotFound   = infraerrors.NotFound("WELFARE_OPERATION_NOT_FOUND", "welfare operation not found")
)

// WelfareWallet is internal state. The API only exposes explicit public DTOs.
type WelfareWallet struct {
	UserID                int64
	BalanceCents          int64
	TotalEarnedCents      int64
	TotalRedeemedCents    int64
	TotalCheckinDays      int64
	CycleID               int64
	CycleDay              int64
	LastCheckinDate       string
	DailyLowCount         int
	EligibleSpend         string
	DrawsUsed             int64
	WalletVersion         int64
	WelfareBalanceVersion int64
}

type WelfareSettings struct {
	Enabled      bool       `json:"enabled"`
	LaunchAt     *time.Time `json:"launch_at"`
	RulesVersion int        `json:"rules_version"`
}

type WelfareState struct {
	Wallet         WelfareWallet
	AccountBalance string
	Program        WelfareSettings
}

type WelfareMilestone struct {
	Day    int64  `json:"day"`
	Status string `json:"status"`
}

type WelfareOverview struct {
	WelfareBalance        string             `json:"welfare_balance"`
	AccountBalance        string             `json:"account_balance"`
	AvailableDraws        int64              `json:"available_draws"`
	DrawsUsed             int64              `json:"draws_used"`
	TotalCheckinDays      int64              `json:"total_checkin_days"`
	CycleDay              int64              `json:"cycle_day"`
	TodayCheckedIn        bool               `json:"today_checked_in"`
	BusinessDate          string             `json:"business_date"`
	NextResetAt           time.Time          `json:"next_reset_at"`
	EligibleSpend         string             `json:"eligible_spend"`
	NextDrawRemaining     string             `json:"next_draw_remaining"`
	TicketDebt            int64              `json:"ticket_debt"`
	WalletVersion         int64              `json:"wallet_version"`
	WelfareBalanceVersion int64              `json:"welfare_balance_version"`
	RewardsEnabled        bool               `json:"rewards_enabled"`
	RulesVersion          int                `json:"rules_version"`
	Milestones            []WelfareMilestone `json:"milestones"`
}

type WelfarePrize struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Amount      string `json:"amount"`
	Probability string `json:"probability"`
}

type WelfareRules struct {
	RulesVersion   int            `json:"rules_version"`
	Timezone       string         `json:"timezone"`
	DrawThreshold  string         `json:"draw_threshold"`
	RedemptionRate string         `json:"redemption_rate"`
	Prizes         []WelfarePrize `json:"prizes"`
}

type WelfareOperation struct {
	OperationID        string           `json:"operation_id"`
	Status             string           `json:"status"`
	Overview           *WelfareOverview `json:"overview"`
	Amount             string           `json:"amount,omitempty"`
	RewardAmount       string           `json:"reward_amount,omitempty"`
	BaseRewardAmount   string           `json:"base_reward_amount,omitempty"`
	StreakRewardAmount string           `json:"streak_reward_amount,omitempty"`
	Prize              *WelfarePrize    `json:"prize,omitempty"`
}

type WelfareQuote struct {
	Amount                string `json:"amount"`
	WelfareBalance        string `json:"welfare_balance"`
	AccountBalance        string `json:"account_balance"`
	AccountBalanceAfter   string `json:"account_balance_after"`
	WelfareBalanceAfter   string `json:"welfare_balance_after"`
	WelfareBalanceVersion int64  `json:"welfare_balance_version"`
}

type WelfareCalendarDay struct {
	Date         string `json:"date"`
	CheckedIn    bool   `json:"checked_in"`
	RewardAmount string `json:"reward_amount,omitempty"`
}

type WelfareCalendar struct {
	Month string               `json:"month"`
	Days  []WelfareCalendarDay `json:"days"`
}

type WelfareRecordFilter struct {
	Type     string
	Page     int
	PageSize int
	DateFrom string
	DateTo   string
}

type WelfareRecord struct {
	ID           string    `json:"id"`
	OperationID  string    `json:"operation_id"`
	Type         string    `json:"type"`
	CreatedAt    time.Time `json:"created_at"`
	BusinessDate string    `json:"business_date"`
	Amount       string    `json:"amount"`
	BalanceAfter string    `json:"balance_after"`
	Description  string    `json:"description"`
}

type WelfareRecords struct {
	Items      []WelfareRecord `json:"items"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	TotalDraws int64           `json:"total_draws"`
}

type WelfareLedgerEntry struct {
	Type                           string
	AmountCents, BalanceAfterCents int64
}
type WelfareMutation struct {
	Result        *WelfareOperation
	Entries       []WelfareLedgerEntry
	TransferCents int64
	BusinessDate  string
	Checkin       bool
	PrivateAudit  any
}

// Mutate serializes each user, replays a committed matching operation before
// calling apply, and commits the wallet, ledger, user credit and outbox together.
type WelfareRepository interface {
	State(context.Context, int64) (*WelfareState, error)
	Mutate(ctx context.Context, userID int64, kind, key, fingerprint string, apply func(*WelfareState) (*WelfareMutation, error)) (*WelfareOperation, error)
	Calendar(context.Context, int64, string) ([]WelfareCalendarDay, error)
	Records(context.Context, int64, WelfareRecordFilter) (*WelfareRecords, error)
	Operation(context.Context, int64, string) (*WelfareOperation, error)
	OperationByKey(context.Context, int64, string, string) (*WelfareOperation, error)
	GetSettings(context.Context) (*WelfareSettings, error)
	UpdateSettings(context.Context, bool) (*WelfareSettings, error)
}

type WelfareRandom func(n int64) (int64, error)
type WelfareOption func(*WelfareService)
type WelfareService struct {
	repo   WelfareRepository
	random WelfareRandom
	now    func() time.Time
}

func WithWelfareRandom(random WelfareRandom) WelfareOption {
	return func(s *WelfareService) { s.random = random }
}
func WithWelfareClock(now func() time.Time) WelfareOption {
	return func(s *WelfareService) { s.now = now }
}
func NewWelfareService(repo WelfareRepository, options ...WelfareOption) *WelfareService {
	s := &WelfareService{repo: repo, random: func(n int64) (int64, error) {
		v, err := rand.Int(rand.Reader, big.NewInt(n))
		if err != nil {
			return 0, err
		}
		return v.Int64(), nil
	}, now: time.Now}
	for _, option := range options {
		option(s)
	}
	return s
}

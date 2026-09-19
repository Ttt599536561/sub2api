package service

import "context"

// OperationByKey recovers a committed response after a lost connection without
// performing or retrying the financial operation. A missing result is not proof
// that an in-flight request failed; explicit retries must keep the original key.
func (s *WelfareService) OperationByKey(ctx context.Context, userID int64, kind, key string) (*WelfareOperation, error) {
	if (kind != "draw" && kind != "redeem") || !validWelfareKey(key) {
		return nil, ErrWelfareInvalidRequest
	}
	return s.repo.OperationByKey(ctx, userID, kind, key)
}

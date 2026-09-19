package service

import (
	"context"
	"log/slog"
)

// SetWelfareAvailabilityProvider is attached during application construction.
// Availability stays true after launch even while new rewards are paused.
func (s *SettingService) SetWelfareAvailabilityProvider(provider func(context.Context) (bool, error)) {
	s.welfareAvailabilityProvider = provider
}

func (s *SettingService) welfareAvailable(ctx context.Context) (bool, error) {
	if s.welfareAvailabilityProvider == nil {
		return false, nil
	}
	available, err := s.welfareAvailabilityProvider(ctx)
	if err != nil {
		slog.Warn("welfare public availability unavailable", "error", err)
		return false, err
	}
	return available, nil
}

// NotifyWelfareSettingsChanged invalidates cached HTML/public configuration.
func (s *SettingService) NotifyWelfareSettingsChanged() {
	if s != nil && s.onUpdate != nil {
		s.onUpdate()
	}
}

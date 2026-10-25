package qqimage

// ConfigValidationError identifies a rejected field without carrying its value.
// It remains ErrInvalid for existing callers outside the configuration form.
type ConfigValidationError struct {
	Field string
	Issue string
}

func (e *ConfigValidationError) Error() string { return "qq_image_config_invalid" }
func (e *ConfigValidationError) Unwrap() error { return ErrInvalid }

func invalidConfig(field, issue string) error {
	return &ConfigValidationError{Field: field, Issue: issue}
}

// Details allows only known field/issue pairs across the HTTP boundary. A future
// caller must not accidentally turn an input value into response metadata.
func (e *ConfigValidationError) Details() (string, string) {
	if e != nil {
		valid := false
		switch e.Field {
		case "request":
			valid = e.Issue == "invalid_request"
		case "receive_mode":
			valid = e.Issue == "invalid_format"
		case "app_id", "allowed_groups":
			valid = e.Issue == "invalid_format" || e.Issue == "required"
		case "bot_secret":
			valid = e.Issue == "invalid_format" || e.Issue == "conflicting_clear" || e.Issue == "missing_credential"
		case "api_key":
			valid = e.Issue == "conflicting_clear" || e.Issue == "missing_credential"
		case "image_hosts":
			valid = e.Issue == "invalid_format" || e.Issue == "too_many"
		case "api_url":
			valid = e.Issue == "invalid_url" || e.Issue == "generation_endpoint" || e.Issue == "required"
		case "public_base_url":
			valid = e.Issue == "invalid_url" || e.Issue == "required"
		case "price", "user_daily_amount":
			valid = e.Issue == "invalid_amount" || e.Issue == "required"
		case "model", "size":
			valid = e.Issue == "required"
		case "user_daily_limit", "group_concurrency", "global_concurrency", "queue_limit", "queue_timeout_seconds", "deadline_seconds", "retention_days", "prompt_max_chars":
			valid = e.Issue == "out_of_range"
		}
		if valid {
			return e.Field, e.Issue
		}
	}
	return "request", "invalid_request"
}

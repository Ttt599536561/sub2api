package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type admissionGroupRepo struct {
	service.GroupRepository
	group *service.Group
	err   error
	reads int
}

func (r *admissionGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	group := *r.group
	return &group, nil
}

package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/qqimage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type runtimeGatewayStub struct {
	started chan struct{}
	stopped chan struct{}
}

func (g *runtimeGatewayStub) Run(ctx context.Context) {
	close(g.started)
	<-ctx.Done()
	close(g.stopped)
}
func (g *runtimeGatewayStub) Status() qqimage.GatewayStatus {
	return qqimage.GatewayStatus{State: "connected"}
}
func TestQQImageGatewayRuntimeStartsAndStops(t *testing.T) {
	g := &runtimeGatewayStub{make(chan struct{}), make(chan struct{})}
	r := &QQImageRuntime{gateway: g}
	r.Start()
	select {
	case <-g.started:
	case <-time.After(time.Second):
		t.Fatal("gateway not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, r.Stop(ctx))
	select {
	case <-g.stopped:
	default:
		t.Fatal("gateway not joined")
	}
}
func TestQQImageConnectionStatusRequiresAdmin(t *testing.T) {
	h := &QQImageHandler{connection: func() qqimage.GatewayStatus { return qqimage.GatewayStatus{State: "connected"} }}
	w := qqHTTPRequest(h, "GET", "/item/connection", "", 0, "", h.AdminConnection)
	require.Equal(t, 401, w.Code)
	w = qqHTTPRequest(h, "GET", "/item/connection", "", 42, service.RoleUser, h.AdminConnection)
	require.Equal(t, 403, w.Code)
	w = qqHTTPRequest(h, "GET", "/item/connection", "", 42, service.RoleAdmin, h.AdminConnection)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"state":"connected"`)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

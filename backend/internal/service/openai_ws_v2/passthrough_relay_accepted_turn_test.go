package openai_ws_v2

import (
	"context"
	"fmt"

	coderws "github.com/coder/websocket"
)

// These streams have one terminal per request. Gate every response on its
// response.create instead of inventing several billable turns from one request.
func newAcceptedTurnTestConns(frames []passthroughTestFrame, model string) (FrameConn, FrameConn) {
	client := newPassthroughTestFrameConn(nil, false)
	upstream := newPassthroughTestFrameConn(nil, false)
	return &acceptedTurnTestClient{passthroughTestFrameConn: client, remaining: len(frames) - 1, model: model},
		&acceptedTurnTestUpstream{passthroughTestFrameConn: upstream, frames: frames}
}

type acceptedTurnTestClient struct {
	*passthroughTestFrameConn
	remaining int
	model     string
}

func (c *acceptedTurnTestClient) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if err := c.passthroughTestFrameConn.WriteFrame(ctx, kind, payload); err != nil {
		return err
	}
	if c.remaining > 0 {
		c.remaining--
		select {
		case c.readCh <- passthroughTestFrame{msgType: coderws.MessageText, payload: []byte(fmt.Sprintf(`{"type":"response.create","model":%q}`, c.model))}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type acceptedTurnTestUpstream struct {
	*passthroughTestFrameConn
	frames []passthroughTestFrame
}

func (c *acceptedTurnTestUpstream) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if err := c.passthroughTestFrameConn.WriteFrame(ctx, kind, payload); err != nil {
		return err
	}
	if len(c.frames) == 0 {
		return fmt.Errorf("unexpected response.create")
	}
	select {
	case c.readCh <- c.frames[0]:
	case <-ctx.Done():
		return ctx.Err()
	}
	c.frames = c.frames[1:]
	if len(c.frames) == 0 {
		return c.Close()
	}
	return nil
}

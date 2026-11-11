package openai_ws_v2

import (
	"context"
	"fmt"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func cacheTTLTestEvent(event, id, usage string) []byte {
	return []byte(fmt.Sprintf(`{"type":%q,"response":{"id":%q,"usage":%s}}`, event, id, usage))
}

func cacheTTLTestUsage(details string) string {
	return `{"input_tokens":1000000,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":1000000,"cache_write_tokens_details":` + details + `}}`
}

func TestRelay_CacheTTLValidation(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		short, long int
	}{
		{"30m", cacheTTLTestUsage(`{"ttl_30m_tokens":1000000,"ttl_12h_tokens":0}`), 1000000, 0},
		{"12h", cacheTTLTestUsage(`{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}`), 0, 1000000},
		{"mixed", cacheTTLTestUsage(`{"ttl_30m_tokens":400000,"ttl_12h_tokens":600000}`), 400000, 600000},
		{"prompt alias", `{"input_tokens":1000000,"output_tokens":0,"prompt_tokens_details":{"cache_write_tokens":1000000,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}}}`, 0, 1000000},
		{"missing", cacheTTLTestUsage(`null`), 0, 0},
		{"absent", `{"input_tokens":1000000,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":1000000}}`, 0, 0},
		{"partial", cacheTTLTestUsage(`{"ttl_12h_tokens":1000000}`), 0, 0},
		{"negative", cacheTTLTestUsage(`{"ttl_30m_tokens":-1,"ttl_12h_tokens":1000001}`), 0, 0},
		{"fractional", cacheTTLTestUsage(`{"ttl_30m_tokens":0.5,"ttl_12h_tokens":999999.5}`), 0, 0},
		{"string", cacheTTLTestUsage(`{"ttl_30m_tokens":"0","ttl_12h_tokens":"1000000"}`), 0, 0},
		{"inconsistent", cacheTTLTestUsage(`{"ttl_30m_tokens":0,"ttl_12h_tokens":999999}`), 0, 0},
		{"overflow", cacheTTLTestUsage(`{"ttl_30m_tokens":9223372036854775808,"ttl_12h_tokens":0}`), 0, 0},
	} {
		for _, terminal := range []string{"response.completed", "response.incomplete", "response.failed", "error"} {
			t.Run(tc.name+"/"+terminal, func(t *testing.T) {
				state := &relayState{}
				parseUsageAndAccumulate(state, cacheTTLTestEvent(terminal, "r", tc.usage), terminal, nil)
				usage := finalizeRelayTurnUsage(state)
				require.Equal(t, 1000000, usage.CacheCreationInputTokens)
				require.Equal(t, tc.short, usage.CacheCreation30mInputTokens)
				require.Equal(t, tc.long, usage.CacheCreation12hInputTokens)
			})
		}
	}
}

func TestRelay_CacheTTLUsageSnapshots(t *testing.T) {
	known := cacheTTLTestUsage(`{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}`)
	unknown := cacheTTLTestUsage(`null`)
	for _, tc := range []struct {
		name     string
		updates  []string
		terminal string
		wantLong int
	}{
		{"partial retained at empty terminal", []string{known}, `{}`, 1000000},
		{"partial retained at zero terminal", []string{known}, `{"input_tokens":0,"output_tokens":0}`, 1000000},
		{"unrelated partial retains details", []string{known, `{"output_tokens":2}`}, `{}`, 1000000},
		{"new partial replaces stale details", []string{known, unknown}, `{}`, 0},
		{"terminal replaces stale details", []string{known}, unknown, 0},
		{"terminal resolves partial unknown", []string{unknown}, known, 1000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &relayState{}
			for _, usage := range tc.updates {
				parseUsageAndAccumulate(state, cacheTTLTestEvent("response.in_progress", "r", usage), "response.in_progress", nil)
			}
			parseUsageAndAccumulate(state, cacheTTLTestEvent("response.incomplete", "r", tc.terminal), "response.incomplete", nil)
			usage := finalizeRelayTurnUsage(state)
			require.Equal(t, 1000000, usage.CacheCreationInputTokens, "snapshots must not be summed")
			require.Equal(t, tc.wantLong, usage.CacheCreation12hInputTokens)
			require.Zero(t, usage.CacheCreation30mInputTokens)
		})
	}
}

func TestRelay_CacheTTLMultiTurnAggregation(t *testing.T) {
	short := cacheTTLTestUsage(`{"ttl_30m_tokens":1000000,"ttl_12h_tokens":0}`)
	long := cacheTTLTestUsage(`{"ttl_30m_tokens":0,"ttl_12h_tokens":1000000}`)
	unknown := cacheTTLTestUsage(`null`)
	for _, tc := range []struct {
		name                string
		usages              []string
		wantShort, wantLong int
	}{
		{"all known", []string{short, long, short}, 2000000, 1000000},
		{"unknown last", []string{short, long, unknown}, 0, 0},
		{"unknown first", []string{unknown, short, long}, 0, 0},
		{"unknown middle", []string{short, unknown, long}, 0, 0},
		{"invalid middle", []string{short, cacheTTLTestUsage(`{"ttl_30m_tokens":1,"ttl_12h_tokens":1}`), long}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var frames []passthroughTestFrame
			for i, usage := range tc.usages {
				frames = append(frames, passthroughTestFrame{msgType: coderws.MessageText, payload: cacheTTLTestEvent("response.completed", fmt.Sprintf("r%d", i), usage)})
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var turns []RelayTurnResult
			result, exit := Relay(ctx, newPassthroughTestFrameConn(nil, false), newPassthroughTestFrameConn(frames, true),
				[]byte(`{"type":"response.create","model":"gpt-6-luna"}`), RelayOptions{OnTurnComplete: func(turn RelayTurnResult) { turns = append(turns, turn) }})
			require.Nil(t, exit)
			require.Len(t, turns, 3)
			require.Equal(t, 3000000, result.Usage.CacheCreationInputTokens)
			require.Equal(t, tc.wantShort, result.Usage.CacheCreation30mInputTokens)
			require.Equal(t, tc.wantLong, result.Usage.CacheCreation12hInputTokens)
			for i, turn := range turns {
				require.Equal(t, 1000000, turn.Usage.CacheCreationInputTokens)
				switch tc.usages[i] {
				case short:
					require.Equal(t, 1000000, turn.Usage.CacheCreation30mInputTokens)
				case long:
					require.Equal(t, 1000000, turn.Usage.CacheCreation12hInputTokens)
				default:
					require.Zero(t, turn.Usage.CacheCreation30mInputTokens)
					require.Zero(t, turn.Usage.CacheCreation12hInputTokens)
				}
			}
		})
	}
}

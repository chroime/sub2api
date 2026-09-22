package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupStreamingACKOverridesAccountInBothHandlerFamilies(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini,
		service.PlatformAntigravity, service.PlatformGrok, service.PlatformKimi, service.PlatformZhipu,
		service.PlatformDeepseek, service.PlatformMiniMax, service.PlatformOpenCodeGo, service.PlatformComposite} {
		for _, enabled := range []bool{true, false} {
			for _, openai := range []bool{true, false} {
				name := platform
				if enabled {
					name += "/on-account-off"
				} else {
					name += "/off-account-on"
				}
				if openai {
					name += "/openai-handler"
				} else {
					name += "/native-handler"
				}
				t.Run(name, func(t *testing.T) {
					cfg := gatewayACKTestConfig(5)
					c, recorder := syntheticFirstResponseHandlerContext()
					groupID := int64(7)
					group := &service.Group{ID: groupID, Platform: platform, Hydrated: true, Status: service.StatusActive, StreamingACKEnabled: &enabled}
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
					accountPlatform := platform
					if platform == service.PlatformComposite {
						accountPlatform = service.PlatformGemini
					}
					account := gatewayACKTestAccount(accountPlatform, !enabled, groupID)
					var stop func()
					if openai {
						stop = (&OpenAIGatewayHandler{cfg: cfg}).startSyntheticFirstResponse(c, true, time.Now(), account, &groupID)
					} else {
						stop = (&GatewayHandler{cfg: cfg}).startStreamingACK(c, true, time.Now(), account, &groupID)
					}
					t.Cleanup(stop)
					if enabled {
						require.Eventually(t, func() bool { return service.StreamingACKCommitted(c) }, time.Second, time.Millisecond)
					} else {
						time.Sleep(15 * time.Millisecond)
					}
					stop()
					if enabled {
						require.Equal(t, ":\n\n", recorder.Body.String())
					} else {
						require.Empty(t, recorder.Body.String())
					}
				})
			}
		}
	}
}

func TestGroupStreamingACKDoesNotOverrideMasterOrNonStreaming(t *testing.T) {
	for _, master := range []bool{true, false} {
		for _, stream := range []bool{true, false} {
			if master && stream {
				continue
			}
			for _, openai := range []bool{true, false} {
				cfg := gatewayACKTestConfig(5)
				cfg.Gateway.SyntheticFirstResponse.Enabled = master
				c, recorder := syntheticFirstResponseHandlerContext()
				groupID, enabled := int64(7), true
				group := &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Hydrated: true, Status: service.StatusActive, StreamingACKEnabled: &enabled}
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				account := gatewayACKTestAccount(service.PlatformOpenAI, false, groupID)
				var stop func()
				if openai {
					stop = (&OpenAIGatewayHandler{cfg: cfg}).startSyntheticFirstResponse(c, stream, time.Now(), account, &groupID)
				} else {
					stop = (&GatewayHandler{cfg: cfg}).startStreamingACK(c, stream, time.Now(), account, &groupID)
				}
				time.Sleep(15 * time.Millisecond)
				stop()
				require.Empty(t, recorder.Body.String())
			}
		}
	}
}

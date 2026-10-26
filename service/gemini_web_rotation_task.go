package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/geminiweb/engine"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	// Imported tickets may already be close to expiry, so refresh shortly after
	// boot. Later rounds follow the interval returned by RotateCookiesPage.
	geminiWebFirstRotateDelay = 15 * time.Second
	geminiWebRotateTimeout    = 180 * time.Second
)

var (
	geminiWebRotateOnce    sync.Once
	geminiWebRotateRunning atomic.Bool
)

// StartGeminiWebCookieRotationTask keeps the Gemini Web account pool alive.
//
// Without it a captured cookie stops authenticating about thirty minutes after
// import: the origin answers with the anonymous shell, the relay silently loses
// the paid models, and the account only fails loudly once it has already
// lapsed. Rotation refreshes the short-lived __Secure-1PSIDTS ticket in place.
func StartGeminiWebCookieRotationTask() {
	geminiWebRotateOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), common.LogText(
				"gemini web cookie rotation task started: first=%s", geminiWebFirstRotateDelay))
			timer := time.NewTimer(geminiWebFirstRotateDelay)
			defer timer.Stop()
			for {
				<-timer.C
				timer.Reset(runGeminiWebCookieRotationOnce())
			}
		})
	})
}

func runGeminiWebCookieRotationOnce() time.Duration {
	next := engine.DefaultRotateInterval
	if !geminiWebRotateRunning.CompareAndSwap(false, true) {
		return next
	}
	defer geminiWebRotateRunning.Store(false)

	accounts, err := model.ListEnabledGeminiWebAccounts()
	if err != nil {
		logger.LogWarn(context.Background(), common.LogText("gemini web rotation: list accounts: %s", err.Error()))
		return next
	}
	for _, account := range accounts {
		cookie := account.Cookie
		proxyURL := account.Proxy
		if proxyURL == "" {
			proxyURL = geminiWebChannelProxy(account.ChannelId)
			// The rotation below still sends this proxy, so a failed binding is
			// logged instead of aborting the account.
			if bindErr := model.BindGeminiWebAccountProxy(account.Id, proxyURL); bindErr != nil {
				logger.LogWarn(context.Background(), common.LogText(
					"gemini web rotation: account #%d proxy binding failed: %s", account.Id, bindErr.Error()))
			}
		}

		// Refresh the short-lived ticket first. SIDCC does not extend the
		// signed-in session; doing it first made the account look alive while
		// __Secure-1PSIDTS had already expired.
		psidtsCookie, psidtsErr := engine.RotatePSIDTSForAccount(
			context.Background(), account.Id, cookie, proxyURL, geminiWebRotateTimeout)
		if psidtsErr == nil {
			cookie = psidtsCookie
		} else {
			logger.LogWarn(context.Background(), common.LogText(
				"gemini web rotation: account #%d __Secure-1PSIDTS rotate failed: %s",
				account.Id, psidtsErr.Error()))
		}

		sidccCookie, sidccInterval, sidccErr := engine.RotateSIDCC(
			context.Background(), cookie, proxyURL, geminiWebRotateTimeout)
		if sidccErr != nil {
			logger.LogWarn(context.Background(), common.LogText(
				"gemini web rotation: account #%d SIDCC rotate failed: %s", account.Id, sidccErr.Error()))
		} else {
			cookie = sidccCookie
			if sidccInterval >= engine.MinRotateInterval {
				next = sidccInterval
			}
		}

		if cookie != account.Cookie {
			updated, updateErr := model.UpdateGeminiWebAccountCookieIfUnchanged(account.Id, account.Cookie, cookie)
			if updateErr != nil {
				logger.LogWarn(context.Background(), common.LogText(
					"gemini web rotation: account #%d cookie write failed: %s", account.Id, updateErr.Error()))
			} else if !updated {
				logger.LogWarn(context.Background(), common.LogText(
					"gemini web rotation: account #%d cookie changed concurrently; discarded stale rotation", account.Id))
				continue
			}
		}

		// A successful rotation is not proof that Google still recognizes the
		// account: RotateCookies can return Set-Cookie for an anonymous session.
		// Probe /app for SNlM0e and only then reset the auth failure count.
		_, _, sessionErr := engine.EnsureSession(
			context.Background(), account.Id, cookie, proxyURL, geminiWebRotateTimeout)
		if sessionErr != nil {
			logger.LogWarn(context.Background(), common.LogText(
				"gemini web rotation: account #%d session is no longer signed in (%s); re-import the cookie",
				account.Id, sessionErr.Error()))
			if markErr := model.MarkGeminiWebAccountResult(account.Id, 401, sessionErr.Error()); markErr != nil {
				logger.LogWarn(context.Background(), common.LogText(
					"gemini web rotation: account #%d result write failed: %s", account.Id, markErr.Error()))
			}
			continue
		}
		if markErr := model.MarkGeminiWebAccountResult(account.Id, 200, ""); markErr != nil {
			logger.LogWarn(context.Background(), common.LogText(
				"gemini web rotation: account #%d result write failed: %s", account.Id, markErr.Error()))
		}
	}
	if next < engine.MinRotateInterval {
		next = engine.MinRotateInterval
	}
	return next
}

// geminiWebChannelProxy resolves the egress configured on the account channel.
// A host that cannot reach Google directly (for example a mainland network)
// needs this proxy for both the relay and the keep-alive rotation.
func geminiWebChannelProxy(channelId int) string {
	channel, err := model.CacheGetChannel(channelId)
	if err != nil || channel == nil {
		return ""
	}
	raw := channel.GetSetting().Proxy
	if raw == "" {
		return ""
	}
	parsed, _, parseErr := common.ParseProxyURLRuntime(raw)
	if parseErr != nil || parsed == nil {
		return ""
	}
	return parsed.String()
}

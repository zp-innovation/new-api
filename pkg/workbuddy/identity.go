package workbuddy

import (
	"crypto/rand"
	"encoding/hex"
	"maps"
	"strings"
	"time"
)

// Realm selects the upstream region. The two realms differ by host, outbound
// identity and billing behaviour, never by credential shape.
type Realm string

const (
	RealmIntl Realm = "intl"
	RealmCN   Realm = "cn"
)

// Product selects which official client the outbound identity imitates. It
// changes only the identity headers and the User-Agent; tokens are issued per
// account and realm and are independent of the product.
type Product string

const (
	ProductDesktop Product = "workbuddy"
	ProductVSCode  Product = "vscode"
	ProductCLI     Product = "cli"
)

// Client versions, copied verbatim from the reference gateway
// (wb_identity.py:29-31).
const (
	DesktopIDECNVersion   = "5.8.3"
	DesktopIDEIntlVersion = "5.8.4"
	CLIIDEVersion         = "2.168.2"
	VSCodeIDEVersion      = "1.119.0"
	VSCodeProductVersion  = "4.12.39997873"
	DesktopBuiltinCLI     = "2.161.0"
	CLIProductName        = "CodeBuddy"
	CLIAgentRuntime       = "other"
	CLIStainlessVersion   = "6.25.0"
	CLINodeVersion        = "v22.21.1"
)

// RealmConfig mirrors REALM_CONFIGS (wb_accounts.py:173-198).
type RealmConfig struct {
	Name            string
	ChatUpstream    string
	BillingUpstream string
	Origin          string
	Domain          string
	ChatUA          string
	BillingUA       string
	HasCheckin      bool
}

var realmConfigs = map[Realm]RealmConfig{
	RealmIntl: {
		Name:            "国际版 (Global)",
		ChatUpstream:    "https://www.workbuddy.ai",
		BillingUpstream: "https://www.workbuddy.ai",
		Origin:          "https://www.workbuddy.ai",
		Domain:          "www.workbuddy.ai",
		ChatUA:          "WorkBuddy/" + DesktopIDEIntlVersion + " workbuddy-ai/" + DesktopIDEIntlVersion + " CLI/" + DesktopBuiltinCLI,
		BillingUA:       "WorkBuddy/" + DesktopIDEIntlVersion,
		HasCheckin:      false,
	},
	RealmCN: {
		Name:            "国内版 (China)",
		ChatUpstream:    "https://copilot.tencent.com",
		BillingUpstream: "https://www.codebuddy.cn",
		Origin:          "https://www.codebuddy.cn",
		Domain:          "copilot.tencent.com",
		ChatUA:          "WorkBuddy/" + DesktopIDECNVersion + " WorkBuddy/" + DesktopIDECNVersion + " CLI/" + DesktopBuiltinCLI,
		BillingUA:       "WorkBuddy/" + DesktopIDECNVersion,
		HasCheckin:      true,
	},
}

// Config returns the realm configuration; an unknown realm falls back to intl,
// exactly like get_realm_config (wb_accounts.py:217-218).
func (r Realm) Config() RealmConfig {
	if cfg, ok := realmConfigs[r]; ok {
		return cfg
	}
	return realmConfigs[RealmIntl]
}

// NormalizeRealm collapses the accepted spellings; anything unknown is intl.
func NormalizeRealm(value string) Realm {
	if strings.EqualFold(strings.TrimSpace(value), string(RealmCN)) {
		return RealmCN
	}
	return RealmIntl
}

// NormalizeProduct collapses the accepted spellings; anything unknown is the
// desktop client, like normalize_product (wb_identity.py:39-48).
func NormalizeProduct(value string) Product {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "workbuddy", "wb", "desktop":
		return ProductDesktop
	case "vscode", "vsc", "ide":
		return ProductVSCode
	case "cli", "codebuddy":
		return ProductCLI
	}
	return ProductDesktop
}

// Endpoint returns the chat base URL and the X-Domain value for a
// (realm, product) pair (wb_identity.py:51-68).
func Endpoint(realm Realm, product Product) (baseURL string, domain string) {
	cfg := realm.Config()
	switch NormalizeProduct(string(product)) {
	case ProductVSCode:
		if realm == RealmCN {
			// The VSCode identity rides the workbuddy.cn edge on the CN realm.
			return "https://www.workbuddy.cn", "www.workbuddy.cn"
		}
		return cfg.ChatUpstream, cfg.Domain
	case ProductCLI:
		// On the CN realm CLI rides copilot.tencent.com; on intl it rides the
		// workbuddy.ai edge, because www.codebuddy.ai does not resolve.
		return cfg.ChatUpstream, cfg.Domain
	default:
		if realm == RealmCN {
			// The desktop client talks to the workbuddy.cn edge on the CN
			// realm. This is also the host the official client refreshes its
			// session against.
			return "https://www.workbuddy.cn", "www.workbuddy.cn"
		}
		return cfg.ChatUpstream, cfg.Domain
	}
}

// Purpose selects which header set a request carries. The upstream splits them:
// the billing endpoints do not accept the chat identity headers, and the chat
// endpoint wants the full identity (wb_accounts.py:1098-1159).
type Purpose string

const (
	PurposeChat    Purpose = "chat"
	PurposeBilling Purpose = "billing"
)

// Identity describes one outbound request identity.
type Identity struct {
	Realm        Realm
	Product      Product
	UID          string
	AccessToken  string
	Domain       string
	EnterpriseID string

	// MachineID and SessionID override the derived device identifiers. Empty
	// means "derive from UID" (see MachineID/SessionID).
	MachineID string
	SessionID string

	// Now and NewUUID are injectable for deterministic tests. A nil value
	// means time.Now and a random version-4 UUID.
	Now     func() time.Time
	NewUUID func() string
}

// Headers assembles the complete outbound header set for one request.
func (i Identity) Headers(purpose Purpose) map[string]string {
	realm := NormalizeRealm(string(i.Realm))
	product := NormalizeProduct(string(i.Product))
	cfg := realm.Config()

	locale := "en-US"
	if realm == RealmCN {
		locale = "zh-CN"
	}

	headers := map[string]string{
		"Content-Type":        "application/json",
		"Accept":              "application/json, text/plain, */*",
		"X-Requested-With":    "XMLHttpRequest",
		"Origin":              cfg.Origin,
		"Referer":             cfg.Origin + "/",
		"X-CodeBuddy-Request": "1",
		"Accept-Language":     locale,
		"X-Machine-ID":        i.machineID(),
		"X-Session-ID":        i.sessionID(),
		"Authorization":       "Bearer " + i.AccessToken,
		"X-User-Id":           uidOrAnonymous(i.UID),
	}

	if purpose == PurposeBilling {
		headers["User-Agent"] = cfg.BillingUA
		headers["X-Request-ID"] = RequestID(i.UID, i.now())
		// The billing endpoints carry the realm-level domain
		// (wb_accounts.py:1106-1133).
		headers["X-Domain"] = i.domainValue(cfg)
		if realm == RealmCN {
			headers["X-Product"] = "SaaS"
		}
	} else {
		// The chat endpoints carry the (realm, product) endpoint domain
		// (wb_identity.py:96/111/129).
		headers["X-Domain"] = i.chatDomainValue(realm, product)
		for key, value := range i.chatIdentityHeaders(realm, product, cfg.ChatUA) {
			headers[key] = value
		}
	}

	if i.EnterpriseID != "" {
		headers["X-Enterprise-Id"] = i.EnterpriseID
		headers["X-Tenant-Id"] = i.EnterpriseID
	}

	return headers
}

// chatIdentityHeaders returns the product-specific part of the chat header set
// (wb_identity.py:76-145). The caller has already merged the common part.
func (i Identity) chatIdentityHeaders(realm Realm, product Product, desktopUA string) map[string]string {
	conversationID := i.newUUID()
	messageID := i.newMessageID()
	requestID := i.newMessageID()

	switch product {
	case ProductVSCode:
		userAgent := "VSCode/" + VSCodeIDEVersion + " WorkBuddy/" + VSCodeProductVersion
		headers := map[string]string{
			"X-Agent-Intent":            "craft",
			"X-Agent-Type":              "main",
			"X-IDE-Type":                "VSCode",
			"X-IDE-Name":                "VSCode",
			"X-IDE-Version":             VSCodeIDEVersion,
			"X-Product-Version":         VSCodeProductVersion,
			"X-Env-ID":                  "production",
			"X-Product":                 "SaaS",
			"User-Agent":                userAgent,
			"X-Conversation-ID":         conversationID,
			"X-Conversation-Request-ID": requestID,
			"X-Conversation-Message-ID": messageID,
			"X-Request-ID":              requestID,
			"X-Request-Trace-Id":        i.newUUID(),
		}
		maps.Copy(headers, i.vscodeTraceHeaders())
		return headers
	case ProductCLI:
		userAgent := "CLI/" + CLIIDEVersion + " " + CLIProductName + "/" + CLIIDEVersion
		headers := map[string]string{
			"X-Agent-Intent":              "craft",
			"X-Agent-Purpose":             "conversation",
			"X-Agent-Runtime":             CLIAgentRuntime,
			"X-Agent-Type":                "main",
			"X-IDE-Type":                  "CLI",
			"X-IDE-Name":                  "CLI",
			"X-IDE-Version":               CLIIDEVersion,
			"X-Product":                   "SaaS",
			"User-Agent":                  userAgent,
			"X-Conversation-ID":           conversationID,
			"X-Conversation-Request-ID":   requestID,
			"X-Root-Request-ID":           requestID,
			"X-Conversation-Message-ID":   messageID,
			"X-Request-ID":                messageID,
			"X-Private-Data":              privateDataFor(i.EnterpriseID),
			"x-stainless-lang":            "js",
			"x-stainless-package-version": CLIStainlessVersion,
			"x-stainless-os":              "Windows",
			"x-stainless-arch":            "x64",
			"x-stainless-runtime":         "node",
			"x-stainless-runtime-version": CLINodeVersion,
			"x-stainless-retry-count":     "0",
			"x-stainless-timeout":         "1200",
		}
		maps.Copy(headers, i.traceHeaders(true))
		return headers
	default:
		ideVersion := DesktopIDEIntlVersion
		if realm == RealmCN {
			ideVersion = DesktopIDECNVersion
		}
		headers := map[string]string{
			"X-Agent-Intent":            "craft",
			"X-Agent-Purpose":           "conversation",
			"X-Agent-Type":              "main",
			"X-IDE-Name":                "WorkBuddy",
			"X-IDE-Type":                "WorkBuddy",
			"X-IDE-Version":             ideVersion,
			"X-Private-Data":            "true",
			"X-Product":                 "SaaS",
			"User-Agent":                desktopUA,
			"X-Conversation-ID":         conversationID,
			"X-Conversation-Request-ID": requestID,
			"X-Root-Request-ID":         requestID,
			"X-Conversation-Message-ID": messageID,
			"X-Request-ID":              messageID,
		}
		maps.Copy(headers, i.traceHeaders(false))
		return headers
	}
}

func (i Identity) traceHeaders(withParent bool) map[string]string {
	trace := i.newMessageID()
	span := i.newSpanID()
	headers := map[string]string{
		"X-Trace-ID":   trace,
		"traceparent":  "00-" + trace + "-" + span + "-01",
		"b3":           trace + "-" + span + "-1",
		"X-B3-TraceId": trace,
		"X-B3-SpanId":  span,
		"X-B3-Sampled": "1",
	}
	if withParent {
		parent := i.newSpanID()
		headers["X-B3-ParentSpanId"] = parent
		headers["b3"] = trace + "-" + span + "-1-" + parent
	}
	return headers
}

func (i Identity) vscodeTraceHeaders() map[string]string {
	trace := i.newMessageID()
	span := i.newSpanID()
	parent := i.newSpanID()
	return map[string]string{
		"X-Trace-ID":        trace,
		"b3":                trace + "-" + span + "-1-" + parent,
		"X-B3-TraceId":      trace,
		"X-B3-ParentSpanId": parent,
		"X-B3-SpanId":       span,
		"X-B3-Sampled":      "1",
	}
}

func (i Identity) newSpanID() string {
	value := i.newMessageID()
	if len(value) >= 16 {
		return value[:16]
	}
	return strings.Repeat("0", 16-len(value)) + value
}

func privateDataFor(enterpriseID string) string {
	if strings.TrimSpace(enterpriseID) != "" {
		return "true"
	}
	return "false"
}

func (i Identity) newMessageID() string {
	return strings.ReplaceAll(i.newUUID(), "-", "")
}

func (i Identity) machineID() string {
	if i.MachineID != "" {
		return i.MachineID
	}
	return MachineID(i.UID)
}

func (i Identity) sessionID() string {
	if i.SessionID != "" {
		return i.SessionID
	}
	return SessionID(i.UID)
}

// chatDomainValue returns the X-Domain for the chat path: the endpoint table's
// domain for this (realm, product) pair, which differs from the realm-level
// domain for the CN VSCode identity (wb_identity.py:51-68, :96/111/129).
func (i Identity) chatDomainValue(realm Realm, product Product) string {
	if i.Domain != "" {
		return i.Domain
	}
	_, domain := Endpoint(realm, product)
	return domain
}

func (i Identity) domainValue(cfg RealmConfig) string {
	if i.Domain != "" {
		return i.Domain
	}
	return cfg.Domain
}

func (i Identity) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

func (i Identity) newUUID() string {
	if i.NewUUID != nil {
		return i.NewUUID()
	}
	return newUUID()
}

// uidOrAnonymous mirrors the reference's "<uid or 'anonymous'>" fallback.
func uidOrAnonymous(uid string) string {
	if uid == "" {
		return "anonymous"
	}
	return uid
}

// newUUID returns a lowercase random version-4 UUID.
func newUUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand never fails on the supported platforms; keep the
		// signature branchless rather than complicating every call site.
		return "00000000000040008000000000000000"
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

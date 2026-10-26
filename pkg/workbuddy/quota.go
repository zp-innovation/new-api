package workbuddy

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// expirySentinelDays is the window beyond which an end time means "does not
// expire" rather than a countdown (wb_accounts.py:279, :1569-1580).
const expirySentinelDays = 730

// enterprisePackageCode marks the synthesized package an enterprise balance
// carries: the enterprise endpoint answers with a period allowance and no
// package list at all (wb_accounts.py:1820-1896).
const enterprisePackageCode = "enterprise"

// fallbackProductCode is the product the billing fallback asks about
// (wb_accounts.py:1741).
const fallbackProductCode = "p_tcaca"

// CreditPackage is one resource package of the balance detail.
type CreditPackage struct {
	Name           string   `json:"name"`
	PackageCode    string   `json:"package_code,omitempty"`
	ProductName    string   `json:"product_name,omitempty"`
	SubProductName string   `json:"sub_product_name,omitempty"`
	GrantReason    string   `json:"grant_reason,omitempty"`
	ResourceID     string   `json:"resource_id,omitempty"`
	DealName       string   `json:"deal_name,omitempty"`
	CreateTime     string   `json:"create_time,omitempty"`
	Remain         float64  `json:"remain"`
	Used           float64  `json:"used"`
	Size           float64  `json:"size"`
	Unit           string   `json:"unit,omitempty"`
	InUsage        bool     `json:"in_usage"`
	AutoRenew      bool     `json:"auto_renew"`
	NoExpiry       bool     `json:"no_expiry"`
	IsExpired      bool     `json:"is_expired"`
	CycleStartTime string   `json:"cycle_start_time,omitempty"`
	CycleEndTime   string   `json:"cycle_end_time,omitempty"`
	ExpireTime     string   `json:"expire_time,omitempty"`
	DaysLeft       *float64 `json:"days_left,omitempty"`
	Status         int      `json:"status"`
}

// CheckinInfo is the CN realm's daily sign-in state.
type CheckinInfo struct {
	TodayCheckedIn bool    `json:"today_checked_in"`
	StreakDays     int     `json:"streak_days"`
	DailyCredit    float64 `json:"daily_credit"`
	TodayCredit    float64 `json:"today_credit"`
	Active         bool    `json:"active"`
}

// ExpiringPackage names the package whose credits lapse first.
type ExpiringPackage struct {
	Name         string  `json:"name"`
	PackageCode  string  `json:"package_code,omitempty"`
	Remain       float64 `json:"remain"`
	CycleEndTime string  `json:"cycle_end_time,omitempty"`
	DaysLeft     float64 `json:"days_left"`
}

// Credits is the balance a channel panel renders. The upstream bills in
// credits per package, so there is no single currency amount here.
type Credits struct {
	Remain           float64          `json:"remain"`
	Used             float64          `json:"used"`
	Size             float64          `json:"size"`
	UsedPercent      string           `json:"used_percent"`
	RemainPercent    string           `json:"remain_percent"`
	IsPaidUser       bool             `json:"is_paid_user"`
	IsEnterprise     bool             `json:"is_enterprise"`
	Checkin          *CheckinInfo     `json:"checkin,omitempty"`
	EarliestExpiring *ExpiringPackage `json:"earliest_expiring,omitempty"`
	Packages         []CreditPackage  `json:"packages"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// numberValue decodes a numeric field the upstream may spell as a number, a
// numeric string or null; an unparsable value reads as zero, like the
// reference's _val() helper (wb_accounts.py:1523-1531).
type numberValue float64

// UnmarshalJSON implements json.Unmarshaler.
func (n *numberValue) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if text == "" || text == "null" {
		*n = 0
		return nil
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		*n = 0
		return nil
	}
	*n = numberValue(parsed)
	return nil
}

func (n numberValue) float() float64 { return float64(n) }

// boolValue decodes a JSON flag the upstream spells either as a boolean or as
// the number 0/1. The international billing meter uses the numeric form, which
// a plain bool rejects with "cannot unmarshal number into Go struct field".
// An unreadable value reads as false rather than failing the whole document,
// matching TextValue.
type boolValue bool

// UnmarshalJSON implements json.Unmarshaler.
func (b *boolValue) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = strings.TrimSpace(unquoted)
	}
	*b = text == "true" || text == "1"
	return nil
}

// accountAttribute is one Key/Value entry of a package's attributes.
type accountAttribute struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// resourceAccount mirrors the upstream package payload. Every capacity field
// comes in a precise and a rounded spelling and the reference takes the first
// one the payload carries (wb_accounts.py:1519-1535). The capacity fields are
// pointers because presence is not truthiness: a present 0 means a spent
// package, while a missing or null key must fall through to the next spelling.
type resourceAccount struct {
	// The international meter spells the id as a number and the domestic one as
	// a string, so the id keeps the upstream text rather than a float.
	AccountID                  TextValue          `json:"AccountId"`
	PackageName                string             `json:"PackageName"`
	PackageCode                string             `json:"PackageCode"`
	ProductName                string             `json:"ProductName"`
	SubProductName             string             `json:"SubProductName"`
	ResourceID                 string             `json:"ResourceId"`
	DealName                   string             `json:"DealName"`
	CapacityUnit               string             `json:"CapacityUnit"`
	CycleStartTime             string             `json:"CycleStartTime"`
	CycleEndTime               string             `json:"CycleEndTime"`
	ExpiredTime                string             `json:"ExpiredTime"`
	Status                     int                `json:"Status"`
	InUsage                    boolValue          `json:"InUsage"`
	AutoRenewFlag              boolValue          `json:"AutoRenewFlag"`
	SupportAutoRenew           boolValue          `json:"SupportAutoRenew"`
	CreateTime                 numberValue        `json:"CreateTime"`
	DeductionEndTime           numberValue        `json:"DeductionEndTime"`
	CycleCapacitySizePrecise   *numberValue       `json:"CycleCapacitySizePrecise"`
	CycleCapacitySize          *numberValue       `json:"CycleCapacitySize"`
	CapacitySizePrecise        *numberValue       `json:"CapacitySizePrecise"`
	CapacitySize               *numberValue       `json:"CapacitySize"`
	CycleCapacityRemainPrecise *numberValue       `json:"CycleCapacityRemainPrecise"`
	CycleCapacityRemain        *numberValue       `json:"CycleCapacityRemain"`
	CapacityRemainPrecise      *numberValue       `json:"CapacityRemainPrecise"`
	CapacityRemain             *numberValue       `json:"CapacityRemain"`
	CycleCapacityUsedPrecise   *numberValue       `json:"CycleCapacityUsedPrecise"`
	CycleCapacityUsed          *numberValue       `json:"CycleCapacityUsed"`
	CapacityUsedPrecise        *numberValue       `json:"CapacityUsedPrecise"`
	CapacityUsed               *numberValue       `json:"CapacityUsed"`
	AccountAttributes          []accountAttribute `json:"AccountAttributes"`
}

// FetchCredits returns the account's balance. The order mirrors the reference:
// on the CN realm the detailed personal billing answer wins, an enterprise
// account whose personal answer is empty falls back to the enterprise usage,
// and everything else lands on the billing fallback (wb_accounts.py:1913-1935).
// Only a meter that does not exist (404/501) may be replaced by the fallback: a
// failing meter has to surface, otherwise the panel would report a different
// balance than the one the upstream actually answered with.
func (c *Client) FetchCredits(ctx context.Context, identity Identity, now time.Time) (*Credits, error) {
	realm := NormalizeRealm(string(identity.Realm))
	if identity.Realm == "" {
		realm = c.Realm
	}
	if realm != RealmCN {
		return c.fetchCreditsFallback(ctx, identity, realm, now)
	}
	detailed, err := c.fetchCreditsCNDetailed(ctx, identity, realm, now)
	if err == nil {
		if detailed.Size == 0 && c.isEnterprise(identity) {
			if enterprise, entErr := c.fetchCreditsEnterprise(ctx, identity, realm, now); entErr == nil {
				return enterprise, nil
			}
		}
		return detailed, nil
	}
	if !isMissingCreditsEndpoint(err) {
		return nil, err
	}
	cause := err
	if c.isEnterprise(identity) {
		enterprise, entErr := c.fetchCreditsEnterprise(ctx, identity, realm, now)
		if entErr == nil {
			return enterprise, nil
		}
		cause = errors.Join(cause, entErr)
	}
	fallback, fallbackErr := c.fetchCreditsFallback(ctx, identity, realm, now)
	if fallbackErr != nil {
		return nil, errors.Join(cause, fallbackErr)
	}
	return fallback, nil
}

// isMissingCreditsEndpoint reports whether a CN meter failure means the meter
// endpoint does not exist for this account rather than that the call failed.
func isMissingCreditsEndpoint(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, status := range []int{apiErr.HTTPStatus, apiErr.Code} {
		if status == http.StatusNotFound || status == http.StatusNotImplemented {
			return true
		}
	}
	return false
}

// isEnterprise reports whether the account belongs to a team space. The id
// lives in the account or, for a switched account, only in the token
// (wb_accounts.py:1803-1818).
func (c *Client) isEnterprise(identity Identity) bool {
	return identity.EnterpriseID != "" || TokenEnterpriseID(identity.AccessToken) != ""
}

// postJSON performs a POST against an absolute URL and decodes the envelope's
// data into out.
func (c *Client) postJSON(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	payload, err := c.callRawURL(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	var decoded envelope
	if err := common.Unmarshal(payload, &decoded); err != nil {
		return fmt.Errorf("workbuddy: decode %s response: %w", url, err)
	}
	if len(decoded.Data) == 0 {
		return nil
	}
	return common.Unmarshal(decoded.Data, out)
}

// billingBase resolves the realm's billing upstream, honouring the client's
// override when one is set.
func (c *Client) billingBase() string {
	if c.BillingBaseURL != "" {
		return strings.TrimRight(c.BillingBaseURL, "/")
	}
	return c.Realm.Config().BillingUpstream
}

// billingHeaders are the billing identity headers. The CN meter endpoints
// additionally want the web client marker, which the fallback does not send
// (wb_accounts.py:1106-1133, :1612-1614).
func billingHeaders(identity Identity) map[string]string {
	return identity.Headers(PurposeBilling)
}

// webBillingHeaders are the billing headers plus the web client marker.
func webBillingHeaders(identity Identity) map[string]string {
	headers := billingHeaders(identity)
	headers["X-Client-Platform"] = "web"
	return headers
}

// summaryPackage is one entry of the CN package overview.
type summaryPackage struct {
	PackageCode         string      `json:"PackageCode"`
	CycleTotalCapacity  numberValue `json:"CycleTotalCapacity"`
	CycleRemainCapacity numberValue `json:"CycleRemainCapacity"`
	CycleUsedCapacity   numberValue `json:"CycleUsedCapacity"`
}

// fetchCreditsCNDetailed walks the CN meter endpoints: the package overview,
// then the free and paid package accounts it names, then the sign-in state.
// A failing free/paid/check-in call is tolerated; a failing overview is not
// (wb_accounts.py:1610-1733).
func (c *Client) fetchCreditsCNDetailed(ctx context.Context, identity Identity, realm Realm, now time.Time) (*Credits, error) {
	headers := webBillingHeaders(identity)
	base := c.billingBase()

	var summary struct {
		IsPaidUser bool             `json:"IsPaidUser"`
		Packages   []summaryPackage `json:"Packages"`
	}
	if err := c.postJSON(ctx, base+ResourceSummaryPath, headers, map[string]any{}, &summary); err != nil {
		return nil, err
	}

	codes := make([]string, 0, len(summary.Packages))
	seenCodes := make(map[string]bool, len(summary.Packages))
	for _, pkg := range summary.Packages {
		code := strings.TrimSpace(pkg.PackageCode)
		if code == "" || seenCodes[code] {
			continue
		}
		seenCodes[code] = true
		codes = append(codes, code)
	}

	accounts := make([]resourceAccount, 0, len(codes))
	if len(codes) > 0 {
		var free struct {
			Accounts []resourceAccount `json:"Accounts"`
		}
		freeBody := map[string]any{
			"PackageCodes": codes,
			"PageNumber":   1,
			"PageSize":     200,
			"Status":       []int{0},
		}
		if err := c.postJSON(ctx, base+ResourceFreePackagesPath, headers, freeBody, &free); err == nil {
			accounts = append(accounts, free.Accounts...)
		}
		var paid struct {
			Accounts []resourceAccount `json:"Accounts"`
		}
		paidBody := map[string]any{
			"PackageCodes":  codes,
			"PageNumber":    1,
			"PageSize":      200,
			"Status":        []int{0, 3},
			"NeedRenewInfo": true,
		}
		if err := c.postJSON(ctx, base+ResourcePaidPackagesPath, headers, paidBody, &paid); err == nil {
			accounts = append(accounts, paid.Accounts...)
		}
	}

	checkin := c.fetchCheckin(ctx, identity, realm)

	packages := parseAccounts(accounts, now)
	// With no individual package the overview's own totals still describe the
	// balance, so they stand in for it without inventing detail rows.
	totals := packages
	if len(totals) == 0 {
		for _, pkg := range summary.Packages {
			totals = append(totals, CreditPackage{
				PackageCode: pkg.PackageCode,
				Size:        pkg.CycleTotalCapacity.float(),
				Remain:      pkg.CycleRemainCapacity.float(),
				Used:        pkg.CycleUsedCapacity.float(),
			})
		}
	}
	return buildCredits(packages, totals, summary.IsPaidUser, false, checkin, now), nil
}

// fetchCheckin reads the CN daily sign-in state; the endpoint's absence is not
// an error, it just leaves the sign-in section empty (wb_accounts.py:1650-1666).
func (c *Client) fetchCheckin(ctx context.Context, identity Identity, realm Realm) *CheckinInfo {
	cfg := realm.Config()
	if !cfg.HasCheckin {
		return nil
	}
	var data struct {
		TodayCheckedIn bool        `json:"today_checked_in"`
		StreakDays     int         `json:"streak_days"`
		DailyCredit    numberValue `json:"daily_credit"`
		TodayCredit    numberValue `json:"today_credit"`
		Active         *bool       `json:"active"`
	}
	if err := c.postJSON(ctx, c.chatBase()+CheckinStatusPath, webBillingHeaders(identity), map[string]any{}, &data); err != nil {
		return nil
	}
	active := true
	if data.Active != nil {
		active = *data.Active
	}
	return &CheckinInfo{
		TodayCheckedIn: data.TodayCheckedIn,
		StreakDays:     data.StreakDays,
		DailyCredit:    roundTo(data.DailyCredit.float(), 2),
		TodayCredit:    roundTo(data.TodayCredit.float(), 2),
		Active:         active,
	}
}

// fetchCreditsEnterprise reads a team space's period allowance. The upstream
// answers with the period usage and limit only, so one package is synthesized
// for the panel's detail list (wb_accounts.py:1820-1896). The period allowance
// refills at the end of the cycle instead of lapsing, which is why the package
// is marked as not expiring.
func (c *Client) fetchCreditsEnterprise(ctx context.Context, identity Identity, realm Realm, now time.Time) (*Credits, error) {
	var data struct {
		Credit         numberValue `json:"credit"`
		LimitNum       numberValue `json:"limitNum"`
		CycleStartTime string      `json:"cycleStartTime"`
		CycleEndTime   string      `json:"cycleEndTime"`
		CycleResetTime string      `json:"cycleResetTime"`
	}
	if err := c.postJSON(ctx, c.billingBase()+EnterpriseUsagePath, webBillingHeaders(identity), map[string]any{}, &data); err != nil {
		return nil, err
	}
	used := roundTo(data.Credit.float(), 2)
	size := roundTo(data.LimitNum.float(), 2)
	remain := 0.0
	if size > 0 {
		remain = roundTo(math.Max(0, size-used), 2)
	}
	cycleEnd := data.CycleEndTime
	if cycleEnd == "" {
		cycleEnd = data.CycleResetTime
	}
	packages := []CreditPackage{}
	if size > 0 {
		packages = append(packages, CreditPackage{
			Name:           "Enterprise quota",
			PackageCode:    enterprisePackageCode,
			GrantReason:    "Enterprise space",
			CreateTime:     data.CycleStartTime,
			Remain:         remain,
			Used:           used,
			Size:           size,
			Unit:           "credits",
			InUsage:        used > 0,
			AutoRenew:      true,
			NoExpiry:       true,
			CycleStartTime: data.CycleStartTime,
			CycleEndTime:   cycleEnd,
		})
	}
	return buildCredits(packages, packages, true, true, nil, now), nil
}

// fetchCreditsFallback asks the billing meter for the account's packages. It is
// the only path the international realm has (wb_accounts.py:1735-1801).
func (c *Client) fetchCreditsFallback(ctx context.Context, identity Identity, realm Realm, now time.Time) (*Credits, error) {
	body := map[string]any{
		"PageNumber":               1,
		"PageSize":                 100,
		"ProductCode":              fallbackProductCode,
		"Status":                   []int{0, 3},
		"PackageEndTimeRangeBegin": now.Format("2006-01-02 15:04:05"),
		// The meter lists only packages ending inside the window, so this is
		// its upper bound. It tracks the caller's clock instead of a fixed
		// year, which would eventually sit behind real package deadlines.
		"PackageEndTimeRangeEnd": now.AddDate(10, 0, 0).Format("2006-01-02 15:04:05"),
	}
	var data struct {
		Response struct {
			Data struct {
				Accounts []resourceAccount `json:"Accounts"`
			} `json:"Data"`
		} `json:"Response"`
	}
	if err := c.postJSON(ctx, c.billingBase()+GetResourcePath, billingHeaders(identity), body, &data); err != nil {
		return nil, err
	}
	packages := parseAccounts(data.Response.Data.Accounts, now)
	return buildCredits(packages, packages, false, false, nil, now), nil
}

// chatBase resolves the chat upstream the client was built for.
func (c *Client) chatBase() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return c.Realm.Config().ChatUpstream
}

// parseAccounts converts the upstream packages and drops duplicate account ids.
func parseAccounts(accounts []resourceAccount, now time.Time) []CreditPackage {
	packages := make([]CreditPackage, 0, len(accounts))
	seen := make(map[TextValue]bool, len(accounts))
	for _, account := range accounts {
		if account.AccountID != "" {
			if seen[account.AccountID] {
				continue
			}
			seen[account.AccountID] = true
		}
		packages = append(packages, parsePackageAccount(account, now))
	}
	return packages
}

// parsePackageAccount converts one upstream package (wb_accounts.py:1519-1608).
func parsePackageAccount(account resourceAccount, now time.Time) CreditPackage {
	size := firstValue(
		account.CycleCapacitySizePrecise,
		account.CycleCapacitySize,
		account.CapacitySizePrecise,
		account.CapacitySize,
	)
	remain := firstValue(
		account.CycleCapacityRemainPrecise,
		account.CycleCapacityRemain,
		account.CapacityRemainPrecise,
		account.CapacityRemain,
	)
	used := firstValue(
		account.CycleCapacityUsedPrecise,
		account.CycleCapacityUsed,
		account.CapacityUsedPrecise,
		account.CapacityUsed,
	)
	// The upstream leaves whichever figure is derivable empty.
	if size > 0 && used <= 0 && remain <= size {
		used = math.Max(0, size-remain)
	} else if size > 0 && remain <= 0 && used < size {
		remain = math.Max(0, size-used)
	}

	grantReason := ""
	for _, attribute := range account.AccountAttributes {
		if attribute.Key == "grantReason" {
			grantReason = attribute.Value
			break
		}
	}

	name := account.PackageName
	if name == "" {
		name = "Package"
	}
	endTime := account.CycleEndTime
	if deduction := deductionEndText(account); deduction != "" {
		endTime = deduction
	}
	if endTime == "" {
		endTime = account.ExpiredTime
	}
	daysLeft, noExpiry, isExpired := expiryState(endTime, now)

	inUsage := bool(account.InUsage)
	if !inUsage && !isExpired && remain > 0 && used > 0 {
		inUsage = true
	}

	unit := account.CapacityUnit
	if unit == "" {
		unit = "credits"
	}
	return CreditPackage{
		Name:           name,
		PackageCode:    account.PackageCode,
		ProductName:    account.ProductName,
		SubProductName: account.SubProductName,
		GrantReason:    grantReason,
		ResourceID:     account.ResourceID,
		DealName:       account.DealName,
		CreateTime:     epochText(account.CreateTime.float()),
		Remain:         roundTo(remain, 2),
		Used:           roundTo(used, 2),
		Size:           roundTo(size, 2),
		Unit:           unit,
		InUsage:        inUsage,
		AutoRenew:      bool(account.AutoRenewFlag) || bool(account.SupportAutoRenew),
		NoExpiry:       noExpiry,
		IsExpired:      isExpired,
		CycleStartTime: account.CycleStartTime,
		CycleEndTime:   account.CycleEndTime,
		ExpireTime:     endTime,
		DaysLeft:       daysLeft,
		Status:         account.Status,
	}
}

// buildCredits assembles the panel's view: totals from totals, the detail rows
// from packages, sorted in-use first, then by expiry, then by size
// (wb_accounts.py:1668-1730).
func buildCredits(packages, totals []CreditPackage, isPaidUser, isEnterprise bool, checkin *CheckinInfo, now time.Time) *Credits {
	remain := 0.0
	used := 0.0
	size := 0.0
	for _, pkg := range totals {
		remain += pkg.Remain
		used += pkg.Used
		size += pkg.Size
	}
	remain = roundTo(remain, 2)
	used = roundTo(used, 2)
	size = roundTo(size, 2)

	sortedPackages := append([]CreditPackage(nil), packages...)
	if sortedPackages == nil {
		// The panel reads `packages` as a list; an empty balance must not
		// serialize as null.
		sortedPackages = []CreditPackage{}
	}
	sort.SliceStable(sortedPackages, func(i, j int) bool {
		left, right := sortedPackages[i], sortedPackages[j]
		if left.InUsage != right.InUsage {
			return left.InUsage
		}
		if left.IsExpired != right.IsExpired {
			return !left.IsExpired
		}
		leftDays, rightDays := expiryDaysOrder(left), expiryDaysOrder(right)
		if leftDays != rightDays {
			return leftDays < rightDays
		}
		return left.Remain > right.Remain
	})

	credits := &Credits{
		Remain:        remain,
		Used:          used,
		Size:          size,
		UsedPercent:   percent(used, size, "0.0%"),
		RemainPercent: percent(remain, size, "100.0%"),
		IsPaidUser:    isPaidUser,
		IsEnterprise:  isEnterprise,
		Checkin:       checkin,
		Packages:      sortedPackages,
		UpdatedAt:     now,
	}
	for _, pkg := range sortedPackages {
		if pkg.Remain <= 0 || pkg.IsExpired || pkg.DaysLeft == nil {
			continue
		}
		if credits.EarliestExpiring == nil || *pkg.DaysLeft < credits.EarliestExpiring.DaysLeft {
			credits.EarliestExpiring = &ExpiringPackage{
				Name:         pkg.Name,
				PackageCode:  pkg.PackageCode,
				Remain:       pkg.Remain,
				CycleEndTime: pkg.CycleEndTime,
				DaysLeft:     *pkg.DaysLeft,
			}
		}
	}
	return credits
}

// firstValue returns the first spelling the payload carries, mirroring the
// reference's first-present lookup over the precise and rounded spellings
// (wb_accounts.py:1519-1535). Presence is not truthiness: a present 0 is a real
// figure (a spent package), so only a missing or null key yields nil and falls
// through.
func firstValue(values ...*numberValue) float64 {
	for _, value := range values {
		if value != nil {
			return value.float()
		}
	}
	return 0
}

// deductionEndText formats the deduction deadline (epoch milliseconds) at which
// a package stops covering usage. The cycle end is only the billing boundary and
// can sit years away from it, so expiry prefers this field
// (wb_accounts.py:282-297).
func deductionEndText(account resourceAccount) string {
	return epochText(account.DeductionEndTime.float())
}

// epochText formats an epoch value, treating values over 1e11 as milliseconds.
func epochText(value float64) string {
	if value <= 0 {
		return ""
	}
	if value > 1e11 {
		value /= 1000
	}
	return time.Unix(int64(value), 0).Local().Format("2006-01-02 15:04:05")
}

// expiryState converts the upstream end time into a countdown. A window beyond
// the sentinel counts as "never expires" instead of a far-off deadline.
func expiryState(text string, now time.Time) (daysLeft *float64, noExpiry, expired bool) {
	cleaned := strings.ReplaceAll(text, "T", " ")
	if len(cleaned) > 19 {
		cleaned = cleaned[:19]
	}
	var end time.Time
	var err error
	if len(cleaned) >= 19 {
		end, err = time.ParseInLocation("2006-01-02 15:04:05", cleaned, now.Location())
	} else if len(cleaned) >= 10 {
		end, err = time.ParseInLocation("2006-01-02", cleaned[:10], now.Location())
	} else {
		return nil, false, false
	}
	if err != nil {
		return nil, false, false
	}
	diffSeconds := end.Sub(now).Seconds()
	if diffSeconds > expirySentinelDays*86400 {
		return nil, true, false
	}
	days := roundTo(diffSeconds/86400, 1)
	return &days, false, diffSeconds < 0
}

// expiryDaysOrder orders packages by expiry: those without one go last.
func expiryDaysOrder(pkg CreditPackage) float64 {
	if pkg.DaysLeft == nil {
		return 99999
	}
	if *pkg.DaysLeft < 0 {
		return 99999 + math.Abs(*pkg.DaysLeft)
	}
	return *pkg.DaysLeft
}

// percent renders a share of the total with the given empty-size fallback.
func percent(value, size float64, fallback string) string {
	if size <= 0 {
		return fallback
	}
	return fmt.Sprintf("%.1f%%", value/size*100)
}

// roundTo rounds to the given number of decimals, like the reference's round().
func roundTo(value float64, digits int) float64 {
	factor := math.Pow(10, float64(digits))
	return math.Round(value*factor) / factor
}

package commandcode

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Usage is the sanitized account and quota view the console renders.
type Usage struct {
	UserID           string       `json:"user_id,omitempty"`
	UserName         string       `json:"user_name,omitempty"`
	OrgID            string       `json:"org_id,omitempty"`
	OrgName          string       `json:"org_name,omitempty"`
	PlanID           string       `json:"plan_id,omitempty"`
	PlanName         string       `json:"plan_name,omitempty"`
	MonthlyCredits   float64      `json:"monthly_credits"`
	PurchasedCredits float64      `json:"purchased_credits"`
	FreeCredits      float64      `json:"free_credits"`
	MonthlyUsed      float64      `json:"monthly_used"`
	MonthlyCap       float64      `json:"monthly_cap"`
	FiveHour         *WindowUsage `json:"five_hour,omitempty"`
	Weekly           *WindowUsage `json:"weekly,omitempty"`
	PeriodStart      string       `json:"period_start,omitempty"`
	PeriodEnd        string       `json:"period_end,omitempty"`
	FetchedAt        time.Time    `json:"fetched_at"`
}

// WindowUsage is one rolling subscription window.
type WindowUsage struct {
	Used    float64 `json:"used"`
	Cap     float64 `json:"cap"`
	Percent float64 `json:"percent"`
	ResetAt string  `json:"resets_at,omitempty"`
}

// PlanName maps the upstream plan id to the display name the official CLI uses.
func PlanName(planID string) string {
	if name, ok := planNames[strings.TrimSpace(planID)]; ok {
		return name
	}
	return strings.TrimSpace(planID)
}

var planNames = map[string]string{
	"individual-go":       "Go",
	"individual-goat":     "GOAT",
	"individual-pro":      "Pro",
	"individual-pro-v1":   "Pro",
	"individual-provider": "Provider",
	"individual-max":      "Max",
	"individual-ultra":    "Ultra",
	"teams-pro":           "Teams Pro",
}

// number accepts both JSON numbers and numeric strings.
type number float64

func (n *number) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*n = 0
		return nil
	}
	if data[0] == '"' {
		var value string
		if err := common.Unmarshal(data, &value); err != nil {
			return err
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return err
		}
		*n = number(parsed)
		return nil
	}
	parsed, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}
	*n = number(parsed)
	return nil
}

func (n number) Float64() float64 {
	return float64(n)
}

type creditsAmounts struct {
	MonthlyCredits   number `json:"monthlyCredits"`
	PurchasedCredits number `json:"purchasedCredits"`
	FreeCredits      number `json:"freeCredits"`
}

type creditsWindow struct {
	Used    number `json:"used"`
	Cap     number `json:"cap"`
	ResetAt number `json:"resetAt"`
}

type creditsPayload struct {
	Credits      *creditsAmounts `json:"credits"`
	WindowLimits struct {
		Limited  bool           `json:"limited"`
		FiveHour *creditsWindow `json:"fiveHour"`
		Weekly   *creditsWindow `json:"weekly"`
	} `json:"windowLimits"`
}

type subscriptionPayload struct {
	Data struct {
		PlanID             string `json:"planId"`
		CurrentPeriodStart string `json:"currentPeriodStart"`
		CurrentPeriodEnd   string `json:"currentPeriodEnd"`
	} `json:"data"`
}

type usageSummaryPayload struct {
	TotalCost           number `json:"totalCost"`
	TotalMonthlyCredits number `json:"totalMonthlyCredits"`
}

func windowUsage(window *creditsWindow) *WindowUsage {
	if window == nil {
		return nil
	}
	usage := &WindowUsage{
		Used: window.Used.Float64(),
		Cap:  window.Cap.Float64(),
	}
	if usage.Cap > 0 {
		usage.Percent = usage.Used / usage.Cap * 100
	}
	if window.ResetAt.Float64() > 0 {
		usage.ResetAt = time.UnixMilli(int64(window.ResetAt.Float64())).UTC().Format(time.RFC3339)
	}
	return usage
}

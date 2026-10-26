package antigravity

import (
	"slices"
	"strings"
)

// ModelQuota is one flattened quota row for the admin quota view.
type ModelQuota struct {
	Model             string  `json:"model"`
	DisplayName       string  `json:"display_name,omitempty"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time,omitempty"`
	Recommended       bool    `json:"recommended,omitempty"`
	SupportsImages    bool    `json:"supports_images,omitempty"`
	SupportsThinking  bool    `json:"supports_thinking,omitempty"`
}

// QuotaFromModels flattens a fetchAvailableModels response into rows sorted by
// model name. A nil response yields no rows.
func QuotaFromModels(resp *FetchAvailableModelsResponse) []ModelQuota {
	if resp == nil || len(resp.Models) == 0 {
		return nil
	}

	rows := make([]ModelQuota, 0, len(resp.Models))
	for name, info := range resp.Models {
		row := ModelQuota{
			Model:            name,
			DisplayName:      info.DisplayName,
			Recommended:      info.Recommended != nil && *info.Recommended,
			SupportsImages:   info.SupportsImages != nil && *info.SupportsImages,
			SupportsThinking: info.SupportsThinking != nil && *info.SupportsThinking,
		}
		if info.QuotaInfo != nil {
			row.RemainingFraction = info.QuotaInfo.RemainingFraction
			row.ResetTime = info.QuotaInfo.ResetTime
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b ModelQuota) int {
		return strings.Compare(a.Model, b.Model)
	})
	return rows
}

// ModelNames lists the model ids offered to the account, sorted and deduped.
// It is the model list a relay channel can advertise.
func ModelNames(resp *FetchAvailableModelsResponse) []string {
	if resp == nil || len(resp.Models) == 0 {
		return nil
	}

	names := make([]string, 0, len(resp.Models))
	for name := range resp.Models {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// TierIDToPlanType maps an upstream tier id to a display plan name.
func TierIDToPlanType(tierID string) string {
	switch strings.ToLower(strings.TrimSpace(tierID)) {
	case "free-tier":
		return "Free"
	case "standard-tier":
		return "Standard"
	case "g1-pro-tier":
		return "Pro"
	case "g1-ultra-tier":
		return "Ultra"
	case "":
		return "Free"
	default:
		// Google keeps adding sub-tiers such as g1-ultra-lite-tier and
		// g1-pro-plus-tier; they belong to the Ultra/Pro family rather than
		// showing up as their raw upstream id in the console.
		if plan := paidPlanFromTierID(tierID); plan != "" {
			return plan
		}
		return strings.TrimSpace(tierID)
	}
}

// paidPlanFromTierID recognizes the g1-ultra-*-tier and g1-pro-*-tier family of
// paid tier ids, returning "" for free, standard and unknown ids.
func paidPlanFromTierID(tierID string) string {
	id := strings.ToLower(strings.TrimSpace(tierID))
	if !strings.HasSuffix(id, "-tier") {
		return ""
	}
	switch {
	case strings.HasPrefix(id, "g1-ultra-"):
		return "Ultra"
	case strings.HasPrefix(id, "g1-pro-"):
		return "Pro"
	default:
		return ""
	}
}

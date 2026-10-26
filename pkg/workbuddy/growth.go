package workbuddy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The growth centre is CN-only. Its web fallback host is hard-coded in the
// reference gateway and differs from the realm origin (www.codebuddy.cn):
// the growth centre the browser talks to lives on www.workbuddy.cn
// (wb_tasks.py:19, :293-311).
const (
	cnWebOrigin     = "https://www.workbuddy.cn"
	cnWebUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"
	acceptChunkSize = 20
	nightWindowFrom = 23
	nightWindowTo   = 8
)

// growthTaskSpec is the local knowledge about one upstream task: the event kind
// that advances it, its reward and whether it can be automated at all
// (wb_tasks.py:37-74).
type growthTaskSpec struct {
	Kind        string
	Target      int
	Reward      float64
	Name        string
	Unforgeable bool
	Reason      string
	DesktopOnly string
}

var growthTaskSpecs = map[string]growthTaskSpec{
	"create_canvas":               {Kind: "canvas", Target: 1, Reward: 300, Name: "Create a design"},
	"template_5":                  {Kind: "template", Target: 5, Reward: 200, Name: "Create from a template"},
	"expert_5":                    {Kind: "expert", Target: 5, Reward: 200, Name: "Use expert assistants"},
	"Expert_team_use_3":           {Kind: "team", Target: 3, Reward: 150, Name: "Use expert teams"},
	"skill_1":                     {Kind: "skill", Target: 1, Reward: 100, Name: "Try a skill"},
	"automation_1":                {Kind: "automation", Target: 1, Reward: 100, Name: "Create an automation"},
	"playbook_prompt":             {Kind: "playbook", Target: 1, Reward: 100, Name: "Use a playbook"},
	"Expert_lighthouse":           {Kind: "lighthouse", Target: 1, Reward: 100, Name: "Use the Lighthouse expert"},
	"Buddy_App":                   {Kind: "buddy5", Target: 1, Reward: 100, Name: "Open a Buddy app", DesktopOnly: "open any Buddy app in the desktop client"},
	"Buddy_App_QQ":                {Kind: "buddy5", Target: 1, Reward: 100, Name: "Open the teacher assistant", DesktopOnly: "open the teacher assistant in the desktop client"},
	"Hp_Appearance":               {Kind: "skin", Target: 1, Reward: 100, Name: "Apply a theme"},
	"chat_5":                      {Kind: "chat", Target: 5, Reward: 100, Name: "Start 5 conversations"},
	"Model_chat_GLM5.2":           {Kind: "glmchat", Target: 1, Reward: 100, Name: "Try GLM-5.2"},
	"black_cat":                   {Kind: "cat", Target: 3, Reward: 100, Name: "Night owl (23:00-08:00)"},
	"RichMeow_Chat":               {Kind: "richmeow", Target: 1, Reward: 100, Name: "Desktop chat event chain", DesktopOnly: "start a conversation in the desktop client"},
	"Library_read":                {Kind: "library", Target: 1, Reward: 100, Name: "Browse the library", DesktopOnly: "open the library in the desktop client and read the intro"},
	"first_buddy":                 {Kind: "chat", Target: 1, Reward: 300, Name: "Claim a Buddy"},
	"wb_wechat_oa_subscribe_task": {Unforgeable: true, Reward: 100, Name: "Follow the official account", Reason: "requires following the Tencent WorkBuddy WeChat account for 24 hours"},
	"Expert_Philanthropy":         {Unforgeable: true, Reason: "a real donation", Name: "Charity donation"},
}

// Expert and team events are deduplicated upstream by (eventCode, id), so every
// report has to carry a different id or the progress never moves
// (wb_tasks.py:86-105).
var expertIDPool = []string{
	"ex_PZw8Gu81HfN4", "ex_ROsDtJbzADFV", "ex_SMUnl0nJbPix", "ex_ZTR062oVBOCW",
	"ex_a3sSSFBy8qaC", "ex_aG1kvKbq8lPx", "ex_al1vxtUOYQ10", "ex_cZfiyuET9UQP",
	"ex_eggOvQuVP0hq", "ex_hSwsQjkSKnkX", "ex_mMbwwmFA9n9P", "ex_uAQE5POfk7Zh",
	"ex_uZzSAScSy7FZ", "ex_LHywGrZOtG7G", "ex_NX5C8GBciVed", "ex_DdCsaoq4AtcO",
	"ex_KzqKQguubrNQ", "ex_2cvvUZQhDyeJ",
}

var teamIDPool = []string{
	"CloudOpsTeam", "CloudContentTeam", "CloudDevTeam", "ProductStrategyTeam",
	"MarketingCampaignTeam", "SalesBattleTeam", "DesignEngineTeam", "HrOperationsTeam",
}

// GrowthTask is one entry of the upstream task list.
type GrowthTask struct {
	Code         string  `json:"task_code"`
	Name         string  `json:"name"`
	Description  string  `json:"description,omitempty"`
	JumpURL      string  `json:"jump_url,omitempty"`
	Status       string  `json:"status"`
	Type         string  `json:"task_type"`
	Current      int     `json:"current"`
	Target       int     `json:"target"`
	RewardCredit float64 `json:"reward_credit"`
	RewardEnergy float64 `json:"reward_energy"`
	Unforgeable  bool    `json:"unforgeable,omitempty"`
	Reason       string  `json:"reason,omitempty"`
	Kind         string  `json:"-"`
	DesktopOnly  string  `json:"-"`
}

// NeedsAcceptance reports whether the task still has to be accepted before its
// events count.
func (t GrowthTask) NeedsAcceptance() bool {
	return t.Status == "not_accepted" && !t.Unforgeable && t.Type != "auto"
}

// FetchGrowthTasks lists the CN growth tasks with their progress
// (wb_tasks.py:107-137).
func (c *Client) FetchGrowthTasks(ctx context.Context, identity Identity) ([]GrowthTask, error) {
	var data struct {
		Tasks []struct {
			Code        string `json:"task_code"`
			Title       string `json:"title"`
			Description string `json:"description"`
			TaskDesc    string `json:"task_desc"`
			JumpURL     string `json:"jump_url"`
			AcceptState string `json:"accept_status"`
			TaskType    string `json:"task_type"`
			Progress    struct {
				Current int `json:"current"`
				Target  int `json:"target"`
			} `json:"progress"`
			RewardCredit numberValue `json:"reward_credit"`
			RewardEnergy numberValue `json:"reward_energy"`
		} `json:"tasks"`
	}
	if err := c.getJSON(ctx, c.chatBase()+GrowthTasks, billingHeaders(identity), &data); err != nil {
		return nil, err
	}
	tasks := make([]GrowthTask, 0, len(data.Tasks))
	for _, raw := range data.Tasks {
		spec := growthTaskSpecs[raw.Code]
		name := raw.Title
		if name == "" {
			name = spec.Name
		}
		description := raw.Description
		if description == "" {
			description = raw.TaskDesc
		}
		target := raw.Progress.Target
		if target == 0 {
			target = spec.Target
		}
		reward := raw.RewardCredit.float()
		if reward == 0 {
			reward = spec.Reward
		}
		status := raw.AcceptState
		if status == "" {
			status = "not_accepted"
		}
		tasks = append(tasks, GrowthTask{
			Code:         raw.Code,
			Name:         name,
			Description:  description,
			JumpURL:      raw.JumpURL,
			Status:       status,
			Type:         raw.TaskType,
			Current:      raw.Progress.Current,
			Target:       target,
			RewardCredit: reward,
			RewardEnergy: raw.RewardEnergy.float(),
			Unforgeable:  spec.Unforgeable,
			Reason:       spec.Reason,
			Kind:         spec.Kind,
			DesktopOnly:  spec.DesktopOnly,
		})
	}
	return tasks, nil
}

// AcceptResult reports which task codes the upstream accepted.
type AcceptResult struct {
	Accepted []string
	Failed   []string
	Reasons  []string
}

// AcceptGrowthTasks accepts the given task codes in chunks. The upstream only
// counts events for accepted tasks, so a failed acceptance has to be visible
// (wb_tasks.py:205-267).
func (c *Client) AcceptGrowthTasks(ctx context.Context, identity Identity, codes []string) (*AcceptResult, error) {
	result := &AcceptResult{}
	if len(codes) == 0 {
		return result, nil
	}
	var firstErr error
	for start := 0; start < len(codes); start += acceptChunkSize {
		end := min(start+acceptChunkSize, len(codes))
		part := codes[start:end]
		var data struct {
			Results []struct {
				Code    string `json:"task_code"`
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"results"`
		}
		err := c.postJSON(ctx, c.chatBase()+GrowthAccept, billingHeaders(identity), map[string]any{"task_codes": part}, &data)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			result.Failed = append(result.Failed, part...)
			result.Reasons = append(result.Reasons, err.Error())
			continue
		}
		seen := map[string]bool{}
		for _, item := range data.Results {
			seen[item.Code] = true
			if item.Status == "accepted" || item.Status == "already_accepted" {
				result.Accepted = append(result.Accepted, item.Code)
				continue
			}
			result.Failed = append(result.Failed, item.Code)
			result.Reasons = append(result.Reasons, fmt.Sprintf("%s: %s", item.Code, item.Message))
		}
		for _, code := range part {
			if !seen[code] {
				result.Failed = append(result.Failed, code)
			}
		}
	}
	return result, firstErr
}

// ClaimResult is one reward claim.
type ClaimResult struct {
	OK      bool    `json:"ok"`
	Credit  float64 `json:"credit"`
	Energy  float64 `json:"energy"`
	Message string  `json:"message,omitempty"`
}

// ClaimGrowthTask claims one task reward. The chat host rejects a claim with
// HTTP 400 once the progress has not landed yet, and the reference gateway then
// retries the same call on the growth-centre web host with the browser identity
// (wb_tasks.py:269-318).
func (c *Client) ClaimGrowthTask(ctx context.Context, identity Identity, code string) *ClaimResult {
	path := "/activity/growth/tasks/" + code + "/claim"
	if credit, energy, err := c.claimAt(ctx, c.chatBase()+path, billingHeaders(identity)); err == nil {
		return &ClaimResult{OK: true, Credit: credit, Energy: energy}
	} else if !isHTTPStatus(err, 400) {
		return &ClaimResult{Message: err.Error()}
	}
	credit, energy, err := c.claimAt(ctx, c.webBase()+path, webGrowthHeaders(identity, c.webBase()))
	if err != nil {
		return &ClaimResult{Message: err.Error()}
	}
	return &ClaimResult{OK: true, Credit: credit, Energy: energy}
}

func (c *Client) claimAt(ctx context.Context, url string, headers map[string]string) (float64, float64, error) {
	var data struct {
		Credit numberValue `json:"credit"`
		Energy numberValue `json:"energy"`
	}
	if err := c.postJSON(ctx, url, headers, map[string]any{}, &data); err != nil {
		return 0, 0, err
	}
	return data.Credit.float(), data.Energy.float(), nil
}

// webGrowthHeaders are the browser identity the growth centre expects on the
// fallback claim (wb_tasks.py:296-305). The origin travels in because the web
// host is overridable, exactly like the other bases.
func webGrowthHeaders(identity Identity, origin string) map[string]string {
	return map[string]string{
		"Authorization":     "Bearer " + identity.AccessToken,
		"Accept":            "application/json, text/plain, */*",
		"Content-Type":      "application/json",
		"Origin":            origin,
		"Referer":           origin + "/profile/growth-center",
		"X-Client-Platform": "web",
		"User-Agent":        cnWebUserAgent,
		"X-User-Id":         uidOrAnonymous(identity.UID),
		"X-Domain":          origin,
	}
}

// isHTTPStatus reports whether err is an *APIError whose transport answered with
// the given status.
func isHTTPStatus(err error, status int) bool {
	var apiErr *APIError
	return asAPIError(err, &apiErr) && apiErr.HTTPStatus == status
}

// BuildGrowthEvent builds one canonical growth event for the given kind. The
// shapes are the desktop client's own report payloads, captured per kind
// (wb_tasks.py:321-388).
func BuildGrowthEvent(uid, kind string, idx int, expertID string, now time.Time) GrowthEvent {
	millis := now.UnixMilli()
	conversation := fmt.Sprintf("wb-task-%d-%d", millis, idx)
	request := conversation + "-req"
	base := GrowthEvent{
		"timestamp":   millis,
		"reportDelay": 0,
	}
	switch kind {
	case "canvas":
		base["eventCode"] = "wbx_design_canvas_task_create"
		base["conversationId"] = conversation
		base["requestId"] = request
		base["source"] = "summon_keyword"
		base["isCustomModel"] = false
		base["name"] = ""
		base["inputLength"] = 12
		base["id"] = fmt.Sprintf("wbx-canvas-%d", millis)
		base["cost"] = 0
		base["isSuccessful"] = true
	case "template":
		base["eventCode"] = "agent_task_created_with_template"
		base["isCustomModel"] = true
		base["id"] = fmt.Sprintf("%d", idx)
		base["name"] = "slides"
		base["requestId"] = request
		base["conversationId"] = conversation
	case "expert", "team", "lighthouse":
		expertType := "agent"
		if kind == "team" {
			expertType = "team"
		}
		name := expertID
		if name == "" {
			name = "ContentCreator"
		}
		base["eventCode"] = "expert_actual_use"
		base["mode"] = "CLOUD"
		base["id"] = name
		base["name"] = name
		base["expertTitle"] = name
		base["type"] = "02-Engineering"
		base["expertType"] = expertType
		base["source"] = "builtin"
		base["version"] = "1.0.2"
		base["cost"] = 0
		base["characterCount"] = 12
		base["conversationId"] = conversation
		base["requestId"] = request
		base["messageId"] = request
		base["requestModelId"] = activityReportModelID
		base["requestModelName"] = activityReportModelName
	case "skill":
		base["eventCode"] = "skill_info"
		base["skillId"] = "skill_2096525080079265792"
		base["name"] = "pptx"
	case "automation":
		base["eventCode"] = "automated_task_create_suc"
		base["name"] = "weekly digest"
		base["type"] = "cron"
		base["source"] = "manually"
		base["modelId"] = activityReportModelID
		base["modelIsThinking"] = false
		base["conversationId"] = conversation
		base["requestId"] = request
		base["schedule"] = map[string]any{
			"type":  "recurring",
			"rrule": "FREQ=WEEKLY;BYDAY=FR;BYHOUR=9;BYMINUTE=0",
		}
		base["prompt"] = "summarise the week"
	case "playbook":
		base["eventCode"] = "playbook_prompt_send"
		base["id"] = "worker-ledger-freedom-dashboard"
		base["name"] = "ledger"
		base["type"] = "other"
		base["promptLength"] = 10
		base["isOfficial"] = 1
		base["source"] = "discover"
		base["conversationId"] = conversation
		base["requestId"] = request
	case "skin":
		base["eventCode"] = "appearance_skin_apply"
		base["action"] = "apply"
		base["source"] = "settings_close"
		base["id"] = "theme-tkmw7j"
		base["vipLevel"] = "free"
		base["series"] = "craft"
		base["type"] = "unknown"
		base["name"] = "default theme"
	case "chat", "glmchat", "cat":
		modelID, modelName, mode := activityReportModelID, activityReportModelName, "craft"
		if kind == "glmchat" || kind == "cat" {
			modelID, modelName = "glm-5.2", "GLM-5.2"
		}
		if kind == "cat" {
			mode = "night"
		}
		base["eventCode"] = "chat_request_send"
		base["mode"] = mode
		base["conversationId"] = conversation
		base["requestId"] = request
		base["inputLength"] = 12
		base["requestModelId"] = modelID
		base["requestModelName"] = modelName
		base["isPlan"] = false
		base["agentName"] = "default"
		base["agentType"] = "conversation"
	default:
		base["eventCode"] = "heartbeat"
	}
	if uid != "" {
		base["userId"] = uid
	}
	return base
}

// InNightWindow reports whether the local clock is inside the night-owl window
// the black_cat task only counts in (wb_tasks.py:79-81).
func InNightWindow(now time.Time) bool {
	hour := now.Hour()
	return hour >= nightWindowFrom || hour < nightWindowTo
}

// ErrGrowthUnsupported marks a growth call that does not apply to the account.
var ErrGrowthUnsupported = errors.New("workbuddy: growth centre is CN-only")

// TravelResult is one cat-travel handling.
type TravelResult struct {
	Action  string  `json:"action"`
	Credit  float64 `json:"credit"`
	Message string  `json:"message"`
}

// CatTravel inspects the buddy travel state and either claims a finished trip
// or sends the cat out again (wb_tasks.py:463-520).
func (c *Client) CatTravel(ctx context.Context, identity Identity) (*TravelResult, error) {
	headers := billingHeaders(identity)
	var status struct {
		State             string `json:"state"`
		DailyLimitReached bool   `json:"daily_limit_reached"`
	}
	if err := c.getJSON(ctx, c.chatBase()+TravelStatus, headers, &status); err != nil {
		return nil, fmt.Errorf("query travel status: %w", err)
	}
	switch status.State {
	case "arrived":
		var claim struct {
			RewardCredit numberValue `json:"reward_credit"`
		}
		if err := c.postJSON(ctx, c.chatBase()+TravelClaim, headers, map[string]any{}, &claim); err != nil {
			return nil, fmt.Errorf("claim travel reward: %w", err)
		}
		return &TravelResult{Action: "claim", Credit: claim.RewardCredit.float(), Message: "travel reward claimed"}, nil
	case "traveling":
		return &TravelResult{Action: "traveling", Message: "the cat is still travelling"}, nil
	case "idle":
		if status.DailyLimitReached {
			return &TravelResult{Action: "idle", Message: "already travelled today"}, nil
		}
		locationID := 1
		var config struct {
			Locations []struct {
				ID int `json:"id"`
			} `json:"locations"`
		}
		if err := c.getJSON(ctx, c.chatBase()+TravelConfig, headers, &config); err == nil && len(config.Locations) > 0 && config.Locations[0].ID != 0 {
			locationID = config.Locations[0].ID
		}
		_, err := c.callRawURL(ctx, "POST", c.chatBase()+TravelDepart, headers, map[string]any{"location_id": locationID})
		if err != nil {
			return &TravelResult{Action: "depart", Message: err.Error()}, nil
		}
		return &TravelResult{Action: "depart", Message: "the cat is on its way"}, nil
	}
	return &TravelResult{Action: status.State, Message: "state: " + status.State}, nil
}

// StreakBonusResult is one streak-housekeeping pass.
type StreakBonusResult struct {
	Logs []string `json:"logs"`
}

// StreakBonus runs the streak housekeeping chain: makeup a missed day, claim
// the starter gift and compensation, redeem the streak tiers and spend every
// lottery chance (wb_tasks.py:790-880). Every step is idempotent and silently
// skipped when it does not apply.
func (c *Client) StreakBonus(ctx context.Context, identity Identity) *StreakBonusResult {
	headers := billingHeaders(identity)
	result := &StreakBonusResult{}
	now := time.Now()

	// 1. Make up yesterday when the heatmap shows a gap and a card is available.
	var heatmap struct {
		Cells []struct {
			Date  string      `json:"date"`
			Score numberValue `json:"score"`
		} `json:"cells"`
	}
	if err := c.getJSON(ctx, c.chatBase()+HeatmapPath, headers, &heatmap); err == nil {
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		for _, cell := range heatmap.Cells {
			if cell.Date != yesterday || cell.Score.float() != 0 {
				continue
			}
			if streak, err := c.FetchStreak(ctx, identity); err == nil && streak.MakeupCards > 0 {
				if _, err := c.callRawURL(ctx, "POST", c.chatBase()+MakeupUse, headers, map[string]any{"target_date": yesterday}); err == nil {
					result.Logs = append(result.Logs, "made up "+yesterday+" with a makeup card")
				}
			}
			break
		}
	}

	// 2. Starter gift and campaign compensation: once per account, silent when
	//    already claimed.
	for _, item := range []struct{ path, name string }{
		{ClaimGift, "starter gift"},
		{ClaimComp, "campaign compensation"},
	} {
		var claim struct {
			Credit numberValue `json:"credit"`
		}
		if err := c.postJSON(ctx, c.billingBase()+item.path, headers, map[string]any{}, &claim); err == nil && claim.Credit.float() > 0 {
			result.Logs = append(result.Logs, fmt.Sprintf("claimed the %s: +%v", item.name, claim.Credit.float()))
		}
	}

	// 3. Streak tier redemption.
	if streak, err := c.FetchStreak(ctx, identity); err == nil {
		for _, tier := range streak.Tiers {
			status := streak.TierStatus[tier]
			if status == "locked" || status == "claimed" {
				continue
			}
			body := map[string]any{"tier": tier, "client_token": newUUID()}
			if _, err := c.callRawURL(ctx, "POST", c.chatBase()+StreakRedeem, headers, body); err == nil {
				result.Logs = append(result.Logs, "redeemed streak tier "+tier)
			}
		}
	}

	// 4. Lottery: spend every chance, one call each.
	var lottery struct {
		Chances int `json:"chances"`
	}
	if err := c.getJSON(ctx, c.chatBase()+LotteryQuery, headers, &lottery); err == nil {
		drawn := 0
		for i := 0; i < lottery.Chances && i < maxLotteryDraws; i++ {
			body := map[string]any{"client_token": newUUID()}
			if _, err := c.callRawURL(ctx, "POST", c.chatBase()+LotteryDraw, headers, body); err != nil {
				break
			}
			drawn++
		}
		if drawn > 0 {
			result.Logs = append(result.Logs, fmt.Sprintf("drew the growth lottery %d time(s)", drawn))
		}
	}
	return result
}

// maxLotteryDraws caps one pass so an absurd upstream chance count cannot spin
// forever.
const maxLotteryDraws = 20

// EnergyBalance reads the growth energy balance, which the panel shows next to
// the credits.
func (c *Client) EnergyBalance(ctx context.Context, identity Identity) (float64, error) {
	var data struct {
		Balance numberValue `json:"balance"`
	}
	if err := c.getJSON(ctx, c.chatBase()+EnergyPath, billingHeaders(identity), &data); err != nil {
		return 0, err
	}
	return data.Balance.float(), nil
}

// ExpertIDFor picks the expert or team id for the n-th event of a batch; the
// upstream deduplicates by (eventCode, id).
func ExpertIDFor(kind string, index int) string {
	pool := expertIDPool
	if strings.EqualFold(kind, "team") {
		pool = teamIDPool
	}
	if len(pool) == 0 {
		return ""
	}
	return pool[index%len(pool)]
}

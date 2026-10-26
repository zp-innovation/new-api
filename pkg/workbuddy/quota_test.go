package workbuddy

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// numPtr returns the address of a capacity figure. The pointer is what keeps an
// explicit 0 apart from a spelling the payload did not send.
func numPtr(value float64) *numberValue {
	number := numberValue(value)
	return &number
}

func TestNumberValueDecodesEverySpelling(t *testing.T) {
	var decoded struct {
		Number numberValue `json:"number"`
		Quoted numberValue `json:"quoted"`
		Null   numberValue `json:"null"`
		Junk   numberValue `json:"junk"`
	}
	payload := []byte(`{"number":12.5,"quoted":"30","null":null,"junk":"abc"}`)
	if err := common.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Number.float() != 12.5 || decoded.Quoted.float() != 30 {
		t.Fatalf("decoded = %+v", decoded)
	}
	if decoded.Null.float() != 0 || decoded.Junk.float() != 0 {
		t.Fatalf("null/junk = %+v", decoded)
	}
}

func TestParsePackageAccountDerivesMissingFigures(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	pkg := parsePackageAccount(resourceAccount{
		PackageName:              "Free pack",
		PackageCode:              "free",
		CycleCapacitySizePrecise: numPtr(100),
		CycleCapacityRemain:      numPtr(30),
		CycleEndTime:             "2026-11-01 00:00:00",
		AccountAttributes:        []accountAttribute{{Key: "grantReason", Value: "official activity"}},
	}, now)
	if pkg.Used != 70 {
		t.Fatalf("used = %v, want 70 (derived from size - remain)", pkg.Used)
	}
	if !pkg.InUsage {
		t.Fatalf("in usage must be inferred when credits are left and used")
	}
	if pkg.GrantReason != "official activity" {
		t.Fatalf("grant reason = %q", pkg.GrantReason)
	}
	if pkg.Unit != "credits" {
		t.Fatalf("unit = %q", pkg.Unit)
	}
	if pkg.DaysLeft == nil || *pkg.DaysLeft <= 0 {
		t.Fatalf("days left = %v", pkg.DaysLeft)
	}
}

func TestParsePackageAccountPrefersTheDeductionDeadline(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	deadline := float64(now.Add(10 * 24 * time.Hour).UnixMilli())
	pkg := parsePackageAccount(resourceAccount{
		PackageCode:      "bonus",
		CapacitySize:     numPtr(50),
		CapacityRemain:   numPtr(50),
		CycleEndTime:     "2034-01-01 00:00:00",
		DeductionEndTime: numberValue(deadline),
	}, now)
	if pkg.NoExpiry {
		t.Fatalf("the deduction deadline must win over the cycle end")
	}
	if pkg.DaysLeft == nil || *pkg.DaysLeft < 9 || *pkg.DaysLeft > 11 {
		t.Fatalf("days left = %v, want about 10", pkg.DaysLeft)
	}
}

func TestParsePackageAccountTreatsFarDeadlinesAsNoExpiry(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	pkg := parsePackageAccount(resourceAccount{
		PackageCode:    "free",
		CapacitySize:   numPtr(10),
		CapacityRemain: numPtr(10),
		CycleEndTime:   "2036-01-01 00:00:00",
	}, now)
	if !pkg.NoExpiry || pkg.DaysLeft != nil || pkg.IsExpired {
		t.Fatalf("far deadline = %+v", pkg)
	}
	expired := parsePackageAccount(resourceAccount{
		PackageCode:    "old",
		CapacitySize:   numPtr(10),
		CapacityRemain: numPtr(0),
		CycleEndTime:   "2026-10-01 00:00:00",
	}, now)
	if !expired.IsExpired || expired.DaysLeft == nil || *expired.DaysLeft >= 0 {
		t.Fatalf("expired = %+v", expired)
	}
}

func TestBuildCreditsTotalsAndOrdering(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	days := func(value float64) *float64 { return &value }
	packages := []CreditPackage{
		{Name: "expired", Remain: 0, Used: 10, Size: 10, IsExpired: true, DaysLeft: days(-3)},
		{Name: "later", Remain: 20, Used: 0, Size: 20, DaysLeft: days(20)},
		{Name: "in use", Remain: 30, Used: 70, Size: 100, InUsage: true, DaysLeft: days(5)},
	}
	credits := buildCredits(packages, packages, true, false, nil, now)
	if credits.Remain != 50 || credits.Used != 80 || credits.Size != 130 {
		t.Fatalf("totals = %+v", credits)
	}
	if credits.RemainPercent != "38.5%" || credits.UsedPercent != "61.5%" {
		t.Fatalf("percentages = %s / %s", credits.RemainPercent, credits.UsedPercent)
	}
	order := []string{credits.Packages[0].Name, credits.Packages[1].Name, credits.Packages[2].Name}
	if strings.Join(order, ",") != "in use,later,expired" {
		t.Fatalf("order = %v", order)
	}
	if credits.EarliestExpiring == nil || credits.EarliestExpiring.Name != "in use" {
		t.Fatalf("earliest expiring = %+v", credits.EarliestExpiring)
	}
	if !credits.IsPaidUser {
		t.Fatalf("paid user flag lost")
	}
}

func TestBuildCreditsWithoutSizeReportsTheFallbackPercentages(t *testing.T) {
	credits := buildCredits(nil, nil, false, false, nil, time.Now())
	if credits.UsedPercent != "0.0%" || credits.RemainPercent != "100.0%" {
		t.Fatalf("percentages = %s / %s", credits.UsedPercent, credits.RemainPercent)
	}
}

// newQuotaServer routes the meter endpoints and records the requests.
func newQuotaServer(t *testing.T, routes map[string]string) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		body, ok := routes[r.URL.Path]
		if !ok {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("%s: authorization = %q", r.URL.Path, got)
		}
		if got := r.Header.Get("X-User-Id"); got != "uid-1" {
			t.Errorf("%s: x-user-id = %q", r.URL.Path, got)
		}
		_, _ = w.Write([]byte(body))
	}))
	return server, &paths
}

func TestFetchCreditsCNDetailed(t *testing.T) {
	server, paths := newQuotaServer(t, map[string]string{
		ResourceSummaryPath:      `{"code":0,"data":{"IsPaidUser":true,"Packages":[{"PackageCode":"p1","CycleTotalCapacity":10}]}}`,
		ResourceFreePackagesPath: `{"code":0,"data":{"Accounts":[{"AccountId":"a1","PackageName":"Free","PackageCode":"p1","CycleCapacitySizePrecise":100,"CycleCapacityRemainPrecise":25,"CycleEndTime":"2026-11-01 00:00:00"}]}}`,
		ResourcePaidPackagesPath: `{"code":0,"data":{"Accounts":[{"AccountId":"a2","PackageName":"Paid","PackageCode":"p1","CycleCapacitySizePrecise":50,"CycleCapacityUsedPrecise":10,"CycleEndTime":"2026-12-01 00:00:00"}]}}`,
		CheckinStatusPath:        `{"code":0,"data":{"today_checked_in":true,"streak_days":4,"daily_credit":5,"today_credit":5}}`,
	})
	defer server.Close()

	client := NewClient(nil, server.URL, RealmCN, ProductCLI)
	client.BillingBaseURL = server.URL
	identity := Identity{Realm: RealmCN, Product: ProductCLI, UID: "uid-1", AccessToken: "access-token"}
	credits, err := client.FetchCredits(context.Background(), identity, time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchCredits: %v", err)
	}
	if len(credits.Packages) != 2 {
		t.Fatalf("packages = %+v", credits.Packages)
	}
	if credits.Size != 150 || credits.Remain != 65 || credits.Used != 85 {
		t.Fatalf("totals = remain %v used %v size %v", credits.Remain, credits.Used, credits.Size)
	}
	if !credits.IsPaidUser || credits.IsEnterprise {
		t.Fatalf("flags = paid %v enterprise %v", credits.IsPaidUser, credits.IsEnterprise)
	}
	if credits.Checkin == nil || !credits.Checkin.TodayCheckedIn || credits.Checkin.StreakDays != 4 {
		t.Fatalf("checkin = %+v", credits.Checkin)
	}
	want := []string{ResourceSummaryPath, ResourceFreePackagesPath, ResourcePaidPackagesPath, CheckinStatusPath}
	if strings.Join(*paths, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", *paths, want)
	}
}

func TestFetchCreditsFallsBackToTheBillingMeter(t *testing.T) {
	cases := []struct {
		name       string
		account    string
		wantRemain float64
		wantUsed   float64
		wantRenew  bool
	}{
		{
			name:       "a string account id",
			account:    `{"AccountId":"a1","PackageName":"Pro","PackageCode":"pro","CycleCapacitySize":200,"CycleCapacityRemain":120}`,
			wantRemain: 120,
			wantUsed:   80,
		},
		{
			// Captured verbatim from the international meter: the account id,
			// the renewal flags and an attribute type are numbers, so the plain
			// string/bool fields rejected the whole document with "cannot
			// unmarshal number into Go struct field ...AccountId" and the panel
			// could not show anything for an intl account.
			name:       "the live international payload",
			account:    `{"AccountId":6968468,"Uin":"450702222282","AppId":1499787792,"DealName":"20261003282014126031131","CapacityType":1,"CapacityUnit":"credit","CreateTime":1790989430000,"CycleStartTime":"2026-10-03 09:03:50","CycleEndTime":"2026-10-17 09:03:49","DeductionStartTime":1790989430000,"DeductionEndTime":1792199029000,"FeeType":1,"Region":"ap-others","Zone":"ap-others-4","OriginUnit":"credit","PackageCode":"TCACA_code_006_DbXS0lrypC","PackageName":"Bonus Pack","PackageType":"1","PkgSourceType":0,"ProductCode":"p_tcaca","ProductName":"Tencent Cloud AI Code Assistant","SubProductCode":"sp_tcaca_codebuddyide_bonus_pack","SubProductName":"Tencent Cloud CodeBuddy （IDE）","CapacityRemain":0,"CapacityUsed":250,"CapacitySize":250,"CycleCapacityRemain":0,"CycleCapacitySize":250,"CycleCapacityUsed":250,"RemainCycles":0,"TotalCycles":1,"ResourceCycleId":0,"ResourceId":"codebuddy-k7p00tgf9uc8rua","ResourceType":"2","Status":3,"Threshold":0,"SupportAutoRenew":0,"SupportManualRenew":0,"AutoRenewFlag":0,"ExpiredTime":"2026-10-05 05:38:50","DeductionProperties":[],"AutoRenewTimeUnit":"","AutoRenewTimeSpan":0,"RegionId":47,"ZoneId":470004,"CapacitySizePrecise":"250","CapacityRemainPrecise":"0","CapacityUsedPrecise":"250","CycleCapacitySizePrecise":"250","CycleCapacityRemainPrecise":"0","CycleCapacityUsedPrecise":"250","AccountAttributes":[{"Key":"payerType","Value":"0","Type":5},{"Key":"payerUin","Value":"450702222282","Type":5}],"BindRecords":[{"BindObjectId":"acc-yz5h6yy0","StartTime":"2026-10-03 09:03:50","EndTime":"9999-99-99 99:99:99","Properties":[]}]}`,
			wantRemain: 0,
			wantUsed:   250,
		},
		{
			name:       "numeric renewal flags are read",
			account:    `{"AccountId":6968468,"AutoRenewFlag":0,"SupportAutoRenew":1,"PackageName":"Pro","PackageCode":"pro","CycleCapacitySize":200,"CycleCapacityRemain":120}`,
			wantRemain: 120,
			wantUsed:   80,
			wantRenew:  true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var seenPlatform []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seenPlatform = append(seenPlatform, r.Header.Get("X-Client-Platform"))
				if r.URL.Path != GetResourcePath {
					t.Errorf("path = %s, want %s", r.URL.Path, GetResourcePath)
				}
				_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[` + testCase.account + `]}}}}`))
			}))
			defer server.Close()

			client := NewClient(nil, server.URL, RealmIntl, ProductDesktop)
			client.BillingBaseURL = server.URL
			identity := Identity{Realm: RealmIntl, Product: ProductDesktop, UID: "uid-1", AccessToken: "access-token"}
			credits, err := client.FetchCredits(context.Background(), identity, time.Now())
			if err != nil {
				t.Fatalf("FetchCredits: %v", err)
			}
			if len(credits.Packages) != 1 ||
				credits.Packages[0].Remain != testCase.wantRemain ||
				credits.Packages[0].Used != testCase.wantUsed {
				t.Fatalf("packages = %+v", credits.Packages)
			}
			if credits.Packages[0].AutoRenew != testCase.wantRenew {
				t.Fatalf("auto renew = %v, want %v", credits.Packages[0].AutoRenew, testCase.wantRenew)
			}
			// The international realm never calls the CN check-in endpoint and
			// the fallback does not send the web client marker.
			if len(seenPlatform) != 1 || seenPlatform[0] != "" {
				t.Fatalf("x-client-platform = %v, want a single request without it", seenPlatform)
			}
		})
	}
}

func TestFetchCreditsUsesTheEnterpriseAllowanceForTeamAccounts(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == ResourceSummaryPath {
			_, _ = w.Write([]byte(`{"code":0,"data":{"IsPaidUser":false,"Packages":[]}}`))
			return
		}
		if r.URL.Path == CheckinStatusPath {
			_, _ = w.Write([]byte(`{"code":0,"data":{"today_checked_in":false,"streak_days":0}}`))
			return
		}
		if r.URL.Path == EnterpriseUsagePath {
			_, _ = w.Write([]byte(`{"code":0,"data":{"credit":40,"limitNum":100,"cycleStartTime":"2026-10-01 00:00:00","cycleEndTime":"2026-11-01 00:00:00"}}`))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
	defer server.Close()

	client := NewClient(nil, server.URL, RealmCN, ProductCLI)
	client.BillingBaseURL = server.URL
	identity := Identity{Realm: RealmCN, Product: ProductCLI, UID: "uid-1", AccessToken: "access-token", EnterpriseID: "ent-1"}
	credits, err := client.FetchCredits(context.Background(), identity, time.Now())
	if err != nil {
		t.Fatalf("FetchCredits: %v", err)
	}
	if !credits.IsEnterprise {
		t.Fatalf("enterprise flag lost: %+v", credits)
	}
	if credits.Remain != 60 || credits.Used != 40 || credits.Size != 100 {
		t.Fatalf("totals = remain %v used %v size %v", credits.Remain, credits.Used, credits.Size)
	}
	if len(credits.Packages) != 1 || credits.Packages[0].PackageCode != enterprisePackageCode || !credits.Packages[0].NoExpiry {
		t.Fatalf("package = %+v", credits.Packages)
	}
	if strings.Join(paths, ",") != ResourceSummaryPath+","+CheckinStatusPath+","+EnterpriseUsagePath {
		t.Fatalf("calls = %v", paths)
	}
}

// The channel panel reads snake_case keys, so the wire format is pinned here.
func TestCreditsMarshalUsesSnakeCaseKeys(t *testing.T) {
	credits := buildCredits(
		[]CreditPackage{{Name: "p", Remain: 1, Used: 2, Size: 3, InUsage: true}},
		nil,
		false,
		false,
		nil,
		time.Now(),
	)
	payload, err := common.Marshal(credits)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := common.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"remain", "used", "size", "used_percent", "remain_percent",
		"is_paid_user", "is_enterprise", "packages", "updated_at",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("credits json is missing %q: %s", key, payload)
		}
	}

	packages, ok := decoded["packages"].([]any)
	if !ok || len(packages) != 1 {
		t.Fatalf("packages = %v", decoded["packages"])
	}
	first, ok := packages[0].(map[string]any)
	if !ok {
		t.Fatalf("package = %v", packages[0])
	}
	for _, key := range []string{
		"name", "remain", "used", "size", "in_usage", "auto_renew",
		"no_expiry", "is_expired", "status",
	} {
		if _, ok := first[key]; !ok {
			t.Fatalf("package json is missing %q: %s", key, payload)
		}
	}
}

// The panel renders `packages` directly, so an empty balance must still be a
// list rather than null.
func TestCreditsMarshalKeepsAnEmptyPackageList(t *testing.T) {
	payload, err := common.Marshal(buildCredits(nil, nil, false, false, nil, time.Now()))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := common.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	packages, ok := decoded["packages"].([]any)
	if !ok || len(packages) != 0 {
		t.Fatalf("packages = %v, want an empty array", decoded["packages"])
	}
}

// A CN meter that answers with an error is not a missing meter: substituting the
// billing fallback would report a different balance than the upstream answered.
func TestFetchCreditsPropagatesTheDetailErrorOnTheCNRealm(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != ResourceSummaryPath {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"code":503,"msg":"meter exploded"}`))
	}))
	defer server.Close()

	client := NewClient(nil, server.URL, RealmCN, ProductCLI)
	client.BillingBaseURL = server.URL
	identity := Identity{Realm: RealmCN, Product: ProductCLI, UID: "uid-1", AccessToken: "access-token"}

	_, err := client.FetchCredits(context.Background(), identity, time.Now())
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.HTTPStatus)
	assert.Equal(t, []string{ResourceSummaryPath}, paths, "a failing meter must not be masked by the billing fallback")
}

// A CN account whose meter endpoint does not exist still reads the billing
// fallback, which is the only path the international realm has.
func TestFetchCreditsFallsBackWhenTheCNMeterIsMissing(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case ResourceSummaryPath:
			w.WriteHeader(http.StatusNotFound)
		case GetResourcePath:
			_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"AccountId":"a1","PackageName":"Free pack","PackageCode":"p1","CapacitySizePrecise":100,"CapacityRemainPrecise":25,"CycleEndTime":"2099-01-01 00:00:00"}]}}}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(nil, server.URL, RealmCN, ProductCLI)
	client.BillingBaseURL = server.URL
	identity := Identity{Realm: RealmCN, Product: ProductCLI, UID: "uid-1", AccessToken: "access-token"}

	credits, err := client.FetchCredits(context.Background(), identity, time.Now())
	require.NoError(t, err)
	assert.InDelta(t, 25.0, credits.Remain, 0.001)
	assert.InDelta(t, 100.0, credits.Size, 0.001)
	assert.Equal(t, []string{ResourceSummaryPath, GetResourcePath}, paths)
}

// When the fallback fails too, the meter error stays in the returned error
// instead of being replaced by the fallback's.
func TestFetchCreditsJoinsTheFallbackFailureToTheDetailError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case ResourceSummaryPath:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"msg":"no such meter"}`))
		case GetResourcePath:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code":500,"msg":"billing down"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(nil, server.URL, RealmCN, ProductCLI)
	client.BillingBaseURL = server.URL
	identity := Identity{Realm: RealmCN, Product: ProductCLI, UID: "uid-1", AccessToken: "access-token"}

	_, err := client.FetchCredits(context.Background(), identity, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such meter")
	assert.Contains(t, err.Error(), "billing down")
}

// A capacity field is present when the payload carries it, not when it is
// non-zero: a spent package reports precise 0 while the rounded spelling may
// still hold a stale total, and the reference keeps the precise 0
// (wb_accounts.py:1519-1535).
func TestParsePackageAccountTreatsAPresentZeroAsAFigure(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		account    resourceAccount
		wantSize   float64
		wantRemain float64
		wantUsed   float64
	}{
		{
			name: "a present precise zero wins over the rounded figure",
			account: resourceAccount{
				PackageCode:           "spent",
				CapacitySizePrecise:   numPtr(0),
				CapacitySize:          numPtr(250),
				CapacityRemainPrecise: numPtr(0),
				CapacityRemain:        numPtr(250),
			},
			wantSize:   0,
			wantRemain: 0,
		},
		{
			name: "the rounded spelling is the fallback when precise is absent",
			account: resourceAccount{
				PackageCode:    "rounded",
				CapacitySize:   numPtr(250),
				CapacityRemain: numPtr(100),
			},
			wantSize:   250,
			wantRemain: 100,
			wantUsed:   150,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			pkg := parsePackageAccount(testCase.account, now)
			assert.Equal(t, testCase.wantSize, pkg.Size)
			assert.Equal(t, testCase.wantRemain, pkg.Remain)
			assert.Equal(t, testCase.wantUsed, pkg.Used)
		})
	}
}

// The fallback asks the meter for packages ending inside [now, now+10y]. The
// upper bound must follow the caller's clock: a fixed year would silently stop
// listing packages once their deadlines moved past it.
func TestFetchCreditsFallbackComputesTheWindowEndFromNow(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, GetResourcePath, r.URL.Path)
		var body map[string]any
		if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
			return
		}
		assert.Equal(t, now.Format("2006-01-02 15:04:05"), body["PackageEndTimeRangeBegin"])
		assert.Equal(t, now.AddDate(10, 0, 0).Format("2006-01-02 15:04:05"), body["PackageEndTimeRangeEnd"])
		_, _ = w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[]}}}}`))
	}))
	defer server.Close()

	client := NewClient(nil, server.URL, RealmIntl, ProductDesktop)
	client.BillingBaseURL = server.URL
	identity := Identity{Realm: RealmIntl, Product: ProductDesktop, UID: "uid-1", AccessToken: "access-token"}
	_, err := client.FetchCredits(context.Background(), identity, now)
	require.NoError(t, err)
}

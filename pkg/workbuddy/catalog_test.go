package workbuddy

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsChatModel(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"deepseek-v4.1-flash", true},
		{"deepseek-v4.1-flash-sg", true},
		{"gpt-6-astra", true},
		{"", false},
		{"auto", false},
		{"default-model", false},
		{"lite", false},
		{"codewise-summarize", false},
		{"completion-fast", false},
		{"hy4-preview-image-alpha", false},
		{"hy4-preview-image-alpha-edit", false},
		{"hy4-taco-completion", false},
	}
	for _, tc := range cases {
		if got := IsChatModel(tc.id); got != tc.want {
			t.Errorf("IsChatModel(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestTextValueDecodesStringsAndNumbers(t *testing.T) {
	var decoded struct {
		Quoted TextValue `json:"quoted"`
		Bare   TextValue `json:"bare"`
		Null   TextValue `json:"null"`
	}
	payload := []byte(`{"quoted":"x0.00","bare":12.5,"null":null}`)
	if err := common.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Quoted.String() != "x0.00" {
		t.Errorf("quoted = %q", decoded.Quoted)
	}
	if decoded.Bare.String() != "12.5" {
		t.Errorf("bare = %q", decoded.Bare)
	}
	if decoded.Null.String() != "" {
		t.Errorf("null = %q", decoded.Null)
	}
}

func TestParseCatalogPrefersTheLongestCLIList(t *testing.T) {
	payload := []byte(`{
	  "code": 0,
	  "data": {
	    "agents": {
	      "vscode": {"models": ["gpt-6-astra", "gpt-5.6-luna"]},
	      "cli": {"models": ["gpt-6-astra", "gpt-5.6-luna", "gemini-3.5-flash", "gpt-6-astra"]}
	    },
	    "models": [{"id": "gpt-6-astra", "credits": "x0.00", "maxOutputTokens": 32000}]
	  }
	}`)
	catalog, err := ParseCatalog(payload)
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}
	want := []string{"gpt-6-astra", "gpt-5.6-luna", "gemini-3.5-flash"}
	if strings.Join(catalog.IDs, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", catalog.IDs, want)
	}
	meta, ok := catalog.Models["gpt-6-astra"]
	if !ok {
		t.Fatalf("metadata for gpt-6-astra missing: %v", catalog.Models)
	}
	if meta.MaxOutputTokens != 32000 || meta.Credits.String() != "x0.00" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestParseCatalogReadsTheCacheFileShape(t *testing.T) {
	// The desktop cache file is the same document without the data envelope.
	payload := []byte(`{"agents":{"cli":{"models":["hy3","glm-5.3"]}}}`)
	catalog, err := ParseCatalog(payload)
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}
	if strings.Join(catalog.IDs, ",") != "hy3,glm-5.3" {
		t.Fatalf("ids = %v", catalog.IDs)
	}
}

func TestParseCatalogRejectsAnEmptyCatalogue(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		empty   bool
	}{
		{"no agents", []byte(`{"code":0,"data":{}}`), true},
		{"empty list", []byte(`{"agents":{"cli":{"models":[]}}}`), true},
		{"blank", nil, true},
		{"broken", []byte(`not json`), false},
	}
	for _, tc := range cases {
		_, err := ParseCatalog(tc.payload)
		if err == nil {
			t.Errorf("%s: err = nil, want an error", tc.name)
			continue
		}
		if tc.empty && !errors.Is(err, ErrEmptyCatalog) {
			t.Errorf("%s: err = %v, want ErrEmptyCatalog", tc.name, err)
		}
		if !tc.empty && errors.Is(err, ErrEmptyCatalog) {
			t.Errorf("%s: err = %v, want a decode error", tc.name, err)
		}
	}
}

func TestCurateCatalogHidesPaidTwinsOnly(t *testing.T) {
	ids := []string{
		"hy4-preview", "hy4-preview-f", "hy3", "hy3-x", "deepseek-v4.1-flash",
		"deepseek-v4.1-flash-sg", "auto", "codewise-summarize", "gpt-6-astra",
	}
	models := map[string]CatalogModel{
		"hy4-preview":            {ID: "hy4-preview", Credits: "x0.00"},
		"hy4-preview-f":          {ID: "hy4-preview-f", Credits: "1.5"},
		"hy3":                    {ID: "hy3", Credits: "0"},
		"hy3-x":                  {ID: "hy3-x", Credits: "2"},
		"deepseek-v4.1-flash":    {ID: "deepseek-v4.1-flash", Credits: "3"},
		"deepseek-v4.1-flash-sg": {ID: "deepseek-v4.1-flash-sg", Credits: "3.5"},
		"gpt-6-astra":            {ID: "gpt-6-astra", Credits: "0.00"},
	}
	got := CurateCatalog(ids, models)
	want := "hy4-preview,hy3,deepseek-v4.1-flash,deepseek-v4.1-flash-sg,gpt-6-astra"
	if strings.Join(got, ",") != want {
		t.Fatalf("curated = %v, want %v", got, want)
	}
}

func TestCurateCatalogKeepsUnpricedModels(t *testing.T) {
	// Without the bundled snapshot an unknown price cannot mark a twin as paid,
	// so the model stays listed rather than silently disappearing.
	got := CurateCatalog([]string{"hy4-preview", "hy4-preview-f"}, map[string]CatalogModel{
		"hy4-preview": {ID: "hy4-preview"},
	})
	if strings.Join(got, ",") != "hy4-preview,hy4-preview-f" {
		t.Fatalf("curated = %v", got)
	}
}

func TestFetchCatalogRetriesWithTheAppUserAgent(t *testing.T) {
	const catalogBody = `{"code":0,"data":{"agents":{"cli":{"models":["gpt-6-astra","hy3"]}}}}`
	var agents []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ConfigPath {
			t.Errorf("path = %s, want %s", r.URL.Path, ConfigPath)
		}
		agents = append(agents, r.Header.Get("User-Agent"))
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("X-User-Id"); got != "uid-1" {
			t.Errorf("x-user-id = %q", got)
		}
		if got := r.Header.Get("X-Domain"); got != "" {
			t.Errorf("x-domain must not be sent, got %q", got)
		}
		if r.Header.Get("User-Agent") == RealmIntl.Config().ChatUA {
			// The WAF answers the unknown build with a business error.
			_, _ = w.Write([]byte(`{"code":12403,"msg":"invalid client"}`))
			return
		}
		_, _ = w.Write([]byte(catalogBody))
	}))
	defer server.Close()

	client := NewClient(nil, server.URL, RealmIntl, ProductDesktop)
	identity := Identity{Realm: RealmIntl, Product: ProductDesktop, AccessToken: "access-token", UID: "uid-1"}
	catalog, err := client.FetchCatalog(context.Background(), identity)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if strings.Join(catalog.IDs, ",") != "gpt-6-astra,hy3" {
		t.Fatalf("ids = %v", catalog.IDs)
	}
	if len(agents) != 2 {
		t.Fatalf("attempts = %v, want two user agents", agents)
	}
	if agents[1] != RealmIntl.Config().BillingUA {
		t.Fatalf("second attempt used %q", agents[1])
	}
}

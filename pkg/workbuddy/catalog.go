package workbuddy

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"net/http"
	"sort"
	"strings"
)

// ErrEmptyCatalog reports a catalogue answer that carried no model ids. The
// upstream answers HTTP 200 with an empty list when the credential is not
// usable, so an empty catalogue is an error and never "this exit has no
// models" (wb_proxy.py:7044-7085).
var ErrEmptyCatalog = errors.New("workbuddy: the catalogue response carried no models")

// virtualAliasModels are picker entries that are not models a client can pin
// (wb_proxy.py:6615-6625). "lite" rides with them because the upstream lists it
// as a completion variant.
var virtualAliasModels = map[string]bool{
	"default-model":  true,
	"fast-model":     true,
	"balanced-model": true,
	"primary-model":  true,
	"deep-model":     true,
	"auto":           true,
	"lite":           true,
}

var nonChatPrefixes = []string{"codewise-", "completion-"}

var nonChatSuffixes = []string{"-image-alpha", "-image-alpha-edit", "-taco-completion"}

// IsChatModel reports whether an upstream id names a chat model. The picker
// hides the virtual aliases, the completion and image helpers
// (wb_proxy.py:6625-6637).
func IsChatModel(id string) bool {
	if id == "" || virtualAliasModels[id] {
		return false
	}
	for _, prefix := range nonChatPrefixes {
		if strings.HasPrefix(id, prefix) {
			return false
		}
	}
	for _, suffix := range nonChatSuffixes {
		if strings.HasSuffix(id, suffix) {
			return false
		}
	}
	return true
}

// TextValue decodes a JSON value the upstream spells either as a string or as a
// number; the catalogue's credits field is both across builds. Anything
// unparsable reads as empty rather than failing the whole document.
type TextValue string

// UnmarshalJSON implements json.Unmarshaler.
func (t *TextValue) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*t = ""
		return nil
	}
	if strings.HasPrefix(text, `"`) {
		var decoded string
		if err := common.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*t = TextValue(decoded)
		return nil
	}
	*t = TextValue(strings.Trim(text, `"`))
	return nil
}

// String returns the decoded value.
func (t TextValue) String() string { return string(t) }

// CatalogModel is one entry of the catalogue's per-model metadata array. The
// fields mirror what the reference gateway reads (wb_proxy.py:7069-7084).
type CatalogModel struct {
	ID              string    `json:"id"`
	DisplayName     string    `json:"displayName"`
	Description     string    `json:"description"`
	Credits         TextValue `json:"credits"`
	MaxTokens       int       `json:"maxTokens"`
	MaxOutputTokens int       `json:"maxOutputTokens"`
	DefaultEffort   string    `json:"defaultEffort"`
	Recommended     bool      `json:"recommended"`
}

// Catalog is the model list a realm advertises plus the metadata that came with
// it. The bundled snapshot the reference gateway keeps is deliberately not
// embedded here: the plan (section 10.1) reads the live catalogue only, at the
// cost of the snapshot's maxOutputTokens and defaultEffort fallbacks.
type Catalog struct {
	IDs    []string
	Models map[string]CatalogModel
}

// Curated returns the ids the picker should advertise.
func (c *Catalog) Curated() []string {
	if c == nil {
		return nil
	}
	return CurateCatalog(c.IDs, c.Models)
}

// ParseCatalog reads a /v3/config answer. The endpoint returns the catalogue
// under "data" while the desktop cache file is the same document without that
// envelope, so both roots are read (wb_proxy.py:7008-7085).
func ParseCatalog(payload []byte) (*Catalog, error) {
	if len(payload) == 0 {
		return nil, ErrEmptyCatalog
	}
	var root map[string]any
	if err := common.Unmarshal(payload, &root); err != nil {
		return nil, fmt.Errorf("workbuddy: decode catalogue: %w", err)
	}
	cli, other := agentModelLists(root)
	best := longestList(cli)
	if len(best) == 0 {
		best = longestList(other)
	}
	ids := dedupeStrings(best)
	if len(ids) == 0 {
		return nil, ErrEmptyCatalog
	}
	models := map[string]CatalogModel{}
	collectCatalogMeta(root, models)
	return &Catalog{IDs: ids, Models: models}, nil
}

// agentModelLists returns every agent's bare-string model list, cli-named
// agents separated from the rest.
func agentModelLists(payload map[string]any) (cli, other [][]string) {
	roots := []map[string]any{payload}
	if data, ok := payload["data"].(map[string]any); ok {
		roots = append(roots, data)
	}
	for _, root := range roots {
		entries := agentEntries(root["agents"])
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		// A mapping's order is not preserved by encoding/json, so the tie
		// between two equally long lists is broken by name instead.
		sort.Strings(names)
		for _, name := range names {
			modelList, ok := entries[name]["models"].([]any)
			if !ok || len(modelList) == 0 {
				continue
			}
			if _, ok := modelList[0].(string); !ok {
				continue
			}
			ids := make([]string, 0, len(modelList))
			for _, raw := range modelList {
				text, ok := raw.(string)
				if !ok {
					continue
				}
				if trimmed := strings.TrimSpace(text); trimmed != "" {
					ids = append(ids, trimmed)
				}
			}
			if len(ids) == 0 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(name), "cli") {
				cli = append(cli, ids)
			} else {
				other = append(other, ids)
			}
		}
	}
	return cli, other
}

// agentEntries normalises the agents member: a mapping of name to agent, or a
// list of agents that name themselves.
func agentEntries(raw any) map[string]map[string]any {
	entries := map[string]map[string]any{}
	switch typed := raw.(type) {
	case map[string]any:
		for name, value := range typed {
			if entry, ok := value.(map[string]any); ok {
				entries[name] = entry
			}
		}
	case []any:
		for index, value := range typed {
			entry, ok := value.(map[string]any)
			if !ok {
				continue
			}
			name, _ := entry["name"].(string)
			if strings.TrimSpace(name) == "" {
				name = fmt.Sprintf("#%d", index)
			}
			entries[name] = entry
		}
	}
	return entries
}

// longestList returns the longest list, first one winning a tie.
func longestList(lists [][]string) []string {
	var best []string
	for _, list := range lists {
		if len(list) > len(best) {
			best = list
		}
	}
	return best
}

// dedupeStrings keeps the first occurrence of every id and preserves order.
func dedupeStrings(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// collectCatalogMeta gathers the per-model metadata array from anywhere in the
// document, first entry winning.
func collectCatalogMeta(node any, models map[string]CatalogModel) {
	switch typed := node.(type) {
	case map[string]any:
		if list, ok := typed["models"].([]any); ok && len(list) > 0 {
			if _, ok := list[0].(map[string]any); ok {
				for _, raw := range list {
					item, ok := raw.(map[string]any)
					if !ok {
						continue
					}
					id, _ := item["id"].(string)
					id = strings.TrimSpace(id)
					if id == "" {
						continue
					}
					if _, exists := models[id]; exists {
						continue
					}
					encoded, err := common.Marshal(item)
					if err != nil {
						continue
					}
					var model CatalogModel
					if err := common.Unmarshal(encoded, &model); err != nil {
						continue
					}
					models[id] = model
				}
			}
		}
		for _, value := range typed {
			collectCatalogMeta(value, models)
		}
	case []any:
		for _, value := range typed {
			collectCatalogMeta(value, models)
		}
	}
}

// CurateCatalog trims a catalogue to the models the picker should offer: the
// virtual aliases and non-chat ids go, and so does a paid "-f"/"-x" twin of a
// model the same document offers for free. The regional "-sg" build is
// deliberately kept: it is separately priced and has no free twin
// (wb_proxy.py:7105-7149).
func CurateCatalog(ids []string, models map[string]CatalogModel) []string {
	credits := make(map[string]string, len(models))
	for id, model := range models {
		credits[id] = strings.ToLower(strings.TrimSpace(model.Credits.String()))
	}
	known := make(map[string]bool, len(ids)+len(credits))
	for _, id := range ids {
		known[id] = true
	}
	for id := range credits {
		known[id] = true
	}
	free := func(id string) bool {
		switch credits[id] {
		case "x0.00", "x0", "0", "0.00":
			return true
		default:
			return false
		}
	}
	paidTwinOfFree := func(id string) bool {
		if free(id) {
			return false
		}
		for _, suffix := range []string{"-f", "-x"} {
			if strings.HasSuffix(id, suffix) && free(strings.TrimSuffix(id, suffix)) {
				return true
			}
		}
		return known[id+"-f"] && free(id+"-f")
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !IsChatModel(id) || paidTwinOfFree(id) {
			continue
		}
		out = append(out, id)
	}
	return out
}

// FetchCatalog reads the realm's product-config endpoint. The User-Agent
// decides which catalogue comes back and only the desktop UA returns the full
// list (an unknown one is a hard 400 with code 12403), so the realm's chat UA
// is tried first and the plain app UA second (wb_proxy.py:6990-7005,
// :7152-7188).
func (c *Client) FetchCatalog(ctx context.Context, identity Identity) (*Catalog, error) {
	realm := NormalizeRealm(string(identity.Realm))
	if identity.Realm == "" {
		realm = c.Realm
	}
	cfg := realm.Config()
	agents := []string{cfg.ChatUA}
	if cfg.BillingUA != "" && cfg.BillingUA != cfg.ChatUA {
		agents = append(agents, cfg.BillingUA)
	}
	var lastErr error
	for _, ua := range agents {
		payload, err := c.callRaw(ctx, http.MethodGet, ConfigPath, catalogHeaders(identity, realm, ua), nil)
		if err != nil {
			lastErr = err
			continue
		}
		catalog, err := ParseCatalog(payload)
		if err == nil {
			return catalog, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrEmptyCatalog
	}
	return nil, lastErr
}

// catalogHeaders is the product-config header set: the management headers plus
// the account's bearer token and user id, and without X-Domain
// (wb_proxy.py:6990-7005).
func catalogHeaders(identity Identity, realm Realm, ua string) map[string]string {
	cfg := NormalizeRealm(string(realm)).Config()
	return map[string]string{
		"Accept":        "application/json, text/plain, */*",
		"User-Agent":    ua,
		"Origin":        cfg.Origin,
		"Referer":       cfg.Origin + "/",
		"Authorization": "Bearer " + identity.AccessToken,
		"X-User-Id":     identity.UID,
	}
}

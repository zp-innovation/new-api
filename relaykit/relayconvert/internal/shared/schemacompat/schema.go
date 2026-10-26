// Package schemacompat rewrites JSON Schema constructs that upstream
// validators reject even though the JSON Schema dialect allows them.
package schemacompat

import "reflect"

// MaxDepth bounds recursion into nested schemas. Tool schemas are shallow in
// practice; the bound only keeps a pathological input from recursing without
// end.
const MaxDepth = 64

// ForUpstream returns a copy of schema with the union and intersection
// combinators rewritten, and reports whether anything changed.
//
// A nullable union, `anyOf: [<schema>, {"type": "null"}]`, always becomes the
// equivalent `type: [<type>, "null"]`. With flattenCombinations set, every
// other anyOf, oneOf, and allOf is replaced by one object schema: the branches'
// properties are unioned, a required list the branches disagree on is dropped,
// and the constraints the branches agree on are kept. A union of declared types
// or of enum/const values is represented as that union. The result is still a
// widening, because a constraint only one branch carries cannot survive without
// the combinator, but it accepts everything the original union accepted. The
// upstreams that need this reject the combinator outright, so the alternative
// is a failed request.
func ForUpstream(schema any, flattenCombinations bool) (any, bool) {
	changed := false
	rewritten := rewrite(schema, 0, flattenCombinations, &changed)
	return rewritten, changed
}

func rewrite(node any, depth int, flatten bool, changed *bool) any {
	switch typed := node.(type) {
	case map[string]any:
		return rewriteObject(typed, depth, flatten, changed)
	case []any:
		rewritten := make([]any, len(typed))
		for index, item := range typed {
			rewritten[index] = rewrite(item, depth, flatten, changed)
		}
		return rewritten
	default:
		return node
	}
}

func rewriteObject(schema map[string]any, depth int, flatten bool, changed *bool) map[string]any {
	rewritten := make(map[string]any, len(schema))
	for key, value := range schema {
		rewritten[key] = value
	}
	if depth >= MaxDepth {
		return rewritten
	}
	if collapsed, ok := collapseNullableAnyOf(rewritten); ok {
		delete(rewritten, "anyOf")
		for key, value := range collapsed {
			rewritten[key] = value
		}
		*changed = true
	}
	if flatten {
		for _, key := range []string{"anyOf", "oneOf", "allOf"} {
			branches, isList := rewritten[key].([]any)
			if !isList {
				continue
			}
			delete(rewritten, key)
			mergeBranches(rewritten, branches)
			*changed = true
		}
	}
	for key, value := range rewritten {
		switch key {
		case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas":
			container, isMap := value.(map[string]any)
			if !isMap {
				continue
			}
			normalized := make(map[string]any, len(container))
			for name, child := range container {
				normalized[name] = rewrite(child, depth+1, flatten, changed)
			}
			rewritten[key] = normalized
		case "items", "prefixItems", "anyOf", "oneOf", "allOf", "additionalProperties",
			"not", "if", "then", "else", "contains", "propertyNames":
			rewritten[key] = rewrite(value, depth+1, flatten, changed)
		}
	}
	return rewritten
}

// mergeBranches folds a combinator's branches into schema as one object. The
// callers only ask for this when the upstream rejects the combinator itself, so
// the result is a deliberate widening: the branches' properties are unioned, the
// object type is restored when every branch described one, and a required list
// the branches disagree on is dropped. A constraint the branches agree on is
// kept, and their declared types and enum/const values are unioned, so the
// rewritten schema accepts everything the union accepted without forgetting the
// constraints the union expressed.
func mergeBranches(schema map[string]any, branches []any) {
	merged := make(map[string]any)
	branchSchemas := make([]map[string]any, 0, len(branches))
	allObjects := len(branches) > 0
	for _, branch := range branches {
		branchSchema, isMap := branch.(map[string]any)
		if !isMap {
			allObjects = false
			continue
		}
		branchSchemas = append(branchSchemas, branchSchema)
		properties, hasProperties := branchSchema["properties"].(map[string]any)
		if !hasProperties {
			allObjects = false
			continue
		}
		for name, value := range properties {
			if _, exists := merged[name]; !exists {
				merged[name] = value
			}
		}
	}
	if existing, isMap := schema["properties"].(map[string]any); isMap {
		for name, value := range existing {
			merged[name] = value
		}
	}
	if len(merged) > 0 {
		schema["properties"] = merged
	}
	if allObjects {
		if _, exists := schema["type"]; !exists {
			schema["type"] = "object"
		}
	}
	if len(branchSchemas) != len(branches) {
		return
	}
	keepAgreedKeywords(schema, branchSchemas)
	mergeBranchTypes(schema, branchSchemas)
	mergeBranchEnums(schema, branchSchemas)
}

// keepAgreedKeywords copies a keyword into schema when every branch carries it
// with the same value and schema does not. Any instance satisfying one branch
// satisfies that keyword too, so keeping it preserves a constraint the union
// really imposed, such as a shared required list.
func keepAgreedKeywords(schema map[string]any, branches []map[string]any) {
	for key, first := range branches[0] {
		if key == "properties" {
			// The properties were already unioned above.
			continue
		}
		if _, exists := schema[key]; exists {
			continue
		}
		agreed := true
		for _, branch := range branches[1:] {
			value, exists := branch[key]
			if !exists || !reflect.DeepEqual(value, first) {
				agreed = false
				break
			}
		}
		if agreed {
			schema[key] = first
		}
	}
}

// mergeBranchTypes records the union of the types the branches declare when
// they declare different ones, so a `string`-or-`number` union keeps its
// type instead of losing it with the combinator.
func mergeBranchTypes(schema map[string]any, branches []map[string]any) {
	if _, exists := schema["type"]; exists {
		return
	}
	var union []any
	seen := make(map[string]bool, len(branches))
	for _, branch := range branches {
		raw, exists := branch["type"]
		if !exists {
			return
		}
		names, ok := typeNames(raw)
		if !ok {
			return
		}
		for _, name := range names {
			if !seen[name] {
				seen[name] = true
				union = append(union, name)
			}
		}
	}
	if len(union) > 1 {
		schema["type"] = union
	}
}

// typeNames returns the type names a `type` value declares. JSON Schema allows
// a string or a list of strings there.
func typeNames(raw any) ([]string, bool) {
	switch typed := raw.(type) {
	case string:
		if typed == "" {
			return nil, false
		}
		return []string{typed}, true
	case []any:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			name, ok := item.(string)
			if !ok || name == "" {
				return nil, false
			}
			names = append(names, name)
		}
		return names, len(names) > 0
	default:
		return nil, false
	}
}

// mergeBranchEnums unions the values the branches allow through enum or const.
// The union is what the original union allowed, so those values survive the
// rewrite instead of turning into an unconstrained keyword.
func mergeBranchEnums(schema map[string]any, branches []map[string]any) {
	if _, exists := schema["enum"]; exists {
		return
	}
	if _, exists := schema["const"]; exists {
		return
	}
	union := make([]any, 0, len(branches))
	for _, branch := range branches {
		values, ok := allowedValues(branch)
		if !ok {
			return
		}
		for _, value := range values {
			if !containsValue(union, value) {
				union = append(union, value)
			}
		}
	}
	if len(union) > 0 {
		schema["enum"] = union
	}
}

// allowedValues returns the values a branch allows when it constrains them with
// enum or const, which describe the same set for a single value.
func allowedValues(branch map[string]any) ([]any, bool) {
	if values, ok := branch["enum"].([]any); ok {
		return values, len(values) > 0
	}
	value, exists := branch["const"]
	if !exists {
		return nil, false
	}
	return []any{value}, true
}

func containsValue(values []any, value any) bool {
	for _, candidate := range values {
		if reflect.DeepEqual(candidate, value) {
			return true
		}
	}
	return false
}

// collapseNullableAnyOf reports whether schema is the nullable union
// `anyOf: [<typed schema>, {"type": "null"}]` and returns the keywords that
// replace it: the typed branch's keywords with its type widened to accept
// null. Any other union is left alone.
func collapseNullableAnyOf(schema map[string]any) (map[string]any, bool) {
	variants, isList := schema["anyOf"].([]any)
	if !isList || len(variants) != 2 {
		return nil, false
	}
	var branch map[string]any
	nullable := false
	for _, variant := range variants {
		variantSchema, isMap := variant.(map[string]any)
		if !isMap {
			return nil, false
		}
		if variantSchema["type"] == "null" {
			nullable = true
			continue
		}
		if branch != nil {
			return nil, false
		}
		branch = variantSchema
	}
	if !nullable || branch == nil {
		return nil, false
	}
	branchType, isString := branch["type"].(string)
	if !isString || branchType == "" {
		return nil, false
	}
	if existing, exists := schema["type"]; exists && existing != branchType {
		return nil, false
	}
	collapsed := make(map[string]any, len(branch))
	for key, value := range branch {
		if key == "type" {
			continue
		}
		if _, exists := schema[key]; exists {
			return nil, false
		}
		collapsed[key] = value
	}
	collapsed["type"] = []any{branchType, "null"}
	return collapsed, true
}

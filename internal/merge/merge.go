// Package merge implements Clash/mihomo YAML config composition: deep-merge a
// base mapping with zero or more overlay mappings, honoring the prepend-/
// append- directive keys (Clash Verge merge convention).
//
// This is a direct port of packages/agent/src/merge.ts, minus the visual-editor
// patch path — the visual editor is intentionally cut from this build
// (GO_SERVER_PLAN.md §2 砍项), so overlays carrying the
// `x-metacubexd-visual-patch` key have that key stripped before merging. The
// ordinary fields of such overlays still deep-merge (per §8 风险与缓解), so
// migrated visual-editor overlays behave like normal merge profiles.
package merge

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// VisualPatchKey is the reserved top-level key that carries a visual-editor
// patch in an overlay. Cut from this build; stripped before merge.
const VisualPatchKey = "x-metacubexd-visual-patch"

// directive pairs a prepend-/append- key with its target array + position.
type directive struct {
	target string
	at     position
}

type position int

const (
	posPrepend position = iota
	posAppend
)

// DIRECTIVES maps Clash Verge merge convention keys to their target arrays.
// These keys are NEVER emitted to the merged output — they only splice into
// the corresponding base array.
var directives = map[string]directive{
	"prepend-rules":        {target: "rules", at: posPrepend},
	"append-rules":         {target: "rules", at: posAppend},
	"prepend-proxies":      {target: "proxies", at: posPrepend},
	"append-proxies":       {target: "proxies", at: posAppend},
	"prepend-proxy-groups": {target: "proxy-groups", at: posPrepend},
	"append-proxy-groups":  {target: "proxy-groups", at: posAppend},
}

// IsPlainObject reports whether v is a mapping (yaml node with kind MappingNode,
// or a Go map[string]any). Used throughout the merge to decide whether to
// recurse or replace.
func IsPlainObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

// MergeConfigs deep-merges a base Clash/mihomo YAML config with zero or more
// YAML overlays, returning the composed YAML string.
//
// Merge semantics (matches the TS mergeConfigs):
//   - Both base and every overlay MUST parse to a top-level YAML mapping.
//   - Plain-object values recurse.
//   - Scalars and plain (non-directive) arrays REPLACE the base value wholesale.
//   - Directive keys (prepend-/append-) splice into the named base array.
//   - The `x-metacubexd-visual-patch` key is STRIPPED (visual editor cut).
//
// Pure function, no IO.
func MergeConfigs(base string, overlays []string) (string, error) {
	parsedBase, err := parseMapping(base)
	if err != nil {
		return "", fmt.Errorf("mergeConfigs: base config: %w", err)
	}
	acc := parsedBase

	for i, overlay := range overlays {
		parsed, err := parseMapping(overlay)
		if err != nil {
			return "", fmt.Errorf("mergeConfigs: overlay #%d: %w", i, err)
		}
		mergeOne(acc, parsed)
	}

	out, err := yaml.Marshal(acc)
	if err != nil {
		return "", fmt.Errorf("mergeConfigs: marshal: %w", err)
	}
	return string(out), nil
}

// parseMapping parses a YAML document and asserts it's a top-level mapping.
// Returns map[string]any (the shape yaml.v3 produces for mappings).
func parseMapping(s string) (map[string]any, error) {
	if s == "" {
		// An empty overlay is a no-op mapping (matches the TS behavior where
		// yaml.parse('') === null and the guard rejects it; we instead treat
		// empty as an empty mapping to keep the merge composable).
		return map[string]any{}, nil
	}
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be a YAML mapping, got %T", v)
	}
	return m, nil
}

// mergeOne mutates acc by deep-merging overlay onto it. Both must be non-nil
// mappings. Directive keys splice into base arrays; the visual-patch key is
// dropped; everything else follows the object-recurse / else-replace rule.
func mergeOne(acc, overlay map[string]any) {
	for k, val := range overlay {
		// Strip the visual-patch key: this build doesn't apply visual-editor
		// patches. Per GO_SERVER_PLAN.md §8 we still apply the overlay's other
		// fields as ordinary merge, so we don't abort the whole compose.
		if k == VisualPatchKey {
			continue
		}

		if d, ok := directives[k]; ok {
			baseArr := asArray(acc[d.target])
			extra := asArray(val)
			if d.at == posPrepend {
				acc[d.target] = concat(extra, baseArr)
			} else {
				acc[d.target] = concat(baseArr, extra)
			}
			continue
		}

		existing, present := acc[k]
		if present && IsPlainObject(existing) && IsPlainObject(val) {
			// Recurse: both are mappings, deep-merge them.
			mergeOne(existing.(map[string]any), val.(map[string]any))
		} else {
			// Scalars and arrays replace wholesale (matches TS semantics).
			acc[k] = val
		}
	}
}

// asArray coerces v to a []any; non-arrays yield an empty slice (matches the
// TS asArray helper).
func asArray(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

// concat returns a new slice with the elements of a followed by b. We avoid
// append() to keep the original arrays' backing slices untouched — mihomo may
// hold references to parsed substructures and a stray append could mutate them.
func concat(a, b []any) []any {
	out := make([]any, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}

package merge

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parseYAML re-parses merged output into a map for assertion.
func parseYAML(t *testing.T, s string) map[string]any {
	t.Helper()
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("parse merged output: %v\n%s", err, s)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("output not a mapping: %T\n%s", v, s)
	}
	return m
}

func mergeAndParse(t *testing.T, base string, overlays ...string) map[string]any {
	t.Helper()
	out, err := MergeConfigs(base, overlays)
	if err != nil {
		t.Fatalf("MergeConfigs: %v", err)
	}
	return parseYAML(t, out)
}

func TestRejectsNonMappingBase(t *testing.T) {
	if _, err := MergeConfigs("- a\n- b\n", nil); err == nil {
		t.Fatal("expected error for array base")
	}
}

func TestRejectsNonMappingOverlay(t *testing.T) {
	if _, err := MergeConfigs("a: 1\n", []string{"- x\n"}); err == nil {
		t.Fatal("expected error for array overlay")
	}
}

func TestEmptyOverlayIsNoop(t *testing.T) {
	out := mergeAndParse(t, "a: 1\nb: 2\n", "")
	if out["a"] != 1 || out["b"] != 2 {
		t.Errorf("got %v", out)
	}
}

func TestNoOverlays(t *testing.T) {
	out := mergeAndParse(t, "mixed-port: 7890\n")
	if out["mixed-port"] != 7890 {
		t.Errorf("got %v", out["mixed-port"])
	}
}

func TestScalarOverwritesScalar(t *testing.T) {
	out := mergeAndParse(t, "mixed-port: 7890\n", "mixed-port: 7891\n")
	if out["mixed-port"] != 7891 {
		t.Errorf("got %v", out["mixed-port"])
	}
}

func TestArrayReplacesArray(t *testing.T) {
	out := mergeAndParse(t, "rules:\n  - a\n  - b\n", "rules:\n  - c\n")
	rules := out["rules"].([]any)
	if len(rules) != 1 || rules[0] != "c" {
		t.Errorf("got %v, want [c]", rules)
	}
}

func TestNestedMapRecurses(t *testing.T) {
	base := "dns:\n  enable: true\n  listen: 0.0.0.0:1053\n"
	overlay := "dns:\n  listen: 127.0.0.1:1053\n  ipv6: false\n"
	out := mergeAndParse(t, base, overlay)
	dns := out["dns"].(map[string]any)
	if dns["enable"] != true {
		t.Error("nested key not preserved")
	}
	if dns["listen"] != "127.0.0.1:1053" {
		t.Error("nested key not overridden")
	}
	if dns["ipv6"] != false {
		t.Error("nested key not added")
	}
}

func TestNestedMapReplacesScalar(t *testing.T) {
	out := mergeAndParse(t, "dns: 1.1.1.1\n", "dns:\n  enable: true\n")
	dns, ok := out["dns"].(map[string]any)
	if !ok {
		t.Fatalf("dns = %T, want map", out["dns"])
	}
	if dns["enable"] != true {
		t.Error("expected dns.enable = true")
	}
}

func TestPrependRules(t *testing.T) {
	base := "rules:\n  - MATCH\n"
	overlay := "prepend-rules:\n  - DOMAIN,example.com,DIRECT\n"
	out := mergeAndParse(t, base, overlay)
	rules := out["rules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(rules))
	}
	if rules[0] != "DOMAIN,example.com,DIRECT" {
		t.Errorf("rules[0] = %v, want prepended entry", rules[0])
	}
	if rules[1] != "MATCH" {
		t.Errorf("rules[1] = %v, want base entry", rules[1])
	}
	if _, present := out["prepend-rules"]; present {
		t.Error("directive key leaked into output")
	}
}

func TestAppendProxies(t *testing.T) {
	base := "proxies:\n  - {name: existing}\n"
	overlay := "append-proxies:\n  - {name: extra}\n"
	out := mergeAndParse(t, base, overlay)
	proxies := out["proxies"].([]any)
	if len(proxies) != 2 {
		t.Fatalf("got %d proxies, want 2", len(proxies))
	}
	p0 := proxies[0].(map[string]any)
	if p0["name"] != "existing" {
		t.Errorf("base entry not first")
	}
	p1 := proxies[1].(map[string]any)
	if p1["name"] != "extra" {
		t.Errorf("appended entry not last")
	}
	if _, present := out["append-proxies"]; present {
		t.Error("directive key leaked into output")
	}
}

func TestPrependAndAppendSameTarget(t *testing.T) {
	base := "proxy-groups:\n  - base\n"
	overlay := "prepend-proxy-groups:\n  - pre\nappend-proxy-groups:\n  - post\n"
	out := mergeAndParse(t, base, overlay)
	groups := out["proxy-groups"].([]any)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	if groups[0] != "pre" || groups[1] != "base" || groups[2] != "post" {
		t.Errorf("got order %v, want [pre base post]", groups)
	}
}

func TestDirectiveOnMissingTarget(t *testing.T) {
	out := mergeAndParse(t, "mixed-port: 7890\n", "prepend-rules:\n  - RULE\n")
	rules, ok := out["rules"].([]any)
	if !ok {
		t.Fatalf("rules = %T, want []any", out["rules"])
	}
	if len(rules) != 1 || rules[0] != "RULE" {
		t.Errorf("got %v, want [RULE]", rules)
	}
}

func TestDirectiveWithNonArrayBase(t *testing.T) {
	out := mergeAndParse(t, "rules: not-an-array\n", "append-rules:\n  - RULE\n")
	rules, ok := out["rules"].([]any)
	if !ok {
		t.Fatalf("rules = %T, want []any", out["rules"])
	}
	if len(rules) != 1 || rules[0] != "RULE" {
		t.Errorf("got %v, want [RULE]", rules)
	}
}

func TestVisualPatchKeyStripped(t *testing.T) {
	base := "mixed-port: 7890\n"
	overlay := "mixed-port: 7891\nx-metacubexd-visual-patch:\n  rules:\n    - patch\n"
	out := mergeAndParse(t, base, overlay)
	if _, present := out["x-metacubexd-visual-patch"]; present {
		t.Error("visual-patch key leaked into output")
	}
	if out["mixed-port"] != 7891 {
		t.Error("sibling field not merged")
	}
}

func TestMultipleOverlaysCompose(t *testing.T) {
	base := "mixed-port: 7890\nrules:\n  - base\n"
	o1 := "mixed-port: 7891\nprepend-rules:\n  - pre1\n"
	o2 := "mixed-port: 7892\nappend-rules:\n  - post2\n"
	out := mergeAndParse(t, base, o1, o2)
	if out["mixed-port"] != 7892 {
		t.Errorf("last overlay should win, got %v", out["mixed-port"])
	}
	rules := out["rules"].([]any)
	if len(rules) != 3 {
		t.Fatalf("got %d rules, want 3", len(rules))
	}
	if rules[0] != "pre1" || rules[1] != "base" || rules[2] != "post2" {
		t.Errorf("got %v", rules)
	}
}

func TestErrorMentionsOverlay(t *testing.T) {
	_, err := MergeConfigs("a: 1\n", []string{"- bad\n"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "overlay") {
		t.Errorf("error %q should mention 'overlay'", err.Error())
	}
}

package shared

import "regexp"

// ANSIRe matches SGR escape sequences; used to strip colors for plain-text
// matching and test assertions.
var ANSIRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripANSI removes SGR escape sequences for plain-text matching.
func StripANSI(s string) string {
	return ANSIRe.ReplaceAllString(s, "")
}

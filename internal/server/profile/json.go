// JSON helpers for profile/index/state persistence. Kept tiny and separate
// from profile.go so the persistence format is easy to audit in one place.
package profile

import "encoding/json"

func jsonUnmarshal(b []byte, v any) error  { return json.Unmarshal(b, v) }
func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

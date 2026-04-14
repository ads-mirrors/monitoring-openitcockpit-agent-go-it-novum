package utils

import "math"

// SafeFloat checks if the provided float64 value is NaN or Inf and returns 0.0 in those cases, otherwise it returns the original value.
// This should avoid errors on JSON encoding which will fail if NaN or Inf values are present.
// https://github.com/openITCOCKPIT/openitcockpit-agent-go/issues/88
func SafeFloat(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0.0
	}
	return f
}

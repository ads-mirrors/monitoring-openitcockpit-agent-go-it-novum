package agentrt

import (
	"encoding/json"
	"math"
	"testing"
)

func TestSanitizeFloats_NaN(t *testing.T) {
	input := map[string]interface{}{
		"valid":   42.5,
		"nan_val": math.NaN(),
		"inf_val": math.Inf(1),
		"neg_inf": math.Inf(-1),
	}

	result := sanitizeFloats(input).(map[string]interface{})

	if result["valid"].(float64) != 42.5 {
		t.Errorf("expected 42.5, got %v", result["valid"])
	}
	if result["nan_val"].(float64) != 0 {
		t.Errorf("expected 0 for NaN, got %v", result["nan_val"])
	}
	if result["inf_val"].(float64) != 0 {
		t.Errorf("expected 0 for Inf, got %v", result["inf_val"])
	}
	if result["neg_inf"].(float64) != 0 {
		t.Errorf("expected 0 for -Inf, got %v", result["neg_inf"])
	}
}

func TestSanitizeFloats_NestedMap(t *testing.T) {
	input := map[string]interface{}{
		"cpu": map[string]interface{}{
			"total": math.NaN(),
			"cores": []interface{}{1.5, math.NaN(), 3.0, math.Inf(1)},
		},
		"name": "test",
	}

	result := sanitizeFloats(input).(map[string]interface{})

	cpu := result["cpu"].(map[string]interface{})
	if cpu["total"].(float64) != 0 {
		t.Errorf("expected 0 for nested NaN, got %v", cpu["total"])
	}

	cores := cpu["cores"].([]interface{})
	if cores[0].(float64) != 1.5 {
		t.Errorf("expected 1.5, got %v", cores[0])
	}
	if cores[1].(float64) != 0 {
		t.Errorf("expected 0 for NaN in slice, got %v", cores[1])
	}
	if cores[3].(float64) != 0 {
		t.Errorf("expected 0 for Inf in slice, got %v", cores[3])
	}
}

func TestSanitizeFloats_Struct(t *testing.T) {
	type DiskIO struct {
		ReadAvgWait  float64
		WriteAvgWait float64
		LoadPercent  float64
		Device       string
	}

	input := &DiskIO{
		ReadAvgWait:  math.NaN(),
		WriteAvgWait: 1.5,
		LoadPercent:  math.Inf(1),
		Device:       "C:",
	}

	result := sanitizeFloats(input).(*DiskIO)

	if result.ReadAvgWait != 0 {
		t.Errorf("expected 0 for NaN, got %v", result.ReadAvgWait)
	}
	if result.WriteAvgWait != 1.5 {
		t.Errorf("expected 1.5, got %v", result.WriteAvgWait)
	}
	if result.LoadPercent != 0 {
		t.Errorf("expected 0 for Inf, got %v", result.LoadPercent)
	}
	if result.Device != "C:" {
		t.Errorf("expected C:, got %v", result.Device)
	}
}

func TestSanitizeFloats_JsonMarshalSucceeds(t *testing.T) {
	input := map[string]interface{}{
		"value":  math.NaN(),
		"nested": map[string]interface{}{"x": math.Inf(-1)},
		"list":   []interface{}{math.NaN(), 1.0},
	}

	// Without sanitize, this would fail
	_, err := json.Marshal(input)
	if err == nil {
		t.Fatal("expected json.Marshal to fail on NaN without sanitization")
	}

	// With sanitize, it should succeed
	sanitized := sanitizeFloats(input)
	data, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatalf("json.Marshal failed after sanitization: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["value"].(float64) != 0 {
		t.Errorf("expected 0, got %v", parsed["value"])
	}
}

func TestSanitizeFloats_Nil(t *testing.T) {
	if sanitizeFloats(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestSanitizeFloats_NoFloats(t *testing.T) {
	input := map[string]interface{}{
		"name":  "test",
		"count": 42,
		"tags":  []interface{}{"a", "b"},
	}

	result := sanitizeFloats(input)

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["name"].(string) != "test" {
		t.Errorf("expected test, got %v", parsed["name"])
	}
}

package workflow

import (
	"strconv"
	"strings"
)

// compareValues compares two string values with the given operator.
func compareValues(left, right, op string) (bool, error) {
	// Try numeric comparison first
	leftNum, leftErr := strconv.ParseFloat(left, 64)
	rightNum, rightErr := strconv.ParseFloat(right, 64)

	if leftErr == nil && rightErr == nil {
		switch op {
		case ">":
			return leftNum > rightNum, nil
		case "<":
			return leftNum < rightNum, nil
		case ">=":
			return leftNum >= rightNum, nil
		case "<=":
			return leftNum <= rightNum, nil
		}
	}

	// Fall back to string comparison
	switch op {
	case ">":
		return left > right, nil
	case "<":
		return left < right, nil
	case ">=":
		return left >= right, nil
	case "<=":
		return left <= right, nil
	}

	return false, nil
}

// Stringify attempts to stringify various types from YAML.
func Stringify(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case []interface{}:
		var parts []string
		for _, item := range val {
			parts = append(parts, Stringify(item))
		}
		return strings.Join(parts, ", ")
	case map[string]interface{}:
		var parts []string
		for k, item := range val {
			parts = append(parts, k+": "+Stringify(item))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return ""
	}
}

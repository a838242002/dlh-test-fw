package model

import "fmt"

// ScenarioDescription returns the human description for a scenario: the
// dlh.scenario/description annotation if set, else a summary derived from the
// chaos type, target type, and SLO. Any field may be empty.
func ScenarioDescription(annotations map[string]string, id, targetType, slo string) string {
	if d := annotations["dlh.scenario/description"]; d != "" {
		return d
	}
	switch {
	case targetType != "" && slo != "":
		return fmt.Sprintf("%s chaos on a %s target, evaluated against the %s SLO.", chaosFromID(id, targetType), targetType, slo)
	case targetType != "":
		return fmt.Sprintf("chaos scenario on a %s target.", targetType)
	default:
		return fmt.Sprintf("scenario %s.", id)
	}
}

// chaosFromID strips a leading "<targetType>-" so "mysql-pod-delete" → "pod-delete".
func chaosFromID(id, targetType string) string {
	if targetType != "" && len(id) > len(targetType)+1 && id[:len(targetType)+1] == targetType+"-" {
		return id[len(targetType)+1:]
	}
	return id
}

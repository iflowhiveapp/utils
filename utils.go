// utils.go
package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Knetic/govaluate"
)

/* ---------- Metrics configuration ---------- */

// MetricsConfig describes how a metric should be converted.
type MetricsConfig struct {
	ShortCode   string `json:"shortCode"`
	Description string `json:"description"`
	Formula     string `json:"formula"` // e.g. "(#VALUE / #TOTAL_VALUE) * 100"
}

// Metrics is a single metric entry in the global list.
type Metrics struct {
	Metrics       string        `json:"metrics"`  // CPU, Memory, Disk, …
	BaseUnit      string        `json:"baseUnit"` // Bytes, Percentage, …
	MetricsConfig MetricsConfig `json:"metricsConfig"`
}

// GlobalMetricsConfig can be filled by your application at start‑up
// (e.g. from JSON or a DB) and is used by the conversion helpers below.
var GlobalMetricsConfig []Metrics

/* ---------- Generic converters ---------- */

// StringToUint64 converts "123" or "123.45" ➜ 123.
func StringToUint64(str string) uint64 {
	if strings.Contains(str, ".") {
		str = strings.Split(str, ".")[0]
	}
	v, _ := strconv.ParseUint(str, 10, 64) // ignore error, returns 0 on failure
	return v
}

// ConvertStringToInt converts "42" ➜ 42.
func ConvertStringToInt(str string) int {
	v, _ := strconv.Atoi(str)
	return v
}

// StringToFloat converts "3.14" ➜ 3.14.
func StringToFloat(str string) float64 {
	v, _ := strconv.ParseFloat(str, 64)
	return v
}

/* ---------- Metric‑specific helpers ---------- */

// ValueConvertPercentage converts used vs total cores ➜ percentage (%).
func ValueConvertPercentage(usedCores, totalCores float64) (float64, error) {
	for _, m := range GlobalMetricsConfig {
		if m.Metrics == "CPU" && m.BaseUnit == "Percentage" {
			return applyFormula(m.MetricsConfig.Formula, usedCores, totalCores)
		}
	}
	return 0, fmt.Errorf("CPU metrics configuration not found")
}

// MemoryValueConvert converts bytes ➜ MiB/GiB/… depending on config.
func MemoryValueConvert(used, total, free uint64) (u, t, f float64, unit string, err error) {
	return genericMemoryConvert("Memory", used, total, free)
}

// DiskValueConvert converts bytes ➜ MiB/GiB/… depending on config.
func DiskValueConvert(used, total, free uint64) (u, t, f float64, unit string, err error) {
	return genericMemoryConvert("Disk", used, total, free)
}

// genericMemoryConvert is shared by Memory/Disk helpers.
func genericMemoryConvert(metric string, used, total, free uint64) (float64, float64, float64, string, error) {
	for _, m := range GlobalMetricsConfig {
		if m.Metrics == metric && m.MetricsConfig.Formula != "" && m.BaseUnit != "Bytes" {
			u, err1 := applyFormula(m.MetricsConfig.Formula, float64(used), float64(total))
			t, err2 := applyFormula(m.MetricsConfig.Formula, float64(total), float64(total))
			f, err3 := applyFormula(m.MetricsConfig.Formula, float64(free), float64(total))
			if err := firstErr(err1, err2, err3); err != nil {
				return 0, 0, 0, m.BaseUnit, err
			}
			return RoundTwo(u), RoundTwo(t), RoundTwo(f), m.BaseUnit, nil
		}
	}
	// Fallback: leave values in Bytes.
	return RoundTwo(float64(used)), RoundTwo(float64(total)), RoundTwo(float64(free)), "Bytes", nil
}

/* ---------- Internal helpers ---------- */

func applyFormula(formula string, value, total float64) (float64, error) {
	formula = strings.ReplaceAll(formula, "#VALUE", strconv.FormatFloat(value, 'f', 2, 64))
	formula = strings.ReplaceAll(formula, "#TOTAL_VALUE", strconv.FormatFloat(total, 'f', 2, 64))
	return evalFormula(formula)
}

func evalFormula(exprStr string) (float64, error) {
	expr, err := govaluate.NewEvaluableExpression(exprStr)
	if err != nil {
		return 0, err
	}
	v, err := expr.Evaluate(nil)
	if err != nil {
		return 0, err
	}
	return RoundTwo(v.(float64)), nil
}

func RoundTwo(v float64) float64 { return math.Round(v*100) / 100 }

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

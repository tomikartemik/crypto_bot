package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func CountDecimals(f float64) int {
	s := fmt.Sprintf("%f", f)
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return 0
	}
	dec := strings.TrimRight(parts[1], "0")
	return len(dec)
}

func ParseTimeframe(tf string) (time.Duration, error) {
	unit := tf[len(tf)-1]   // последняя буква (m или h)
	value := tf[:len(tf)-1] // всё кроме последней

	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}

	switch unit {
	case 'm', 'M':
		return time.Duration(n) * time.Minute, nil
	case 'h', 'H':
		return time.Duration(n) * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported timeframe: %s", tf)
	}
}

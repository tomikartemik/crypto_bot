package utils

import (
	"fmt"
	"strings"
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

package currency

import (
	"strconv"
	"strings"
)

const IDR = "IDR"

const digitsPerGroup = 3

const idrSeparator = "."

func FormatIDR(amount int64) string {
	return "Rp" + Group(amount, idrSeparator)
}

// Format renders an amount in the given currency, falling back to "CODE 1,250,000" for
// anything that is not IDR so an unexpected currency is still legible rather than
// silently mislabelled as rupiah.
func Format(amount int64, code string) string {
	if strings.EqualFold(strings.TrimSpace(code), IDR) {
		return FormatIDR(amount)
	}
	return strings.ToUpper(strings.TrimSpace(code)) + " " + Group(amount, ",")
}

// Group inserts a separator every three digits from the right, preserving a leading sign.
func Group(amount int64, separator string) string {
	digits := strconv.FormatInt(amount, 10)

	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) <= digitsPerGroup {
		return sign + digits
	}

	// Walk from the right so the leftmost group absorbs the remainder: 1250000 groups as
	// 1|250|000, not 125|000|0.
	var b strings.Builder
	lead := len(digits) % digitsPerGroup
	if lead == 0 {
		lead = digitsPerGroup
	}
	b.WriteString(digits[:lead])
	for i := lead; i < len(digits); i += digitsPerGroup {
		b.WriteString(separator)
		b.WriteString(digits[i : i+digitsPerGroup])
	}
	return sign + b.String()
}

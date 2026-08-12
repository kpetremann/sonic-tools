package sonic

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// sortNames sorts names on their numbers rather than on their digits, so that
// Ethernet2 comes before Ethernet10 and Fantray2_1 before Fantray10_1.
func sortNames(names []string) {
	slices.SortFunc(names, compareNames)
}

func compareNames(a, b string) int {
	for a != "" && b != "" {
		textA, numberA, restA := cutNumber(a)
		textB, numberB, restB := cutNumber(b)

		if textA != textB {
			return strings.Compare(textA, textB)
		}
		if numberA != numberB {
			return cmp.Compare(numberA, numberB)
		}
		a, b = restA, restB
	}

	return cmp.Compare(len(a), len(b))
}

// cutNumber splits the leading text and number of a name: "Fantray10_1" -> "Fantray", 10, "_1".
func cutNumber(name string) (string, int, string) {
	digits := strings.IndexFunc(name, isDigit)
	if digits < 0 {
		return name, -1, ""
	}

	text := name[:digits]
	end := digits
	for end < len(name) && isDigit(rune(name[end])) {
		end++
	}
	number, _ := strconv.Atoi(name[digits:end])

	return text, number, name[end:]
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

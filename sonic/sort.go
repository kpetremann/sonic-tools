package sonic

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// SortPeers sorts BGP peers on the value of their address rather than on its digits, so that
// 192.0.2.9 comes before 192.0.2.10 and the IPv4 sessions before the IPv6 ones. FRR keys the
// unnumbered peers by interface name, those are sorted as names and come last.
func SortPeers(peers []string) {
	slices.SortFunc(peers, comparePeers)
}

func comparePeers(a, b string) int {
	addrA, errA := netip.ParseAddr(a)
	addrB, errB := netip.ParseAddr(b)

	switch {
	case errA == nil && errB == nil:
		return addrA.Compare(addrB)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}

	return compareNames(a, b)
}

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

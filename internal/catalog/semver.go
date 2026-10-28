// Semantic version validation and precedence for catalog versions.
package catalog

import (
	"regexp"
	"strings"
)

var semverPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

func numeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

func validVersion(v string) bool {
	m := semverPattern.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	if m[4] != "" {
		for _, part := range strings.Split(m[4], ".") {
			if numeric(part) && len(part) > 1 && part[0] == '0' {
				return false
			}
		}
	}
	return true
}

func compareNumber(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// compareVersions compares valid versions without converting numeric parts to
// machine integers. Build metadata does not affect semantic precedence.
func compareVersions(a, b string) int {
	x, y := semverPattern.FindStringSubmatch(a), semverPattern.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		if c := compareNumber(x[i], y[i]); c != 0 {
			return c
		}
	}
	if x[4] == "" || y[4] == "" {
		if x[4] == y[4] {
			return 0
		}
		if x[4] == "" {
			return 1
		}
		return -1
	}
	xs, ys := strings.Split(x[4], "."), strings.Split(y[4], ".")
	for i := 0; i < len(xs) && i < len(ys); i++ {
		xn, yn := numeric(xs[i]), numeric(ys[i])
		if xn && yn {
			if c := compareNumber(xs[i], ys[i]); c != 0 {
				return c
			}
		} else if xn != yn {
			if xn {
				return -1
			}
			return 1
		} else if c := strings.Compare(xs[i], ys[i]); c != 0 {
			return c
		}
	}
	if len(xs) < len(ys) {
		return -1
	}
	if len(xs) > len(ys) {
		return 1
	}
	return 0
}

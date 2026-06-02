package update

import (
	"fmt"
	"strconv"
	"strings"
)

// DevVersion is the sentinel version stamped into binaries built without a
// release tag (the default value of cmd.Version). It never compares as
// up-to-date, so `spec update` always offers an upgrade for dev builds.
const DevVersion = "dev"

// IsDev reports whether v is an untagged development build.
func IsDev(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == DevVersion
}

// compareVersions returns -1 if a < b, 0 if a == b, and 1 if a > b, comparing
// the numeric major.minor.patch components. A pre-release suffix (e.g.
// "-rc.1") sorts below the same version without one. Returns an error if either
// value is not parseable as a semantic version.
func compareVersions(a, b string) (int, error) {
	amaj, amin, apatch, apre, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bmaj, bmin, bpatch, bpre, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]int{{amaj, bmaj}, {amin, bmin}, {apatch, bpatch}} {
		if pair[0] != pair[1] {
			return cmpInt(pair[0], pair[1]), nil
		}
	}
	return cmpPrerelease(apre, bpre), nil
}

// parseVersion splits a "vMAJOR.MINOR.PATCH[-prerelease]" string into its
// numeric components and pre-release tag. The leading "v" is optional.
func parseVersion(v string) (major, minor, patch int, prerelease string, err error) {
	core := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(core, '-'); i >= 0 {
		prerelease = core[i+1:]
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, "", fmt.Errorf("invalid version %q: want MAJOR.MINOR.PATCH", v)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, convErr := strconv.Atoi(p)
		if convErr != nil {
			return 0, 0, 0, "", fmt.Errorf("invalid version %q: %q is not numeric", v, p)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], prerelease, nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// cmpPrerelease orders pre-release tags: a release (empty tag) outranks any
// pre-release of the same core version, otherwise compare lexically.
func cmpPrerelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	case a < b:
		return -1
	default:
		return 1
	}
}

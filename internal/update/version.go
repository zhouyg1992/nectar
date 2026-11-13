package update

import (
	"fmt"
	"strconv"
	"strings"
)

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func parseVersion(value string) (semanticVersion, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")
	if value == "" {
		return semanticVersion{}, fmt.Errorf("empty version")
	}

	core := value
	prerelease := ""
	if i := strings.IndexByte(core, '-'); i >= 0 {
		prerelease = core[i+1:]
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semanticVersion{}, fmt.Errorf("invalid version %q", value)
	}

	numbers := make([]int, 3)
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semanticVersion{}, fmt.Errorf("invalid version %q", value)
		}
		numbers[i] = number
	}
	return semanticVersion{
		major:      numbers[0],
		minor:      numbers[1],
		patch:      numbers[2],
		prerelease: prerelease,
	}, nil
}

func compareVersions(left, right string) (int, error) {
	a, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	b, err := parseVersion(right)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]int{
		{a.major, b.major},
		{a.minor, b.minor},
		{a.patch, b.patch},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1, nil
		case pair[0] > pair[1]:
			return 1, nil
		}
	}
	switch {
	case a.prerelease == "" && b.prerelease != "":
		return 1, nil
	case a.prerelease != "" && b.prerelease == "":
		return -1, nil
	case a.prerelease < b.prerelease:
		return -1, nil
	case a.prerelease > b.prerelease:
		return 1, nil
	default:
		return 0, nil
	}
}

func normalizeTag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "v") {
		return value
	}
	return "v" + value
}

func versionWithoutV(value string) string {
	return strings.TrimPrefix(normalizeTag(value), "v")
}

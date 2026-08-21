package version

import "runtime/debug"

// SHA returns the VCS revision this binary was built from. Unbuilt trees
// report "devel".
func SHA() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	revision := ""
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if revision == "" {
		return "devel"
	}
	if dirty {
		return revision + "-dirty"
	}
	return revision
}

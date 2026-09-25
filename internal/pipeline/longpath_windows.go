package pipeline

import "golang.org/x/sys/windows/registry"

// longPathsNote says whether Windows long paths are switched on. takeout and
// ExifTool 13.07+ do not need it; other tools opening the results may.
func longPathsNote() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\FileSystem`, registry.QUERY_VALUE)
	if err != nil {
		return "unknown (takeout does not need it)"
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("LongPathsEnabled")
	if err != nil || v == 0 {
		return "off (takeout does not need it; some other programs cannot open paths over 260 characters)"
	}
	return "on"
}

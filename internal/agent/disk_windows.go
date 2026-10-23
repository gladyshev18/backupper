package agent

import "golang.org/x/sys/windows"

// The agent targets Linux; this lets it build and run tests on Windows.
func diskUsage(path string) (total, free uint64) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	var avail, tot, totFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &totFree); err != nil {
		return 0, 0
	}
	return tot, avail
}

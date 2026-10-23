package agent

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"backupper/internal/model"
)

var toolNames = []string{"mysqldump", "mysql", "pg_dump", "pg_dumpall", "psql", "tar", "sudo", "runuser"}

// readMeminfo parses /proc/meminfo (values in kB).
func readMeminfo() map[string]uint64 {
	out := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		out[strings.TrimSuffix(fields[0], ":")] = v * 1024
	}
	return out
}

// MemAvailable returns free memory usable without swapping.
func MemAvailable() uint64 {
	m := readMeminfo()
	if v, ok := m["MemAvailable"]; ok {
		return v
	}
	return m["MemFree"] + m["Cached"]
}

func osRelease() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

func TmpDir() string {
	if d := os.Getenv("BACKUPPER_TMP"); d != "" {
		return d
	}
	if runtime.GOOS == "linux" {
		return "/var/tmp"
	}
	return os.TempDir()
}

func CollectSysInfo() model.SysInfo {
	si := model.SysInfo{
		OS:     osRelease(),
		Arch:   runtime.GOARCH,
		CPUs:   runtime.NumCPU(),
		TmpDir: TmpDir(),
		Tools:  map[string]bool{},
	}
	si.Hostname, _ = os.Hostname()
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		si.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			si.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			v, _ := strconv.ParseFloat(f[0], 64)
			si.Uptime = int64(v)
		}
	}
	m := readMeminfo()
	si.MemTotal = m["MemTotal"]
	si.MemAvailable = MemAvailable()
	si.SwapTotal = m["SwapTotal"]
	si.SwapFree = m["SwapFree"]
	si.DiskTotal, si.DiskFree = diskUsage(si.TmpDir)
	name, off := time.Now().Zone()
	si.TZ, si.TZOffset = name, off
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		si.TZ = strings.TrimSpace(string(b))
	} else if l, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(l, "zoneinfo/"); i >= 0 {
			si.TZ = l[i+len("zoneinfo/"):]
		}
	}
	for _, t := range toolNames {
		_, err := exec.LookPath(t)
		si.Tools[t] = err == nil
	}
	return si
}

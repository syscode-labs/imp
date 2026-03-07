//go:build linux

package guest

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type cpuStat struct {
	total  uint64
	idle   uint64
	iowait uint64
}

func readCPUStat() (cpuStat, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuStat{}, err
	}
	defer f.Close() //nolint:errcheck
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var vals []uint64
		for _, fld := range fields {
			v, _ := strconv.ParseUint(fld, 10, 64)
			vals = append(vals, v)
		}
		var total uint64
		for _, v := range vals {
			total += v
		}
		idle := uint64(0)
		if len(vals) > 3 {
			idle = vals[3]
		}
		iowait := uint64(0)
		if len(vals) > 4 {
			iowait = vals[4]
		}
		return cpuStat{total: total, idle: idle, iowait: iowait}, nil
	}
	return cpuStat{}, fmt.Errorf("cpu line not found in /proc/stat")
}

// cpuAndIOWaitUsage samples /proc/stat twice 100ms apart and returns both
// the CPU usage ratio and the iowait ratio (both 0.0–1.0).
func cpuAndIOWaitUsage() (cpuRatio, iowaitRatio float64, err error) {
	s1, err := readCPUStat()
	if err != nil {
		return
	}
	time.Sleep(100 * time.Millisecond)
	s2, err := readCPUStat()
	if err != nil {
		return
	}
	total := float64(s2.total - s1.total)
	if total == 0 {
		return
	}
	idle := float64(s2.idle - s1.idle)
	iowait := float64(s2.iowait - s1.iowait)
	cpuRatio = (total - idle) / total
	iowaitRatio = iowait / total
	return
}

// memUsedBytes returns MemTotal - MemAvailable from /proc/meminfo.
func memUsedBytes() (int64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck
	vals := map[string]int64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		v, _ := strconv.ParseInt(fields[1], 10, 64)
		vals[key] = v * 1024 // kB → bytes
	}
	return vals["MemTotal"] - vals["MemAvailable"], nil
}

// diskUsedBytes returns used bytes on the filesystem containing path.
func diskUsedBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	used := int64(stat.Blocks-stat.Bavail) * stat.Bsize
	return used, nil
}

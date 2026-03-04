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

// cpuUsage samples /proc/stat twice 100ms apart and returns the ratio 0.0–1.0.
func cpuUsage() (float64, error) {
	s1, err := readCPUStat()
	if err != nil {
		return 0, err
	}
	time.Sleep(100 * time.Millisecond)
	s2, err := readCPUStat()
	if err != nil {
		return 0, err
	}
	total := float64(s2.total - s1.total)
	idle := float64(s2.idle - s1.idle)
	if total == 0 {
		return 0, nil
	}
	return (total - idle) / total, nil
}

type cpuStat struct{ total, idle uint64 }

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
		return cpuStat{total: total, idle: idle}, nil
	}
	return cpuStat{}, fmt.Errorf("cpu line not found in /proc/stat")
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
	total := int64(stat.Blocks) * stat.Bsize
	free := int64(stat.Bfree) * stat.Bsize
	return total - free, nil
}

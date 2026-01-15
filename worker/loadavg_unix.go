//go:build !windows

package worker

import (
	"github.com/shirou/gopsutil/v3/load"
)

// getLoadAvg 获取系统负载（Unix/Linux）
func getLoadAvg() (*LoadAvgInfo, error) {
	avg, err := load.Avg()
	if err != nil {
		return nil, err
	}
	return &LoadAvgInfo{
		Load1:  avg.Load1,
		Load5:  avg.Load5,
		Load15: avg.Load15,
	}, nil
}

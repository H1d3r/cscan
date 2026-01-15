//go:build windows

package worker

import (
	"errors"
)

// getLoadAvg 获取系统负载（Windows不支持，返回错误）
func getLoadAvg() (*LoadAvgInfo, error) {
	return nil, errors.New("load average not supported on Windows")
}

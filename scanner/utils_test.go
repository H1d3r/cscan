package scanner

import (
	"net"
	"testing"
)

func TestIncIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       net.IP
		expected net.IP
		overflow bool
	}{
		{
			name:     "Normal increment IPv4",
			ip:       net.ParseIP("192.168.1.1").To4(),
			expected: net.ParseIP("192.168.1.2").To4(),
			overflow: false,
		},
		{
			name:     "Overflow increment IPv4",
			ip:       net.ParseIP("255.255.255.255").To4(),
			expected: net.ParseIP("0.0.0.0").To4(),
			overflow: true, // true 表示发生溢出
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ipCopy := make(net.IP, len(tc.ip))
			copy(ipCopy, tc.ip)

			// 编译期报错，因为目前的 incIP 没有返回值
			gotOverflow := incIP(ipCopy)

			if gotOverflow != tc.overflow {
				t.Errorf("incIP(%s) overflow = %v, want %v", tc.ip, gotOverflow, tc.overflow)
			}
			if !ipCopy.Equal(tc.expected) {
				t.Errorf("incIP(%s) resulted in %s, want %s", tc.ip, ipCopy, tc.expected)
			}
		})
	}
}

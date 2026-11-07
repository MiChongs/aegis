//go:build !unix

package traffic

import "time"

// processCPUTime 非 Unix 平台不采样 CPU：过载保护只按在途硬上限工作。
func processCPUTime() (time.Duration, bool) { return 0, false }

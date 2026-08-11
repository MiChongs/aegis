package egress

import (
	"os"
	"testing"
)

// TestMain 关闭 go-shadowsocks2 的重放盐过滤器。
//
// 该过滤器是**进程级单例**：客户端 initWriter 写入的 salt 会被登记，
// 同进程内的服务端 initReader 随即把它判成重放（repeated salt detected）。
// 真实部署里两端不在同一进程，不会触发；只有把两端跑在一个测试进程里才会。
// 上游为此留了「容量为负即禁用」的开关，这里在任何连接建立前设置。
func TestMain(m *testing.M) {
	if err := os.Setenv("SHADOWSOCKS_SF_CAPACITY", "-1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

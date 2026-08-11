package banner

import (
	"io"
	"os"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// 终端能力探测。
//
// 这里是本包唯一直接接触进程环境的地方，且只探测「终端长什么样」这一类事实
// （是否 TTY、有多少列）。着色相关的事实标准变量（NO_COLOR / FORCE_COLOR /
// TERM=dumb）由 go-pretty 的 text 包在自己的 init 里识别，本包不重复实现。
// 任何业务开关（是否打印、用哪种字体、打印多详细）一律走 config.Config
// 注入到 Options，不在这里读环境变量。

// terminalFile 把 io.Writer 还原成 *os.File。
// 只有真实文件句柄才谈得上「是不是终端」和「有多宽」；
// 写进 bytes.Buffer 或日志管道的输出一律按非终端处理。
func terminalFile(w io.Writer) (*os.File, bool) {
	f, ok := w.(*os.File)
	if !ok || f == nil {
		return nil, false
	}
	return f, true
}

// isTerminal 判断输出目标是否为交互式终端。
// 同时覆盖原生控制台与 Cygwin/MSYS（Git Bash）伪终端——
// 后者在 Windows 上很常见，漏判会让横幅无谓地降级成无色纯文本。
func isTerminal(w io.Writer) bool {
	f, ok := terminalFile(w)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// terminalWidth 返回终端列数；非终端或探测失败返回 0，由调用方决定回退值。
//
// 目标句柄探测失败时会再问一次 stdout：常见于 stderr 被重定向、
// 但进程仍然挂在一个有尺寸的控制台上的情形。
func terminalWidth(w io.Writer) int {
	if f, ok := terminalFile(w); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 0 {
			return width
		}
	}
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return width
	}
	return 0
}

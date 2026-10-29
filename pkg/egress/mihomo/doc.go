// Package mihomo 把 mihomo（Clash Meta）核心接进出海网关。
//
// mihomo 以 GPL-3.0 发布，本平台不把它编进自己的二进制，而是作为**旁车进程**托管：
//
//	Core     核心进程的安装（GitHub 发布页 + sha256 校验）、启动、热重载、崩溃自拉起、日志与流量采样
//	Profile  要交给核心的配置（节点、订阅、策略组、分流规则、DNS……），Render 成 config.yaml
//	Client   核心的 external-controller REST 接口（延迟测试、切换节点、连接、订阅更新……）
//	协议     注册到 egress 的 "mihomo" 端点协议：经本机 SOCKS5 入站把流量交给核心
//
// 出海网关与核心之间只有一条带鉴权的回环 SOCKS5 入站。每个被网关引用的策略组 / 节点
// 对应一个入站用户名，核心配置里置顶一条 `IN-USER,<用户名>,<目标>` 规则，于是
// 「这条连接交给哪个策略组」由用户名决定，而核心自己的分流规则只服务于「交给核心分流」的流量。
//
// 本包只依赖 egress 包本身，不依赖 aegis 的内部包。
package mihomo

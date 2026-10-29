// Package mihomo 把 mihomo（Clash Meta）核心接进出海网关。
//
// mihomo 以 GPL-3.0 发布，本平台不把它编进自己的二进制，而是作为**旁车进程**托管：
//
//	Core     核心进程的安装（GitHub 发布页 + sha256 校验）、启动、热重载、崩溃自拉起、日志与流量采样
//	Profile  要交给核心的配置（节点、订阅、策略组、分流规则、DNS……），Render 成 config.yaml
//	Client   核心的 external-controller REST 接口（延迟测试、切换节点、连接、订阅更新……）
//	协议     注册到 egress 的 "mihomo" 端点协议：经本机 SOCKS5 入站把流量交给核心
//
// 出海网关与核心之间是若干条带鉴权的回环 SOCKS5 入站：每个被网关引用的策略组 / 节点各有一个专用入站
// （listeners[].proxy 指向它），TCP 与 UDP 都直达目标、不经分流规则；另有一个不指定目标的入站，
// 「交给 mihomo 分流」的流量从这里进、按核心自己的分流规则走。端口在热重载之间保持不变。
//
// 本包只依赖 egress 包本身，不依赖 aegis 的内部包。
package mihomo

// forwarded-headers.mjs 的行为约束。
//
// 这串逻辑判错的表现不是报错，而是后端悄悄按控制台的地址限流、按控制台的地址封禁，
// 因此每一条规则都必须有测试直接盯着 —— 与 Go 侧 pkg/clientip 的测试同一个理由。
//
//   pnpm test

import assert from "node:assert/strict";
import http from "node:http";
import net from "node:net";
import { after, describe, it } from "node:test";

import {
  backendHopWarning,
  describeBackendHop,
  installOnServer,
  normalizePeerAddress,
  stampForwardedHeaders
} from "./forwarded-headers.mjs";

/** 造一个够用的假请求：只有 headers 与 socket 参与判定。 */
function fakeRequest({ headers = {}, remoteAddress = "10.0.0.7", localPort = 3000, encrypted = false } = {}) {
  return { headers, socket: { remoteAddress, localPort, encrypted } };
}

describe("normalizePeerAddress", () => {
  it("还原 IPv4 映射地址", () => {
    // 双栈监听下 IPv4 连接就是这个形态。不还原的话后端拿到的是一条 IPv6 条目，
    // 与 TRUSTED_PROXIES 里的 IPv4 网段永远对不上。
    assert.equal(normalizePeerAddress("::ffff:203.0.113.9"), "203.0.113.9");
  });

  it("去掉 IPv6 zone", () => {
    assert.equal(normalizePeerAddress("fe80::1%eth0"), "fe80::1");
  });

  it("保留普通地址", () => {
    assert.equal(normalizePeerAddress("2001:db8::1"), "2001:db8::1");
    assert.equal(normalizePeerAddress(" 192.0.2.1 "), "192.0.2.1");
  });

  it("拿不到地址时返回空串", () => {
    // Unix socket / 命名管道没有对端地址，此时宁可什么都不写，
    // 也不能往链上追加一条解析不出来的坏值。
    for (const raw of [undefined, null, "", "   ", "/tmp/app.sock", "not-an-ip"]) {
      assert.equal(normalizePeerAddress(raw), "");
    }
  });
});

describe("stampForwardedHeaders", () => {
  it("链为空时写入对端地址", () => {
    const req = fakeRequest({ remoteAddress: "10.0.0.7" });
    assert.equal(stampForwardedHeaders(req), true);
    assert.equal(req.headers["x-forwarded-for"], "10.0.0.7");
  });

  it("追加而不是覆盖", () => {
    // 这是整个模块的安全底座：客户端伪造的条目留在左边，
    // 后端从右往左走时先遇到我们写的这条真事实。
    const req = fakeRequest({ headers: { "x-forwarded-for": "8.8.8.8" }, remoteAddress: "10.0.0.7" });
    stampForwardedHeaders(req);
    assert.equal(req.headers["x-forwarded-for"], "8.8.8.8, 10.0.0.7");
  });

  it("同一个请求只追加一次", () => {
    const req = fakeRequest({ remoteAddress: "10.0.0.7" });
    stampForwardedHeaders(req);
    assert.equal(stampForwardedHeaders(req), false);
    assert.equal(req.headers["x-forwarded-for"], "10.0.0.7");
  });

  it("容忍链尾多余的逗号", () => {
    const req = fakeRequest({ headers: { "x-forwarded-for": "8.8.8.8, " }, remoteAddress: "10.0.0.7" });
    stampForwardedHeaders(req);
    assert.equal(req.headers["x-forwarded-for"], "8.8.8.8, 10.0.0.7");
  });

  it("补齐缺失的 x-forwarded-proto / -host / -port", () => {
    const req = fakeRequest({ headers: { host: "console.example.com" }, localPort: 3000 });
    stampForwardedHeaders(req);
    assert.equal(req.headers["x-forwarded-proto"], "http");
    assert.equal(req.headers["x-forwarded-host"], "console.example.com");
    assert.equal(req.headers["x-forwarded-port"], "3000");
  });

  it("已有的 x-forwarded-proto 不被覆盖", () => {
    // TLS 在入口终止：入口说 https，我们看到的却是明文 http。
    // 覆盖它会让后端拼出 http 开头的回跳地址。
    const req = fakeRequest({ headers: { "x-forwarded-proto": "https" }, encrypted: false });
    stampForwardedHeaders(req);
    assert.equal(req.headers["x-forwarded-proto"], "https");
  });

  it("上游用了 Forwarded 就续写，没用就不新建", () => {
    const withForwarded = fakeRequest({
      headers: { forwarded: "for=8.8.8.8;proto=https" },
      remoteAddress: "10.0.0.7"
    });
    stampForwardedHeaders(withForwarded);
    assert.equal(withForwarded.headers.forwarded, "for=8.8.8.8;proto=https, for=10.0.0.7");

    const without = fakeRequest({ remoteAddress: "10.0.0.7" });
    stampForwardedHeaders(without);
    assert.equal(without.headers.forwarded, undefined);
  });

  it("Forwarded 里的 IPv6 带方括号并加引号", () => {
    const req = fakeRequest({ headers: { forwarded: "for=8.8.8.8" }, remoteAddress: "2001:db8::1" });
    stampForwardedHeaders(req);
    assert.equal(req.headers.forwarded, 'for=8.8.8.8, for="[2001:db8::1]"');
  });

  it("拿不到对端地址时不动转发链", () => {
    const req = { headers: { "x-forwarded-for": "8.8.8.8" }, socket: { remoteAddress: undefined } };
    assert.equal(stampForwardedHeaders(req), false);
    assert.equal(req.headers["x-forwarded-for"], "8.8.8.8");
  });
});

describe("describeBackendHop", () => {
  const scopeOf = (origin) => describeBackendHop(origin).scope;

  it("同机与内网地址算内网", () => {
    assert.equal(scopeOf("http://127.0.0.1:8088"), "loopback");
    assert.equal(scopeOf("http://[::1]:8088"), "loopback");
    assert.equal(scopeOf("http://localhost:8088"), "loopback");
    assert.equal(scopeOf("http://10.42.0.7:8088"), "private");
    assert.equal(scopeOf("http://192.168.1.9:8088"), "private");
    assert.equal(scopeOf("http://100.64.1.1:8088"), "private"); // CGNAT，多数 PaaS 的容器网段
  });

  it("只可能在内部网络解析的主机名算内网", () => {
    assert.equal(scopeOf("http://aegis-api-git.zeabur.internal:8088"), "internal-name");
    assert.equal(scopeOf("http://aegis-api:8088"), "internal-name"); // 裸服务名
    assert.equal(scopeOf("http://api.default.svc.cluster.local:8088"), "internal-name");
  });

  it("公网地址就是公网地址", () => {
    // 这一档正是线上踩到的：控制台反代绕出公网再回来，
    // 边缘写下的是控制台自己的出口地址，浏览器地址被它挡在后面。
    assert.equal(scopeOf("https://aegis-api.zeabur.app"), "public");
    assert.equal(scopeOf("https://aegis.karpov.cn"), "public");
    assert.equal(scopeOf("http://117.152.185.89:8088"), "public");
  });

  it("没配就不下结论", () => {
    assert.equal(scopeOf(""), "unset");
    assert.equal(scopeOf(undefined), "unset");
    assert.equal(scopeOf("这不是个地址"), "unknown");
  });

  it("只有公网那一档才告警", () => {
    assert.match(backendHopWarning("https://aegis-api.zeabur.app"), /公网地址/);
    for (const ok of ["http://127.0.0.1:8088", "http://aegis-api:8088", "", "这不是个地址"]) {
      assert.equal(backendHopWarning(ok), "");
    }
  });
});

describe("installOnServer", () => {
  const servers = [];

  after(() => {
    for (const server of servers) server.close();
  });

  /** 起一个装好追加逻辑的服务器，返回端口与「处理器看到的请求头」。 */
  async function listen(onRequest, onUpgrade) {
    const server = installOnServer(http.createServer(onRequest));
    if (onUpgrade) server.on("upgrade", onUpgrade);
    servers.push(server);
    await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
    return server.address().port;
  }

  it("普通请求：处理器看到的链末尾是直连对端", async () => {
    let seen;
    const port = await listen((req, res) => {
      seen = req.headers["x-forwarded-for"];
      res.end("ok");
    });

    await fetch(`http://127.0.0.1:${port}/api/ping`, { headers: { "x-forwarded-for": "8.8.8.8" } });
    assert.equal(seen, "8.8.8.8, 127.0.0.1");
  });

  it("WebSocket 升级：同样被追加", async () => {
    // /api/ws 是实时事件的通道，它和普通请求走的是 server 上两个不同的事件。
    // 只覆盖 request 的话，实时链路上的 IP 会静默退回控制台自己的地址。
    let seen;
    const port = await listen(
      (_req, res) => res.end("ok"),
      (req, socket) => {
        seen = req.headers["x-forwarded-for"];
        // 不接受这次升级，直接断开：这里要验的是「握手请求被追加了没有」，
        // 不是 WebSocket 协议本身。
        socket.destroy();
      }
    );

    await new Promise((resolve) => {
      const socket = net.connect(port, "127.0.0.1", () => {
        socket.write(
          "GET /api/ws HTTP/1.1\r\n" +
            `Host: 127.0.0.1:${port}\r\n` +
            "Connection: Upgrade\r\n" +
            "Upgrade: websocket\r\n" +
            "X-Forwarded-For: 8.8.8.8\r\n\r\n"
        );
      });
      // 对端断开在 Windows 上报的是 ECONNRESET 而不是干净的 FIN，
      // 两种都算「这一轮结束了」。
      const done = () => {
        socket.destroy();
        resolve();
      };
      socket.on("close", done);
      socket.on("error", done);
    });

    assert.equal(seen, "8.8.8.8, 127.0.0.1");
  });
});

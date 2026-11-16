"""/healthz、/readyz、/metrics（Prometheus 文本格式），默认监听 :8099。"""

from __future__ import annotations

import threading
import time
from collections.abc import Callable
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Metrics:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self.jobs: dict[str, int] = {}
        self.train_seconds_sum = 0.0
        self.train_seconds_count = 0
        self.published = 0
        self.last_success = 0.0
        self.running = 0
        self.consumer_ready = False
        self.started = time.time()

    def job(self, result: str, seconds: float | None = None, published: bool = False) -> None:
        with self._lock:
            self.jobs[result] = self.jobs.get(result, 0) + 1
            if seconds is not None:
                self.train_seconds_sum += seconds
                self.train_seconds_count += 1
            if published:
                self.published += 1
            if result == "succeeded":
                self.last_success = time.time()

    def render(self) -> str:
        with self._lock:
            lines = [
                "# HELP music_deep_jobs_total 处理过的深度模型训练任务数（按结果）",
                "# TYPE music_deep_jobs_total counter",
            ]
            for k in sorted(set(self.jobs) | {"succeeded", "failed", "skipped"}):
                lines.append(f'music_deep_jobs_total{{result="{k}"}} {self.jobs.get(k, 0)}')
            lines += [
                "# HELP music_deep_train_seconds 单次训练耗时（秒）",
                "# TYPE music_deep_train_seconds summary",
                f"music_deep_train_seconds_sum {self.train_seconds_sum:.3f}",
                f"music_deep_train_seconds_count {self.train_seconds_count}",
                "# HELP music_deep_published_total 自动发布次数",
                "# TYPE music_deep_published_total counter",
                f"music_deep_published_total {self.published}",
                "# HELP music_deep_running 正在训练的任务数",
                "# TYPE music_deep_running gauge",
                f"music_deep_running {self.running}",
                "# HELP music_deep_last_success_timestamp_seconds 最近一次训练成功的时间",
                "# TYPE music_deep_last_success_timestamp_seconds gauge",
                f"music_deep_last_success_timestamp_seconds {self.last_success:.0f}",
                "# HELP music_deep_consumer_ready 消费者是否就绪",
                "# TYPE music_deep_consumer_ready gauge",
                f"music_deep_consumer_ready {1 if self.consumer_ready else 0}",
            ]
        return "\n".join(lines) + "\n"


def serve(host: str, port: int, metrics: Metrics, ready: Callable[[], bool]) -> ThreadingHTTPServer:
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:  # noqa: N802
            path = self.path.split("?")[0]
            if path == "/healthz":
                self._send(200, "ok\n")
            elif path == "/readyz":
                ok = False
                try:
                    ok = ready()
                except Exception:  # noqa: BLE001
                    ok = False
                self._send(200 if ok else 503, "ok\n" if ok else "not ready\n")
            elif path == "/metrics":
                self._send(200, metrics.render(), "text/plain; version=0.0.4; charset=utf-8")
            else:
                self._send(404, "not found\n")

        def _send(self, code: int, body: str, ctype: str = "text/plain; charset=utf-8") -> None:
            data = body.encode("utf-8")
            self.send_response(code)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def log_message(self, *_args: object) -> None:  # 探针太频繁，不记访问日志
            pass

    srv = ThreadingHTTPServer((host, port), Handler)
    srv.daemon_threads = True
    threading.Thread(target=srv.serve_forever, name="metrics-http", daemon=True).start()
    return srv

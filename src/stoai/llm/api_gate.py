"""APICallGate — rate-limiting gate for LLM API calls.

Gate model: a gate thread controls WHEN calls proceed (timing),
a thread pool executes them (concurrency). Multiple calls can be
in-flight simultaneously as long as the RPM window has capacity.
"""
from __future__ import annotations

import concurrent.futures
import queue
import threading
import time
from collections import deque
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from typing import Any, Callable


@dataclass
class _WorkItem:
    fn: Callable[[], Any]
    future: concurrent.futures.Future


class APICallGate:
    """Rate-limiting gate for API calls.

    Args:
        max_rpm: Maximum requests per minute. Must be > 0.
        pool_size: Thread pool size for executing calls. Default: max_rpm // 3,
                   clamped to [2, 32].
    """

    def __init__(self, max_rpm: int, pool_size: int | None = None):
        if max_rpm <= 0:
            raise ValueError(f"max_rpm must be > 0, got {max_rpm}")
        self._max_rpm = max_rpm
        self._timestamps: deque[float] = deque()
        self._queue: queue.Queue[_WorkItem | None] = queue.Queue()
        self._stop = threading.Event()
        effective_pool = pool_size or max(2, min(32, max_rpm // 3))
        self._pool = ThreadPoolExecutor(max_workers=effective_pool)
        self._gate_thread = threading.Thread(
            target=self._gate_loop, daemon=True, name="api-gate"
        )
        self._gate_thread.start()

    def submit(self, fn: Callable[[], Any]) -> Any:
        """Submit an API call through the gate. Blocks until result."""
        if self._stop.is_set():
            raise RuntimeError("Gate is shut down")
        future: concurrent.futures.Future = concurrent.futures.Future()
        self._queue.put(_WorkItem(fn=fn, future=future))
        return future.result()

    def shutdown(self) -> None:
        """Shut down the gate. Pending items get RuntimeError."""
        self._stop.set()
        self._queue.put(None)  # unblock gate thread
        self._gate_thread.join(timeout=5.0)
        # Drain remaining items
        while True:
            try:
                item = self._queue.get_nowait()
            except queue.Empty:
                break
            if item is not None:
                item.future.set_exception(RuntimeError("Gate shut down"))
        self._pool.shutdown(wait=False)

    def _gate_loop(self) -> None:
        """Gate thread: controls timing, submits to pool."""
        while not self._stop.is_set():
            try:
                item = self._queue.get(timeout=1.0)
            except queue.Empty:
                continue
            if item is None:
                break  # shutdown sentinel

            # Prune old timestamps
            now = time.monotonic()
            while self._timestamps and self._timestamps[0] <= now - 60.0:
                self._timestamps.popleft()

            # Wait if RPM window is full
            while len(self._timestamps) >= self._max_rpm and not self._stop.is_set():
                wait_until = self._timestamps[0] + 60.0
                delay = max(0, wait_until - time.monotonic())
                if delay > 0:
                    self._stop.wait(timeout=delay)
                # Re-prune after waking
                now = time.monotonic()
                while self._timestamps and self._timestamps[0] <= now - 60.0:
                    self._timestamps.popleft()

            if self._stop.is_set():
                item.future.set_exception(RuntimeError("Gate shut down"))
                break

            # Record timestamp and dispatch
            self._timestamps.append(time.monotonic())
            self._pool.submit(self._execute, item)

    @staticmethod
    def _execute(item: _WorkItem) -> None:
        """Run in pool thread. Always resolves the future."""
        try:
            result = item.fn()
            item.future.set_result(result)
        except BaseException as exc:
            item.future.set_exception(exc)

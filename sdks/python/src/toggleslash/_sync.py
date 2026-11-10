"""Keeping one ruleset current: the first load, the stream, the poll, the backoff and `Retry-After`
(contract 7 and 8). Nothing here raises at a caller; every failure is recorded in the status, handed to
`report` and leaves the last good ruleset in place."""

from __future__ import annotations

import threading
from collections.abc import Callable
from datetime import datetime
from typing import Any

from ._http import RULESET_ROUTE, STREAM_ROUTE, Aborted, Aborter, Transport
from ._json import loads
from ._retry import retry_after
from ._ruleset import Ruleset, create_ruleset
from ._seams import Seams
from ._sse import SseParser
from ._types import BAD_RULESET, NETWORK, ClientStatus, StreamState, ToggleSlashError

BACKOFF_BASE = 1.0


class Snapshot:
    """A ruleset and the ETag it came with, replaced as one object so a reader never sees them apart."""

    __slots__ = ("etag", "ruleset")

    def __init__(self, ruleset: Ruleset, etag: str | None) -> None:
        self.ruleset = ruleset
        self.etag = etag


class _Failure(ToggleSlashError):
    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class Sync:
    def __init__(
        self,
        transport: Transport,
        sdk_key: str,
        *,
        stream: bool,
        poll_interval: float,
        request_timeout: float,
        report: Callable[[BaseException], None],
        stop: threading.Event,
        seams: Seams,
    ) -> None:
        self._transport = transport
        self._auth = f"Bearer {sdk_key}"
        self._stream_on = stream
        self._poll = poll_interval
        self._timeout = request_timeout
        self._report = report
        self._stop = stop
        self._monotonic = seams.monotonic
        self._wall = seams.wall
        self._wait = seams.wait or stop.wait
        self._jitter = seams.jitter
        self._watchdog = seams.watchdog
        self._state_lock = threading.Lock()
        self._held: Snapshot | None = None
        self._last_sync_at: datetime | None = None
        self._last_error: str | None = None
        self._stream = StreamState.CONNECTING if stream else StreamState.OFF
        self._closed = False
        self._retry_at = 0.0  # a 429's Retry-After on the monotonic clock
        self._inflight_lock = threading.Lock()
        self._inflight: set[Aborter] = set()
        self._aborting = False
        self._first_load = threading.Event()
        self._ready = False  # what the first attempt left, fixed before `_first_load` is set
        self._thread = threading.Thread(target=self._run, name="toggleslash-sync", daemon=True)

    # -- what the client reads ----------------------------------------------------------------------------

    @property
    def held(self) -> Snapshot | None:
        """The ruleset held. One attribute read, so it is the ruleset or its replacement, whole."""
        return self._held

    def status(self) -> ClientStatus:
        with self._state_lock:
            held = self._held
            return ClientStatus(
                etag=held.etag if held is not None else None,
                last_sync_at=self._last_sync_at,
                stream=self._stream,
                last_error=self._last_error,
            )

    def ready(self) -> bool:
        self._first_load.wait()
        return self._ready

    # -- lifecycle ----------------------------------------------------------------------------------------

    def start(self) -> None:
        try:
            self._thread.start()
        except RuntimeError as error:  # no thread could be made: nothing will ever load
            self._first_load.set()
            self._report(error)

    def begin_close(self) -> None:
        """Stops following the server and abandons the requests in flight. Does not wait for the thread."""
        with self._state_lock:
            self._closed = True
            self._stream = StreamState.CLOSED
        with self._inflight_lock:
            self._aborting = True
            inflight = list(self._inflight)
        for aborter in inflight:
            aborter.abort()

    def join(self) -> None:
        if self._thread.is_alive() and self._thread is not threading.current_thread():
            self._thread.join()
        self._first_load.set()

    def _register(self, aborter: Aborter) -> None:
        with self._inflight_lock:
            self._inflight.add(aborter)
            aborting = self._aborting
        if aborting:
            aborter.abort()

    def _unregister(self, aborter: Aborter) -> None:
        aborter.stop_timer()
        with self._inflight_lock:
            self._inflight.discard(aborter)

    # -- the loop (7.3) -----------------------------------------------------------------------------------

    def _run(self) -> None:
        try:
            self._refresh()
            self._ready = self._held is not None
            self._first_load.set()
            self._loop()
        except BaseException as error:
            self._report(error)
        finally:
            self._first_load.set()

    def _loop(self) -> None:
        failures = 0
        while not self._stop.is_set():
            if self._stream_on and not self._wait_for(0.0):
                return
            connected = self._follow() if self._stream_on else False
            failures = 0 if connected else failures + 1
            if not self._wait_for(self._delay(failures)):
                return
            if not connected:
                self._refresh()

    def _delay(self, failures: int) -> float:
        if not self._stream_on:
            return self._poll
        ceiling = min(self._poll, BACKOFF_BASE * 2 ** min(failures, 16))
        return ceiling * (0.5 + self._jitter() / 2)

    def _wait_for(self, seconds: float) -> bool:
        """Waits `seconds`, or until a Retry-After hold has passed if that is later. False when stopped."""
        with self._state_lock:
            remaining = self._retry_at - self._monotonic()
        wait = max(seconds, remaining)
        if wait > 0 and self._wait(wait):
            return False
        return not self._stop.is_set()

    # -- state --------------------------------------------------------------------------------------------

    def _set_stream(self, state: StreamState) -> None:
        with self._state_lock:
            if not self._closed:
                self._stream = state

    def _synced(self) -> None:
        with self._state_lock:
            if not self._closed:
                self._last_sync_at = self._wall()
                self._last_error = None

    def _hold_for(self, reply: Any) -> None:
        wait = retry_after(reply.status, reply.header("Retry-After"))
        if wait is not None:
            with self._state_lock:
                self._retry_at = max(self._retry_at, self._monotonic() + wait)

    def _failed(self, error: BaseException) -> None:
        """Records a failed ruleset or stream attempt. A failure `close` caused is neither recorded nor reported."""
        if isinstance(error, Aborted):
            return
        code = getattr(error, "code", NETWORK)
        with self._state_lock:
            if self._closed:
                return
            self._last_error = code
        self._report(error)

    # -- one ruleset request (7.1) ------------------------------------------------------------------------

    def _refresh(self) -> bool:
        headers = {"Authorization": self._auth}
        held = self._held
        if held is not None and held.etag is not None:
            headers["If-None-Match"] = held.etag
        aborter = Aborter(self._timeout)
        self._register(aborter)
        try:
            reply = self._transport("GET", RULESET_ROUTE, headers, None, aborter)
            try:
                if reply.status == 304:
                    self._synced()
                    return True
                if reply.status != 200:
                    self._hold_for(reply)
                    raise _Failure(f"http_{reply.status}", f"GET {RULESET_ROUTE} answered {reply.status}")
                body = reply.read_body()
                etag = reply.header("ETag")
            finally:
                reply.close()
            ruleset = _parse(body)
            with self._state_lock:
                if self._closed:
                    return False
                self._held = Snapshot(ruleset, etag)
                self._last_sync_at = self._wall()
                self._last_error = None
            return True
        except Exception as error:
            self._failed(error)
            return False
        finally:
            self._unregister(aborter)

    # -- the stream (7.2) ---------------------------------------------------------------------------------

    def _follow(self) -> bool:
        """Follows the stream until it ends or fails. True if it connected and no fetch of its own failed."""
        self._set_stream(StreamState.CONNECTING)
        aborter = Aborter(self._timeout)
        self._register(aborter)
        connected = False
        try:
            reply = self._transport(
                "GET", STREAM_ROUTE, {"Authorization": self._auth, "Accept": "text/event-stream"}, None, aborter
            )
            try:
                if reply.status != 200 or reply.no_body:
                    self._hold_for(reply)
                    raise _Failure(f"http_{reply.status}", f"GET {STREAM_ROUTE} answered {reply.status}")
                aborter.stop_timer()
                parser = SseParser()
                while True:
                    chunk = reply.read_chunk(self._watchdog)
                    if not chunk:
                        return connected
                    for name, data in parser.feed(chunk):
                        if name == "connected":
                            connected = True
                            self._set_stream(StreamState.CONNECTED)
                            self._clear_error()
                        elif name != "changed":
                            continue
                        if not self._on_tag(data):
                            # A stream that stays up would never retry the fetch it asked for.
                            return False
            finally:
                reply.close()
        except Exception as error:
            self._failed(error)
            return False
        finally:
            self._unregister(aborter)
            self._set_stream(StreamState.DISCONNECTED)

    def _clear_error(self) -> None:
        with self._state_lock:
            if not self._closed:
                self._last_error = None

    def _on_tag(self, tag: str) -> bool:
        held = self._held
        if held is not None and held.etag == tag:
            self._synced()
            return True
        return self._refresh()


def _parse(body: bytes) -> Ruleset:
    try:
        data = loads(body)
        if not (
            isinstance(data, dict) and isinstance(data.get("flags"), list) and isinstance(data.get("segments"), list)
        ):
            raise ValueError("not a ruleset")
        return create_ruleset(data)
    except Exception as error:
        raise _Failure(BAD_RULESET, f"GET {RULESET_ROUTE} sent something that is not a ruleset") from error

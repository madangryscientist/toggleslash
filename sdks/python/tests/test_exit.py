"""A client the program never closes does not keep the process alive."""

import os
import subprocess
import sys
import textwrap
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

CHILD = textwrap.dedent(
    """
    import sys
    sys.path.insert(0, "tests")
    from fakes import FakeServer, flag, ruleset, serve_ruleset, stream_route, sse
    from toggleslash import create_client

    server = FakeServer()
    serve_ruleset(server, ruleset(flag("f", [False, True], serve=1)))
    server.route("GET", "/sdk/v1/stream", stream_route([sse("connected")], hold=True))
    server.route("POST", "/sdk/v1/events", lambda r, o: o.send(204))
    client = create_client(url=server.url, sdk_key="k", stream={stream}, flush_interval=0.05)
    assert client.ready()
    assert client.bool_variation("f", {{"key": "u"}}, False) is True
    print("done", flush=True)
    # no close(), and the server is left running too
    """
)


class ExitTests(unittest.TestCase):
    def run_child(self, stream: bool) -> None:
        env = dict(os.environ, PYTHONPATH=str(ROOT / "src"))
        try:
            done = subprocess.run(
                [sys.executable, "-X", "dev", "-W", "error", "-c", CHILD.format(stream=stream)],
                cwd=ROOT,
                env=env,
                capture_output=True,
                text=True,
                timeout=30,
                check=False,
            )
        except subprocess.TimeoutExpired as error:
            self.fail(f"the process did not exit with an unclosed client: {error.stdout!r}")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertIn("done", done.stdout)

    def test_an_unclosed_client_with_the_stream_on_does_not_block_exit(self) -> None:
        self.run_child(True)

    def test_an_unclosed_client_with_the_stream_off_does_not_block_exit(self) -> None:
        self.run_child(False)


if __name__ == "__main__":
    unittest.main()

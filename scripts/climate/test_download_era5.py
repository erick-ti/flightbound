# /// script
# requires-python = ">=3.10"
# dependencies = ["cdsapi==0.7.7"]
# ///
"""Offline tests for download_era5.py. Every request goes to a local server
that records it and answers 503 Service Unavailable unless a test sets
another answer, with a made-up key, so nothing can reach the Climate Data
Store.

    uv run scripts/climate/test_download_era5.py
"""

import http.server
import os
import subprocess
import sys
import tempfile
import threading
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "download_era5.py"
REPO = SCRIPT.parents[2]
DATASET = "reanalysis-era5-land-timeseries"
# A key without a colon takes cdsapi's path for the current Data Stores.
KEY = "00000000-0000-0000-0000-000000000000"
# Where the client checks a token and lists the account's accepted licences.
VERIFY = "/api/profiles/v1/account/verification/pat"
LICENCES = "/api/profiles/v1/account/licences"
REFUSED = (401, b'{"title": "Authentication failed", "detail": "operation not allowed"}')


class FakeServer:
    """Records each request's method and path, and answers it from answers,
    keyed by method and path, or else with 503, which cdsapi's HTTP layer
    retries unless told not to."""

    def __init__(self):
        self.requests = []
        self.answers = {}
        lock = threading.Lock()
        server = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def answer(self):
                with lock:
                    server.requests.append((self.command, self.path))
                length = int(self.headers.get("Content-Length") or 0)
                if length:
                    self.rfile.read(length)
                key = (self.command, self.path.split("?")[0])
                status, body = server.answers.get(key, (503, b'{"title": "unavailable"}'))
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            do_GET = do_POST = do_PUT = do_DELETE = do_HEAD = answer

            def log_message(self, format, *args):
                pass

        self.httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.httpd.server_address[1]}/api"
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()

    @property
    def count(self):
        return len(self.requests)

    def repeated(self):
        return sorted({r for r in self.requests if self.requests.count(r) > 1})

    def close(self):
        self.httpd.shutdown()
        self.httpd.server_close()


class DownloadTest(unittest.TestCase):
    def setUp(self):
        self.server = FakeServer()
        self.addCleanup(self.server.close)
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.tmp = Path(tmp.name)
        self.out = self.tmp / "out"
        rc = self.tmp / "cdsapirc"
        rc.write_text("")
        # Only the local server, a made-up key and an empty config: no proxy,
        # no real configuration file, no real home directory.
        env = {k: v for k, v in os.environ.items() if not k.lower().endswith("_proxy") and not k.startswith("CDSAPI_")}
        env.update(
            CDSAPI_URL=self.server.url,
            CDSAPI_KEY=KEY,
            CDSAPI_RC=str(rc),
            HOME=str(self.tmp),
            NO_PROXY="127.0.0.1,localhost",
            no_proxy="127.0.0.1,localhost",
        )
        self.env = env

    def run_script(self, *args):
        return subprocess.run(
            [sys.executable, "-I", str(SCRIPT), *args],
            env=self.env,
            capture_output=True,
            text=True,
            timeout=120,
        )

    def ledger(self):
        path = self.out / "requests.log"
        return path.read_text().splitlines() if path.exists() else []

    def test_a_failure_is_not_retried_and_stops_the_run(self):
        r = self.run_script("--out", str(self.out), "--place", "tokyo", "--place", "lisbon", "--max-requests", "5")
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.server.repeated(), [], self.server.requests)
        # The service messages read when the client starts, then the first
        # job's first call, which fails and ends the run.
        self.assertEqual(self.server.count, 2, self.server.requests)
        self.assertEqual(len(self.ledger()), 1)
        self.assertEqual(sorted(p.name for p in self.out.iterdir()), ["requests.log"])

    def test_the_server_sees_retries_when_they_happen(self):
        # Positive control: with retries left on, the same server counts
        # more than one request, so the test above would catch a retry.
        import cdsapi

        old = {k: os.environ.get(k) for k in ("NO_PROXY", "no_proxy")}
        os.environ.update(NO_PROXY="127.0.0.1,localhost", no_proxy="127.0.0.1,localhost")
        try:
            client = cdsapi.Client(url=self.server.url, key=KEY, retry_max=3, sleep_max=0, quiet=True)
            with self.assertRaises(Exception):
                client.retrieve(DATASET, {"variable": ["2m_temperature"]}, str(self.tmp / "x.csv"))
        finally:
            for k, v in old.items():
                if v is None:
                    os.environ.pop(k, None)
                else:
                    os.environ[k] = v
        self.assertNotEqual(self.server.repeated(), [], self.server.requests)

    def test_the_cap_counts_earlier_runs(self):
        self.out.mkdir()
        (self.out / "requests.log").write_text("2026-10-11T00:00:00Z tokyo\n2026-10-11T00:01:00Z tokyo\n")
        r = self.run_script("--out", str(self.out), "--place", "tokyo", "--max-requests", "2")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertEqual(self.server.count, 0)
        self.assertEqual(len(self.ledger()), 2)

    def test_finished_places_are_skipped(self):
        self.out.mkdir()
        (self.out / "tokyo.zip").write_text("saved")
        r = self.run_script("--out", str(self.out), "--place", "tokyo", "--max-requests", "5")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertEqual(self.server.count, 0)
        self.assertEqual(self.ledger(), [])

    def test_a_dry_run_sends_nothing(self):
        r = self.run_script("--out", str(self.out), "--place", "tokyo", "--dry-run")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        # The request is for Tokyo's climate point, not its own coordinates.
        self.assertIn('"place": "tokyo"', r.stdout)
        self.assertIn('"dataset": "reanalysis-era5-land-timeseries"', r.stdout)
        self.assertIn('"location": {"longitude": 139.7, "latitude": 35.7}', r.stdout)
        self.assertEqual(self.server.count, 0)
        self.assertFalse(self.out.exists())

    def test_a_run_asks_about_the_era5_land_dataset(self):
        r = self.run_script("--out", str(self.out), "--place", "lisbon", "--max-requests", "1")
        self.assertNotEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn(("GET", f"/api/retrieve/v1/processes/{DATASET}"), self.server.requests)

    def test_out_must_be_outside_the_repository(self):
        inside = REPO / "era5-test-output"
        r = self.run_script("--out", str(inside), "--max-requests", "5")
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertEqual(self.server.count, 0)
        self.assertFalse(inside.exists())

    def test_unknown_places_are_refused(self):
        r = self.run_script("--out", str(self.out), "--place", "atlantis", "--max-requests", "5")
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertEqual(self.server.count, 0)

    def test_max_requests_is_required(self):
        r = self.run_script("--out", str(self.out), "--place", "tokyo")
        self.assertEqual(r.returncode, 2, r.stdout + r.stderr)
        self.assertEqual(self.server.count, 0)

    def jobs(self):
        return [r for r in self.server.requests if r[1].startswith("/api/retrieve/")]

    def test_check_token_reports_a_refused_token_without_a_job(self):
        self.server.answers[("POST", VERIFY)] = REFUSED
        r = self.run_script("--check-token")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("401", r.stderr)
        self.assertNotIn("Traceback", r.stderr)
        self.assertEqual(self.server.requests.count(("POST", VERIFY)), 1, self.server.requests)
        self.assertEqual(self.jobs(), [])
        self.assertFalse(self.out.exists())

    def test_check_token_lists_the_accepted_licences(self):
        self.server.answers[("POST", VERIFY)] = (200, b"{}")
        self.server.answers[("GET", LICENCES)] = (200, b'{"licences": [{"id": "cc-by", "revision": 1}]}')
        r = self.run_script("--check-token")
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("accepted the token", r.stdout)
        self.assertIn("cc-by", r.stdout)
        self.assertEqual(self.jobs(), [])

    def test_check_token_needs_the_dataset_licence(self):
        self.server.answers[("POST", VERIFY)] = (200, b"{}")
        self.server.answers[("GET", LICENCES)] = (200, b'{"licences": [{"id": "other", "revision": 3}]}')
        r = self.run_script("--check-token")
        self.assertEqual(r.returncode, 1, r.stdout + r.stderr)
        self.assertIn("accepted the token", r.stdout)
        self.assertIn("cc-by", r.stderr)
        self.assertEqual(self.jobs(), [])

    def test_check_token_messages_keep_their_order_in_a_pipe(self):
        # Seen with a real account: buffered stdout put the licence hint
        # ahead of the line saying the token was accepted.
        self.server.answers[("POST", VERIFY)] = (200, b"{}")
        self.server.answers[("GET", LICENCES)] = (200, b'{"licences": [{"id": "other", "revision": 3}]}')
        r = subprocess.run(
            [sys.executable, "-I", str(SCRIPT), "--check-token"],
            env=self.env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=120,
        )
        out = r.stdout
        self.assertLess(out.index("accepted the token"), out.index("accept the dataset's licence"), out)

    def test_check_token_takes_no_other_options(self):
        for extra in (["--out", str(self.out)], ["--place", "tokyo"], ["--max-requests", "5"], ["--dry-run"]):
            r = self.run_script("--check-token", *extra)
            self.assertEqual(r.returncode, 2, f"{extra}: {r.stdout}{r.stderr}")
        self.assertEqual(self.server.count, 0)

    def test_a_missing_configuration_sends_nothing(self):
        del self.env["CDSAPI_URL"], self.env["CDSAPI_KEY"]
        for args in (["--check-token"], ["--out", str(self.out), "--place", "tokyo", "--max-requests", "5"]):
            r = self.run_script(*args)
            self.assertEqual(r.returncode, 1, f"{args}: {r.stdout}{r.stderr}")
            self.assertIn("configuration", r.stderr)
            self.assertNotIn("Traceback", r.stderr)
        self.assertEqual(self.server.count, 0)
        self.assertEqual(self.ledger(), [])


if __name__ == "__main__":
    unittest.main()

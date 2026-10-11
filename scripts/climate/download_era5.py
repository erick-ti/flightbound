# /// script
# requires-python = ">=3.10"
# dependencies = ["cdsapi==0.7.7"]
# ///
"""Download ERA5-Land hourly 2 m temperature and total precipitation for each
supported place, as input for the climate normals converter
(backend/cmd/climatenormals). Each request is for the place's climate point
(climate_latitude and climate_longitude in places.json), the nearest grid
point that is mostly land. Each download is saved as the service sends it,
a zip archive holding two CSV files, named <id>.zip.

Needs a Copernicus Climate Data Store account that has accepted the licence
of the "ERA5 Land hourly time-series data" dataset. cdsapi reads the
account's token from ~/.cdsapirc (or CDSAPI_URL and CDSAPI_KEY); this script
never reads or prints it.

    uv run scripts/climate/download_era5.py --check-token
    uv run scripts/climate/download_era5.py --out ~/era5-land --max-requests 40

--check-token asks the service whether it accepts the token and lists the
licences the account has accepted, without submitting a job.

Each place is one job: the client reads the dataset's description, submits
the job, checks on it and downloads the result. It also reads the service's
messages once when it starts. Before a job is submitted it is added to
requests.log in the output directory, and --max-requests caps the jobs in
that log across runs, so interrupted attempts count too. The client never
retries: the first failure stops the run. Run it again to continue; places
with a finished file are skipped.
"""

import argparse
import datetime
import io
import json
import os
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
PLACES = REPO / "backend" / "internal" / "places" / "places.json"
DATASET = "reanalysis-era5-land-timeseries"
# One UTC day either side of 1991-2020, so every local day in the period
# has all of its hours.
DATES = "1990-12-31/2021-01-01"
# The licence the dataset's catalogue entry names.
LICENCE = "cc-by"
LEDGER = "requests.log"


def job(place):
    return {
        "variable": ["2m_temperature", "total_precipitation"],
        "location": {"longitude": place["climate_longitude"], "latitude": place["climate_latitude"]},
        "date": [DATES],
        "data_format": "csv",
    }


def parse_args(argv):
    p = argparse.ArgumentParser(description=(__doc__ or "").split("\n\n")[0])
    p.add_argument("--out", type=Path, help="directory for the downloads, outside the repository")
    p.add_argument("--max-requests", type=int, help="most requests requests.log may ever hold")
    p.add_argument("--place", action="append", default=[], metavar="ID", help="only this place (repeatable)")
    p.add_argument("--dry-run", action="store_true", help="print the requests without sending them")
    p.add_argument("--check-token", action="store_true", help="check the token and licences without submitting a job")
    args = p.parse_args(argv)
    if args.check_token:
        if args.out is not None or args.max_requests is not None or args.place or args.dry_run:
            p.error("--check-token takes no other options")
        return args
    if args.out is None:
        p.error("--out is required")
    if args.max_requests is None and not args.dry_run:
        p.error("--max-requests is required unless --dry-run is given")
    if args.max_requests is not None and args.max_requests < 0:
        p.error("--max-requests cannot be negative")
    return args


def make_client():
    """Returns the CDS client, or None after saying why it cannot start."""
    # Created only when needed, since creating it reads the service's
    # messages. retry_max=1 gives every HTTP call one attempt. Never set
    # debug=True: cdsapi then logs the key.
    import cdsapi

    try:
        return cdsapi.Client(retry_max=1, debug=False)
    except Exception as e:  # such as a missing or incomplete ~/.cdsapirc
        print(f"cannot start the CDS client: {e}", file=sys.stderr)
        return None


def check_token():
    client = make_client()
    if client is None:
        return 1
    try:
        client.client.check_authentication()
    except Exception as e:
        print(f"token check failed: {e}", file=sys.stderr)
        return 1
    print("the CDS accepted the token")
    try:
        licences = client.client.get_accepted_licences()
    except Exception as e:
        print(f"could not list the accepted licences: {e}", file=sys.stderr)
        return 1
    ids = sorted(str(entry.get("id", entry)) for entry in licences)
    print(f"licences accepted on this account: {', '.join(ids) or 'none'}")
    if LICENCE not in ids:
        print(f"accept the dataset's licence ({LICENCE}) on its Download tab before downloading", file=sys.stderr)
        return 1
    return 0


def main(argv=None):
    # Line by line, so that in a pipe stdout keeps its place among stderr.
    if isinstance(sys.stdout, io.TextIOWrapper):
        sys.stdout.reconfigure(line_buffering=True)
    args = parse_args(argv)
    if args.check_token:
        return check_token()
    places = json.loads(PLACES.read_text(encoding="utf-8"))
    known = {p["id"] for p in places}
    unknown = [i for i in args.place if i not in known]
    if unknown:
        print(f"unknown place: {', '.join(unknown)}", file=sys.stderr)
        return 2
    if args.place:
        places = [p for p in places if p["id"] in args.place]

    out = args.out.expanduser().resolve()
    if out == REPO or out.is_relative_to(REPO):
        print(f"--out must be outside the repository: {out}", file=sys.stderr)
        return 2
    ledger = out / LEDGER
    used = len(ledger.read_text(encoding="utf-8").splitlines()) if ledger.exists() else 0
    pending = [p for p in places if not (out / f"{p['id']}.zip").exists()]

    if args.dry_run:
        for p in pending:
            print(json.dumps({"place": p["id"], "dataset": DATASET, "request": job(p)}))
        print(f"{len(pending)} request(s) to send; {used} already in {ledger}")
        return 0

    out.mkdir(parents=True, exist_ok=True)
    client = None
    for p in pending:
        if used >= args.max_requests:
            print(f"stopped: requests.log holds {used} of the {args.max_requests} allowed", file=sys.stderr)
            return 1
        if client is None:
            # Only once a job will be sent, so dry runs and finished places
            # cost no request.
            client = make_client()
            if client is None:
                return 1
        stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        with ledger.open("a", encoding="utf-8") as fh:
            fh.write(f"{stamp} {p['id']}\n")
            fh.flush()
            os.fsync(fh.fileno())
        used += 1
        target = out / f"{p['id']}.zip"
        partial = out / f".{p['id']}.zip.part"
        try:
            client.retrieve(DATASET, job(p), str(partial))
        except Exception as e:  # any failure ends the run; nothing is retried
            partial.unlink(missing_ok=True)
            print(f"{p['id']}: request failed: {e}", file=sys.stderr)
            return 1
        os.replace(partial, target)
        print(f"{p['id']}: saved {target} ({target.stat().st_size} bytes)")
    print(f"done: requests.log holds {used} request(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

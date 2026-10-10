# flightbound

## Development

Requirements: Go 1.27 and Node.js 24.

Start the API. It listens on `127.0.0.1:8080`; pass `-addr` to change that.

```sh
cd backend
go run ./cmd/server
```

When an estimate is in a currency other than the comparison currency, the API downloads the European Central Bank's daily reference rates (`-fx-rates-url` changes the address). To work offline, give it a saved copy of the rates file instead, such as the one the checks use:

```sh
go run ./cmd/server -fx-rates-file internal/fxrates/testdata/eurofxref-daily.xml
```

In another terminal, start the web app, then open http://localhost:3000/compare.

```sh
cd frontend
npm ci
npm run dev
```

The web app forwards `/api/trip-comparisons` to the API at `API_ORIGIN` (default `http://127.0.0.1:8080`). `next dev` reads it at startup; `next start` uses the value that was set when `next build` ran.

Run every check: Go formatting, vet, and tests with the race detector, both builds, and HTTP checks against both apps running on ports 18080 and 13000 (override with `SMOKE_API_PORT` and `SMOKE_WEB_PORT`).

```sh
make smoke
```

How the comparison works, including the API contract: [docs/trip-comparison.md](docs/trip-comparison.md).

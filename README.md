# flightbound

## Development

Requirements: Go 1.27 and Node.js 24.

Start the API. It listens on `127.0.0.1:8080`; pass `-addr` to change that.

```sh
cd backend
go run ./cmd/server
```

When an estimate or the budget is in a currency other than the comparison currency, the API downloads the European Central Bank's daily reference rates (`-fx-rates-url` changes the address). When a destination has a country, each comparison fetches that country's travel advice from GOV.UK. To work offline, give the API the saved copies the checks use instead; with them it contacts neither service, and countries without a saved copy show their advice as unavailable:

```sh
go run ./cmd/server -fx-rates-file internal/fxrates/testdata/eurofxref-daily.xml -travel-advice-dir internal/traveladvice/testdata
```

In another terminal, start the web app, then open http://localhost:3000/compare.

```sh
cd frontend
npm ci
npm run dev
```

The web app forwards `/api/trip-comparisons`, `/api/travel-advice/countries`, and `/api/places` to the API at `API_ORIGIN` (default `http://127.0.0.1:8080`). `next dev` reads it at startup; `next start` uses the value that was set when `next build` ran.

Typical weather for the supported places and the map's land, lakes, and borders (from Natural Earth) are saved in the repository, so neither app needs an outside service for them. Rebuilding the weather figures from Copernicus data is described in [docs/trip-comparison.md](docs/trip-comparison.md#places-and-typical-weather); it needs [uv](https://docs.astral.sh/uv/) and a free Climate Data Store account.

Run every check: Go formatting, vet, and tests with the race detector, both builds, then HTTP checks and browser tests of the map and trip links against both apps running on ports 18080 and 13000 (override with `SMOKE_API_PORT` and `SMOKE_WEB_PORT`).

```sh
make smoke
```

The browser tests use Playwright's headless Chromium. Install it once, and again after Playwright is updated:

```sh
cd frontend
npx playwright install --only-shell chromium
```

How the comparison works, including travel advice, typical weather, the map, and the API contract: [docs/trip-comparison.md](docs/trip-comparison.md).

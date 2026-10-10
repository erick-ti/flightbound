# Trip comparison

The `/compare` page compares two or three destinations for one trip. A traveler enters their own estimates, and the Go API at `POST /api/trip-comparisons` validates them and does all of the arithmetic. The page shows the results exactly as the API returns them.

Results are the traveler's own estimates. They are not live prices, supplier offers, or a complete trip budget: they cover flights and accommodation only.

## Inputs

Shared by every destination:

- Check-in and check-out dates, as calendar dates (`YYYY-MM-DD`).
- The number of travelers, from 1 to 20.
- One currency for every amount. Changing it does not convert anything.

For each destination:

- A name, 1 to 80 characters after surrounding spaces are trimmed.
- A round-trip flight estimate for the whole party.
- An accommodation estimate per night for the whole party.

Either estimate can be left blank.

## Calculation

```text
nights                 = calendar days from check-in to check-out (1 to 365)
stay estimate          = nights × nightly estimate
flight + stay estimate = flight estimate + stay estimate
```

Both estimates already cover the whole party, so the traveler count never multiplies them. Nights come from the calendar dates alone, so time zones and daylight saving changes cannot shift them. Past dates are accepted, since the server cannot know the traveler's local date.

## Unknown estimates and the lowest label

A blank estimate is unknown, which is different from zero; `0` is a valid known estimate. A destination with an unknown estimate still shows its known parts, but it has no flight + stay total and counts as incomplete.

The lowest-estimate label appears only when every destination is complete. Until then no destination is labeled, because an incomplete one could turn out to be cheaper. When complete destinations tie for the lowest total, each one is labeled as a joint lowest estimate.

## Money

Amounts are exact. The API reads each amount as a decimal string and keeps it as an integer count of the currency's minor unit (cents, or whole yen), so nothing is rounded.

- Accepted: digits with an optional decimal point followed by at least one digit, such as `1250`, `1250.5`, or `0.99`.
- Rejected rather than rounded: more decimal places than the currency has (USD `12.345`, JPY `100.5`), signs, exponents, spaces, and separators such as `1,250`.
- At most 12 digits before the decimal point.

Results use the currency's exact number of decimal places, such as `975.85` or `74000`.

## Currencies

The supported currencies are the euro plus the currencies in the European Central Bank's daily euro reference rates as of 2026-10-09: AUD, BRL, CAD, CHF, CNY, CZK, DKK, EUR, GBP, HKD, HUF, IDR, ILS, INR, ISK, JPY, KRW, MXN, MYR, NOK, NZD, PHP, PLN, RON, SEK, SGD, THB, TRY, USD, and ZAR. Decimal places come from ISO 4217 List One (published 2026-09-17): JPY, ISK, and KRW have none, and the others have two.

The table lives in `backend/internal/money/money.go`. The page's currency menu in `frontend/app/compare/TripComparison.tsx` lists the same codes; the API rejects any code that is not in its table.

## API

`POST /api/trip-comparisons` takes a JSON body of at most 16 KiB. Amounts are strings, and `null` means unknown:

```json
{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "travelers": 2,
  "currency": "USD",
  "destinations": [
    { "label": "Lisbon", "flight_estimate": "600.10", "nightly_stay_estimate": "125.25" },
    { "label": "Porto", "flight_estimate": null, "nightly_stay_estimate": "90" }
  ]
}
```

A `200` response repeats the inputs in canonical form and adds the results:

```json
{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "nights": 3,
  "travelers": 2,
  "currency": "USD",
  "all_complete": false,
  "destinations": [
    {
      "label": "Lisbon",
      "flight_estimate": "600.10",
      "nightly_stay_estimate": "125.25",
      "stay_estimate": "375.75",
      "flight_and_stay_estimate": "975.85",
      "complete": true,
      "lowest_estimate": false
    },
    {
      "label": "Porto",
      "flight_estimate": null,
      "nightly_stay_estimate": "90.00",
      "stay_estimate": "270.00",
      "flight_and_stay_estimate": null,
      "complete": false,
      "lowest_estimate": false
    }
  ]
}
```

Errors are JSON objects with a `message`:

| Status | When |
| --- | --- |
| 400 | The body is not one JSON object of the expected shape: malformed JSON, unknown fields, wrong types (such as an amount sent as a number), or extra data after the object. |
| 405 | Any method other than POST. The response includes `Allow: POST`. |
| 413 | The body is larger than 16 KiB. |
| 422 | One or more fields are invalid. `field_errors` lists every problem as `{ "field", "message" }`, where `field` is a JSON path such as `check_out` or `destinations[1].flight_estimate`. When more than three destinations are sent, only the first three are checked. |

Every response from this path sets `Cache-Control: no-store`. Requests to unknown paths get the router's plain-text 404.

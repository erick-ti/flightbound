# Trip comparison

The `/compare` page compares two or three destinations for one trip. A traveler enters their own estimates, each in the currency they have it in, and the Go API at `POST /api/trip-comparisons` validates them, converts them to one comparison currency, and does all of the arithmetic. The page shows the results exactly as the API returns them.

Results are the traveler's own estimates. They are not live prices, supplier offers, or a complete trip budget: they cover flights and accommodation only.

## Inputs

Shared by every destination:

- Check-in and check-out dates, as calendar dates (`YYYY-MM-DD`).
- The number of travelers, from 1 to 20.
- The comparison currency. Every total uses it.

For each destination:

- A name, 1 to 80 characters after surrounding spaces are trimmed.
- A round-trip flight estimate for the whole party.
- An accommodation estimate per night for the whole party.

Either estimate can be left blank. Each estimate has its own currency, which defaults to the comparison currency.

On the page, an estimate's currency follows the comparison currency until the traveler types that estimate or chooses its currency. After that it stays put, so changing the comparison currency converts the estimate instead of relabeling it.

## Calculation

```text
nights                 = calendar days from check-in to check-out (1 to 365)
stay estimate          = nights × nightly estimate, in the nightly estimate's currency
converted flight       = flight estimate in the comparison currency
converted stay         = stay estimate in the comparison currency
flight + stay estimate = converted flight + converted stay
```

Both estimates already cover the whole party, so the traveler count never multiplies them. Nights come from the calendar dates alone, so time zones and daylight saving changes cannot shift them. Past dates are accepted, since the server cannot know the traveler's local date.

An estimate already in the comparison currency is used as entered. The others are converted as described under [Exchange rates](#exchange-rates).

## Unknown estimates and the lowest label

A blank estimate is unknown, which is different from zero; `0` is a valid known estimate. A destination with an unknown estimate still shows its known parts, but it has no flight + stay total and counts as incomplete. An estimate that cannot be converted counts the same way.

The lowest-estimate label appears only when every destination is complete. Until then no destination is labeled, because an incomplete one could turn out to be cheaper. When complete destinations tie for the lowest total, each one is labeled as a joint lowest estimate. Totals are compared in the comparison currency.

## Exchange rates

Conversions use the European Central Bank's [euro foreign exchange reference rates](https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/html/index.en.html), which the ECB usually updates at around 16:00 CET on every TARGET working day. Source: ECB statistics. The ECB publishes these rates for information only, so a converted amount is a reference figure, not the rate a bank or card will charge.

- **Arithmetic.** Each rate is the amount of a currency that one euro buys. A conversion goes through the euro with exact arithmetic, then rounds once to the comparison currency's minor unit, with halves rounded up. For example, EUR 330.00 at 1.1206 is USD 369.798, shown as USD 369.80.
- **What is converted.** The flight estimate and the stay estimate are each converted once, then added, so the converted parts always add up to the total. The nightly estimate is not converted on its own, since multiplying a rounded amount by the nights would multiply its rounding.
- **When rates are needed.** The API looks up rates only when a known estimate is in a currency other than the comparison currency.
- **Freshness.** The API downloads the daily rates file and keeps it for an hour. After a failed download it waits a minute before trying again, and meanwhile it keeps using the last rates it downloaded. Rates are used for at most 7 days after their reference date. In the ECB's TARGET holiday calendar for 2026 to 2028, the longest gap between publications is 5 days, over Easter and over Christmas 2028.
- **No rates.** When no usable rates can be loaded, an estimate that needs converting is unknown in the comparison currency, so its destination has no total and no destination gets the lowest label. When the rates have no entry for a currency, only conversions from or to it are affected: the estimates in that currency, or, when it is the comparison currency, every estimate in another currency.
- **A saved rates file.** Started with `-fx-rates-file <path>`, the API reads a saved copy of the daily rates file once at startup and never downloads rates; the saved copy has no age limit. `make smoke` uses the copy in `backend/internal/fxrates/testdata/`, so checks never contact the ECB. `-fx-rates-url` changes the download address; passing it together with `-fx-rates-file` stops the API at startup.

## Money

Amounts are exact. The API reads each amount as a decimal string and keeps it as an integer count of the currency's minor unit (cents, or whole yen). The only rounding is the conversion rule above.

- Accepted: digits with an optional decimal point followed by at least one digit, such as `1250`, `1250.5`, or `0.99`.
- Rejected rather than rounded: more decimal places than the estimate's currency has (USD `12.345`, JPY `100.5`), signs, exponents, spaces, and separators such as `1,250`.
- At most 12 digits before the decimal point.

Results use each currency's exact number of decimal places, such as `975.85` or `74000`.

## Currencies

The supported currencies are the euro plus the currencies in the European Central Bank's daily euro reference rates as of 2026-10-09: AUD, BRL, CAD, CHF, CNY, CZK, DKK, EUR, GBP, HKD, HUF, IDR, ILS, INR, ISK, JPY, KRW, MXN, MYR, NOK, NZD, PHP, PLN, RON, SEK, SGD, THB, TRY, USD, and ZAR. Decimal places come from ISO 4217 List One (published 2026-09-17): JPY, ISK, and KRW have none, and the others have two.

The table lives in `backend/internal/money/money.go`. The page's currency menus in `frontend/app/compare/TripComparison.tsx` list the same codes; the API rejects any code that is not in its table.

## API

`POST /api/trip-comparisons` takes a JSON body of at most 16 KiB. Amounts are strings, and `null` means unknown. `flight_estimate_currency` and `nightly_stay_estimate_currency` are optional; omitted or `null`, they mean the comparison currency:

```json
{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "travelers": 2,
  "currency": "USD",
  "destinations": [
    { "label": "Lisbon", "flight_estimate": "600.10", "nightly_stay_estimate": "110", "nightly_stay_estimate_currency": "EUR" },
    { "label": "Porto", "flight_estimate": null, "nightly_stay_estimate": "90" }
  ]
}
```

A `200` response repeats the inputs in canonical form, with every estimate's currency, and adds the results:

```json
{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "nights": 3,
  "travelers": 2,
  "currency": "USD",
  "all_complete": false,
  "exchange_rates": {
    "available": true,
    "date": "2026-10-09",
    "per_euro": { "USD": "1.1206" }
  },
  "destinations": [
    {
      "label": "Lisbon",
      "flight_estimate": "600.10",
      "flight_estimate_currency": "USD",
      "nightly_stay_estimate": "110.00",
      "nightly_stay_estimate_currency": "EUR",
      "stay_estimate": "330.00",
      "converted_flight_estimate": "600.10",
      "converted_stay_estimate": "369.80",
      "flight_and_stay_estimate": "969.90",
      "complete": true,
      "lowest_estimate": false
    },
    {
      "label": "Porto",
      "flight_estimate": null,
      "flight_estimate_currency": "USD",
      "nightly_stay_estimate": "90.00",
      "nightly_stay_estimate_currency": "USD",
      "stay_estimate": "270.00",
      "converted_flight_estimate": null,
      "converted_stay_estimate": "270.00",
      "flight_and_stay_estimate": null,
      "complete": false,
      "lowest_estimate": false
    }
  ]
}
```

- `stay_estimate` is in the nightly estimate's currency.
- `converted_flight_estimate`, `converted_stay_estimate`, and `flight_and_stay_estimate` are in the comparison currency. A converted value equals the estimate when no conversion was needed, and is `null` when the estimate is unknown or could not be converted.
- `exchange_rates` is `null` when nothing needed converting. Otherwise `available` says whether usable rates were loaded, `date` is their reference date (`null` when unavailable), and `per_euro` lists each currency other than the euro that a conversion used, with its rate exactly as the ECB published it.

Errors are JSON objects with a `message`:

| Status | When |
| --- | --- |
| 400 | The body is not one JSON object of the expected shape: malformed JSON, unknown fields, wrong types (such as an amount sent as a number), or extra data after the object. |
| 405 | Any method other than POST. The response includes `Allow: POST`. |
| 413 | The body is larger than 16 KiB. |
| 422 | One or more fields are invalid. `field_errors` lists every problem as `{ "field", "message" }`, where `field` is a JSON path such as `check_out`, `destinations[1].flight_estimate`, or `destinations[0].nightly_stay_estimate_currency`. An estimate is checked against its own currency, and only when that currency is valid. An estimate too large to convert is reported on its own field, and a flight + stay total too large to add up is reported on `flight_estimate`. When more than three destinations are sent, only the first three are checked. |

Every response from this path sets `Cache-Control: no-store`. Requests to unknown paths get the router's plain-text 404.

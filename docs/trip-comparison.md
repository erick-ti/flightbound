# Trip comparison

The `/compare` page compares two or three destinations for one trip. A traveler enters their own estimates, each in the currency they have it in, and can add a budget for the trip and a country for each destination. The Go API at `POST /api/trip-comparisons` validates the amounts, converts them to one comparison currency, does all of the arithmetic, checks each destination against the budget, and adds the UK government's travel advice for each country. The page shows the results exactly as the API returns them.

Results are the traveler's own estimates. They are not live prices, supplier offers, or a complete trip budget: they cover flights and accommodation only.

## Inputs

Shared by every destination:

- Check-in and check-out dates, as calendar dates (`YYYY-MM-DD`).
- The number of travelers, from 1 to 20.
- The comparison currency. Every total uses it.
- An optional trip budget: what the traveler has for the whole trip and the whole party. It has its own currency, which defaults to the comparison currency.

For each destination:

- A name, 1 to 80 characters after surrounding spaces are trimmed.
- An optional country, chosen from the countries and territories with travel advice, as described under [Travel advice](#travel-advice).
- A round-trip flight estimate for the whole party.
- An accommodation estimate per night for the whole party.

Either estimate can be left blank. Each estimate has its own currency, which defaults to the comparison currency.

On the page, an estimate's currency follows the comparison currency until the traveler types that estimate or chooses its currency. After that it stays put, so changing the comparison currency converts the estimate instead of relabeling it. The budget's currency works the same way.

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

## Budget

When the traveler sets a budget, each destination is checked against it in the comparison currency. A budget in another currency is converted once, like an estimate. The check never changes a total, completeness, or the lowest label.

| Result | When | Amount shown |
| --- | --- | --- |
| Within | The destination is complete and its flight + stay total is at most the budget. | What the total leaves of the budget for everything else, zero when they are equal. |
| Over | The destination is complete and its total is more than the budget. | How far the total goes over the budget. |
| Over by at least | The destination is incomplete, but the part it has (the flight or the stay) is already more than the budget. | How far that part alone goes over the budget: the least the destination can be over. |
| Unknown | The budget could not be converted, or the destination is incomplete and the part it has is not more than the budget. | None. |

An incomplete destination can still be over budget because amounts are never negative: whatever the missing estimate turns out to be, it can only add to the part that is known. The missing estimate is never treated as zero, and the destination still has no total.

For example, with a budget of USD 1000.00 for three nights:

- Lisbon (flights 600.10, nightly 125.25) totals 975.85, which leaves USD 24.15 of the budget for everything else.
- Porto (flights 800.00, nightly 90.00) totals 1070.00, which is USD 70.00 over.
- Faro (flights 1200.00, nightly unknown) is at least USD 200.00 over.

The flight + stay estimate is not a full trip budget, so the page never says that a destination fits the budget, only what flights and the stay leave for everything else.

## Exchange rates

Conversions use the European Central Bank's [euro foreign exchange reference rates](https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/html/index.en.html), which the ECB usually updates at around 16:00 CET on every TARGET working day. Source: ECB statistics. The ECB publishes these rates for information only, so a converted amount is a reference figure, not the rate a bank or card will charge.

- **Arithmetic.** Each rate is the amount of a currency that one euro buys. A conversion goes through the euro with exact arithmetic, then rounds once to the comparison currency's minor unit, with halves rounded up. For example, EUR 330.00 at 1.1206 is USD 369.798, shown as USD 369.80.
- **What is converted.** The flight estimate and the stay estimate are each converted once, then added, so the converted parts always add up to the total. The nightly estimate is not converted on its own, since multiplying a rounded amount by the nights would multiply its rounding. The budget is converted once, by the same rule.
- **When rates are needed.** The API looks up rates only when a known estimate or the budget is in a currency other than the comparison currency.
- **Freshness.** The API downloads the daily rates file and keeps it for an hour. After a failed download it waits a minute before trying again, and meanwhile it keeps using the last rates it downloaded. Rates are used for at most 7 days after their reference date. In the ECB's TARGET holiday calendar for 2026 to 2028, the longest gap between publications is 5 days, over Easter and over Christmas 2028.
- **No rates.** When no usable rates can be loaded, an estimate that needs converting is unknown in the comparison currency, so its destination has no total and no destination gets the lowest label. A budget that needs converting is unknown too, so no destination is checked against it. When the rates have no entry for a currency, only conversions from or to it are affected: the amounts in that currency, or, when it is the comparison currency, every amount in another currency.
- **A saved rates file.** Started with `-fx-rates-file <path>`, the API reads a saved copy of the daily rates file once at startup and never downloads rates; the saved copy has no age limit. `make smoke` uses the copy in `backend/internal/fxrates/testdata/`, so checks never contact the ECB. `-fx-rates-url` changes the download address; passing it together with `-fx-rates-file` stops the API at startup.

## Travel advice

A destination can name a country or territory from the list of the UK Foreign, Commonwealth & Development Office (FCDO), which publishes travel advice for British travellers on GOV.UK. The comparison then shows that country's warnings against travel in plain words, when FCDO last updated the advice, when the API checked it, and a link to the full advice. The advice never changes an amount, a budget check, completeness, or the lowest label.

FCDO's alert status lists a country's warnings. The API returns them most severe first, and the page words each one as follows, with the country's name from the list:

| Alert | Page wording |
| --- | --- |
| `avoid_all_travel_to_whole_country` | FCDO advises against all travel to {country}. |
| `avoid_all_travel_to_parts` | FCDO advises against all travel to parts of {country}. |
| `avoid_all_but_essential_travel_to_whole_country` | FCDO advises against all but essential travel to {country}. |
| `avoid_all_but_essential_travel_to_parts` | FCDO advises against all but essential travel to parts of {country}. |

A country can have more than one warning, such as warnings for different parts of it. A warning about parts of a country does not say which parts, so the page asks the traveler to check whether the places they plan to visit are affected. With no warning, the page says that FCDO does not advise against travel to any part of the country; the full advice still covers other risks, entry requirements, and health.

- **Source.** The API reads each country's content item from the GOV.UK Content API at `https://www.gov.uk/api/content/foreign-travel-advice/<slug>`, where the slug is the country's identifier in the list, such as `portugal`. GOV.UK content is published under the [Open Government Licence v3.0](https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/), and the page credits it with "Contains public sector information licensed under the Open Government Licence v3.0."
- **Freshness.** Each comparison fetches the advice for its countries again: one request per distinct country, all at the same time and alongside any exchange-rate download. Nothing is cached, since GOV.UK asks clients to cache this content for at most 10 seconds. The comparison waits at most 5 seconds for the advice. Each request uses a new connection, so a failed request is not retried.
- **Unavailable advice.** The destination shows "Travel advice unavailable" with a link to GOV.UK when the request fails, takes longer than 5 seconds, or gets any answer but a 200 (redirects are not followed), or when the content is not what the API expects: not a single JSON object, larger than 1 MiB, not travel advice, a page for another country, withdrawn advice, a missing or repeated field, an alert status that is not a list of the four warnings, or an update time that is not RFC 3339. Field names must match exactly. The rest of the comparison is unaffected, and the API never shows advice it did not just receive.
- **Dates.** `updated_at` is when FCDO last changed the advice (GOV.UK's `public_updated_at`, which GOV.UK gives in UK time); the page shows its date in UK time. `checked_at` is when the API received the advice; the page shows it in the traveler's time zone.
- **Country list.** `GET /api/travel-advice/countries` serves the countries and territories with travel advice from a copy kept in `backend/internal/traveladvice/countries.json`, in slug order, which is also alphabetical order of the names. When FCDO adds or renames a country, refresh it from `https://www.gov.uk/api/content/foreign-travel-advice`: keep `details.country.slug` and `details.country.name` of every entry in `links.children`, in slug order. Until then a renamed country shows as unavailable. The package tests check the list's shape.
- **Requests.** GOV.UK receives the API's request for each chosen country, never a request from the traveler's browser.
- **Saved copies.** Started with `-travel-advice-dir <dir>`, the API reads saved GOV.UK content items, one `<slug>.json` file per country, once at startup and never contacts GOV.UK. A file that is not named after a country in the list or does not parse as that country's advice, or a directory without any saved page, stops the API at startup. A country without a saved copy is unavailable, and `checked_at` is `null`. `make smoke` uses the copies in `backend/internal/traveladvice/testdata/` (Portugal, Thailand, Afghanistan, and Ukraine, as published in October 2026), so checks never contact GOV.UK. These copies contain public sector information licensed under the Open Government Licence v3.0.

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

`POST /api/trip-comparisons` takes a JSON body of at most 16 KiB. Amounts are strings, and `null` means unknown. `budget`, `budget_currency`, `country`, `flight_estimate_currency`, and `nightly_stay_estimate_currency` are optional. An omitted or `null` budget means none is set, an omitted or `null` currency means the comparison currency, and an omitted or `null` country means none. A country is a slug from [the country list](#country-list):

```json
{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "travelers": 2,
  "currency": "USD",
  "budget": "900",
  "budget_currency": "EUR",
  "destinations": [
    { "label": "Lisbon", "country": "portugal", "flight_estimate": "600.10", "nightly_stay_estimate": "110", "nightly_stay_estimate_currency": "EUR" },
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
  "budget": "900.00",
  "budget_currency": "EUR",
  "converted_budget": "1008.54",
  "all_complete": false,
  "exchange_rates": {
    "available": true,
    "date": "2026-10-09",
    "per_euro": { "USD": "1.1206" }
  },
  "destinations": [
    {
      "label": "Lisbon",
      "country": "portugal",
      "flight_estimate": "600.10",
      "flight_estimate_currency": "USD",
      "nightly_stay_estimate": "110.00",
      "nightly_stay_estimate_currency": "EUR",
      "stay_estimate": "330.00",
      "converted_flight_estimate": "600.10",
      "converted_stay_estimate": "369.80",
      "flight_and_stay_estimate": "969.90",
      "complete": true,
      "lowest_estimate": false,
      "budget_status": "within",
      "budget_difference": "38.64",
      "travel_advice": {
        "country_name": "Portugal",
        "url": "https://www.gov.uk/foreign-travel-advice/portugal",
        "available": true,
        "alerts": [],
        "updated_at": "2026-08-11T09:21:44Z",
        "checked_at": "2026-10-11T02:00:00Z"
      }
    },
    {
      "label": "Porto",
      "country": null,
      "flight_estimate": null,
      "flight_estimate_currency": "USD",
      "nightly_stay_estimate": "90.00",
      "nightly_stay_estimate_currency": "USD",
      "stay_estimate": "270.00",
      "converted_flight_estimate": null,
      "converted_stay_estimate": "270.00",
      "flight_and_stay_estimate": null,
      "complete": false,
      "lowest_estimate": false,
      "budget_status": "unknown",
      "budget_difference": null,
      "travel_advice": null
    }
  ]
}
```

- `stay_estimate` is in the nightly estimate's currency.
- `converted_flight_estimate`, `converted_stay_estimate`, and `flight_and_stay_estimate` are in the comparison currency. A converted value equals the estimate when no conversion was needed, and is `null` when the estimate is unknown or could not be converted.
- `budget` is in `budget_currency`, which is always given. `converted_budget` is the budget in the comparison currency: the same amount when no conversion was needed, and `null` when no budget is set or it could not be converted.
- `budget_status` is `not_set`, `unknown`, `within`, `over`, or `over_at_least`, as described under [Budget](#budget). `budget_difference` is in the comparison currency: the amount left for `within`, the amount over for `over`, the least amount over for `over_at_least`, and `null` otherwise.
- `exchange_rates` is `null` when nothing needed converting. Otherwise `available` says whether usable rates were loaded, `date` is their reference date (`null` when unavailable), and `per_euro` lists each currency other than the euro that a conversion used, with its rate exactly as the ECB published it.
- `travel_advice` is `null` when the destination has no country. Otherwise `country_name` and `url` name the country and its advice page on GOV.UK, and `available` says whether current advice was received. When it was, `alerts` lists the warnings most severe first (empty when there are none), and `updated_at` and `checked_at` are RFC 3339 times in UTC, with `checked_at` `null` for a saved copy. When it was not, `alerts`, `updated_at`, and `checked_at` are `null`. See [Travel advice](#travel-advice).

Errors are JSON objects with a `message`:

| Status | When |
| --- | --- |
| 400 | The body is not one JSON object of the expected shape: malformed JSON, unknown fields, wrong types (such as an amount sent as a number, or a country that is not a string), or extra data after the object. |
| 405 | Any method other than POST. The response includes `Allow: POST`. |
| 413 | The body is larger than 16 KiB. |
| 422 | One or more fields are invalid. `field_errors` lists every problem as `{ "field", "message" }`, where `field` is a JSON path such as `check_out`, `destinations[1].flight_estimate`, or `destinations[0].nightly_stay_estimate_currency`. A country that is not in the list is reported on `destinations[i].country`. An estimate or the budget is checked against its own currency, and only when that currency is valid. An estimate too large to convert is reported on its own field, a budget too large to convert on `budget`, and a flight + stay total too large to add up on `flight_estimate`. These are reported only once every field is otherwise valid. When more than three destinations are sent, only the first three are checked. |

A field error found before the calculation stops the request before any travel advice is fetched. The too-large errors are found during the calculation, after the advice requests have started, and those requests are then cancelled.

### Country list

`GET /api/travel-advice/countries` returns the countries and territories with travel advice, in slug order:

```json
{ "countries": [{ "slug": "afghanistan", "name": "Afghanistan" }, { "slug": "albania", "name": "Albania" }] }
```

Any method other than GET or HEAD gets a 405 with `Allow: GET, HEAD`.

Every response from these paths sets `Cache-Control: no-store`. Requests to unknown paths get the router's plain-text 404.

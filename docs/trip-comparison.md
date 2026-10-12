# Trip comparison

The `/compare` page compares two or three destinations for one trip. A traveler enters their own estimates, each in the currency they have it in, and can add a budget for the trip and, for each destination, a place from a short list or a country. The Go API at `POST /api/trip-comparisons` validates the amounts, converts them to one comparison currency, does all of the arithmetic, checks each destination against the budget, adds the UK government's travel advice for each country, and adds the typical weather of each place for the months of the stay. The page shows the results exactly as the API returns them, with the places on a map, and keeps the compared trip in its address so it can be reloaded or sent as a [trip link](#trip-links).

Results are the traveler's own estimates. They are not live prices, supplier offers, or a complete trip budget: they cover flights and accommodation only.

## Inputs

Shared by every destination:

- Check-in and check-out dates, as calendar dates (`YYYY-MM-DD`).
- The number of travelers, from 1 to 20.
- The comparison currency. Every total uses it.
- An optional trip budget: what the traveler has for the whole trip and the whole party. It has its own currency, which defaults to the comparison currency.

For each destination:

- A name, 1 to 80 characters after surrounding spaces are trimmed.
- An optional place, chosen from the supported places, as described under [Places and typical weather](#places-and-typical-weather). A place sets the destination's country and puts it on the [map](#map).
- An optional country, chosen from the countries and territories with travel advice, as described under [Travel advice](#travel-advice). With a place, the country is the place's.
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

## Places and typical weather

A destination can name one of the supported places, such as Lisbon or Bangkok. The place sets the destination's country for travel advice, and the comparison adds the place's typical weather for each calendar month that the stay's nights fall in (from the check-in night to the night before check-out), in the order the stay reaches them. Destinations that are not on the list keep a free-text name and an optional country. Typical weather never changes an amount, a budget check, completeness, the lowest label, or the travel advice.

For each month the page shows one sentence, such as "Typical weather in Lisbon in October: highs 21 °C (70 °F), lows 16 °C (60 °F), about 81 mm (3.2 in) of precipitation." When the precipitation rounds to 0 mm it says "less than 1 mm of precipitation", and when only the inches round to 0.0 it says "less than 0.1 in".

- **The places.** `backend/internal/places/places.json` lists each place's ID, display name, FCDO country slug, latitude and longitude (4 decimal places), its climate point (`climate_latitude` and `climate_longitude`, described below), and the Natural Earth feature it came from (`source_id`). The coordinates were copied by a script from Natural Earth's populated places (`ne_10m_populated_places_simple.geojson` in release v5.1.2 of [natural-earth-vector](https://github.com/nvkelso/natural-earth-vector), public domain), taken from each feature's point geometry. IDs are lowercase ASCII words joined by hyphens and never change once published. There are no UK places, because FCDO publishes no advice for the UK. The package tests check the file's shape, that IDs are unique, that the list is in name order, that every country is in the travel advice list, and that each climate point lies on the 0.1 degree grid within 0.3 degrees of its place.
- **What the figures are.** Normals for 1991 to 2020: the average daily high and the average daily low, and the average total precipitation for the month (rain and snow, with snow counted as melted water). They describe a land area about 9 km wide near the place, its climate point, so they can differ from a weather station in the city, most on coasts and in mountains; daytime highs in coastal cities can come out a few degrees low. They are not a forecast.
- **Source.** [ERA5 Land hourly time-series data from 1950 to present](https://doi.org/10.24381/ee82e357) from the Copernicus Climate Data Store: hourly 2 m air temperature and total precipitation on ERA5-Land's 0.1 degree land grid, read at each place's climate point. The dataset is licensed under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/), and the page credits it with "The results contain modified Copernicus Climate Change Service information 2026. Neither the European Commission nor ECMWF is responsible for any use that may be made of the Copernicus information or data it contains."
- **Climate points.** A place's climate point is the point of ERA5-Land's 0.1 degree grid nearest the place, by great-circle distance, that is at least half land in ECMWF's [ERA5-Land land-sea mask](https://confluence.ecmwf.int/display/CKB/ERA5-Land%3A+data+documentation) and that ERA5-Land covers. The nearest grid point itself can be mostly sea, which flattens daily highs and lows, and ERA5-Land leaves ocean points empty; one point that the mask counts as land, Barcelona's (41.4, 2.2), also came back empty, so Barcelona uses the next one. Ties go to the lower latitude, then the lower longitude. Before adding a place, check that its figures look like land: a day-night swing of several degrees, and highs within 3 °C of the national weather service's normals where they are published.
- **How they are computed.** Days are local solar days: UTC plus the place's longitude divided by 15, rounded to whole hours, because daily highs and lows follow the sun. A day's high and low are the highest and lowest of its 24 hourly temperatures. Each hourly precipitation value covers the hour ending at its time, and counts toward the day that hour falls in. ECMWF derives those hourly values by subtracting running totals, which leaves rounding residue just below zero, so values down to -0.001 mm count as none. A month's high and low are the means over every day of that month in the 30 years, and its precipitation is the mean of the 30 monthly totals. The results are stored in tenths of a degree Celsius and tenths of a millimetre, rounded with halves away from zero.
- **Rounding for display.** The API rounds once from the stored tenths, with integer arithmetic and halves away from zero: whole degrees Celsius, whole degrees Fahrenheit (C × 9/5 + 32), whole millimetres, and inches (25.4 mm) to one decimal place.
- **No figures.** A destination without a place has no typical weather (`climate` is `null`). The package tests require figures for every listed place.
- **Requests.** The figures are saved in `backend/internal/climate/normals.json`, so a comparison never contacts the Climate Data Store.
- **Regenerating the figures.** `scripts/climate/download_era5.py` downloads each place's hourly data once, for its climate point. It needs a Climate Data Store account that has accepted the dataset's licence, with the account's token in `~/.cdsapirc`; `--check-token` checks the token and licence without downloading. Each download is one request, logged in `requests.log` in the output directory before it is sent, and `--max-requests` caps the total in that log across runs. The client never retries, so the first failure stops the run, and a rerun skips places already downloaded. Each download is saved as the service sends it: `<place ID>.zip`, a zip archive holding a 2 m temperature file and a precipitation file. Then `cd backend && go run ./cmd/climatenormals -in <dir> -out <candidate>` checks each download (two CSV files of consecutive hours at the place's climate point, with plausible values and none missing), computes the normals, and writes them in one step, recording each download's SHA-256. Check the candidate before writing over `internal/climate/normals.json`, the default output. The same downloads always give the same file. The attribution's year, in `cmd/climatenormals` and in the page footer, is the year of the downloads, so update both when downloading again, together with the Lisbon figures in this document's examples. Test the script with `uv run --locked scripts/climate/test_download_era5.py`; its tests use a local server, never the Climate Data Store.

## Map

When at least one destination has a place, the results start with a world map above the cards. Each place gets one pin: a button holding the numbers of the destinations at that place (two destinations at the same place share a pin, such as "1,2"), drawn at the `location` the API returns. While the map is shown, each card's heading starts with its number, a card with a place has a "Show on map" button, and a card without one says "Not on the map: no place chosen."

- **Selecting.** The pins and the cards' buttons share one selection. Selecting a pin highlights its cards and brings the first of them into view. Selecting a card's button highlights its pin and moves the map to the pin if it is out of view. Selecting the same place again clears the selection, and each new comparison starts with nothing selected. A selected pin grows and gains a dark ring, and a selected card gains a dark ring, so the change does not rely on color alone.
- **What it shows.** The map draws the comparison as submitted: editing the form moves no pin until the next comparison, and an out-of-date result dims the map with the cards. The first view fits every pin, closer in for a single place, and the pins are fitted again whenever the map changes size. The map does not rotate or tilt. Zooming with a mouse wheel needs Ctrl (⌘ on a Mac) and panning on a touch screen needs two fingers, so the page itself still scrolls.
- **Map data.** Natural Earth's 1:110m land, lakes, and land borders, from release v5.1.2 of [natural-earth-vector](https://github.com/nvkelso/natural-earth-vector) (public domain), served by the web app from `frontend/public/map/` exactly as published:

  | File | Bytes | SHA-256 |
  | --- | --- | --- |
  | `ne_110m_land.geojson` | 138,160 | `9e0729ee253ca7d7a5c4ae9395fb1902264c5377c52e224d13dd85010e2835d9` |
  | `ne_110m_lakes.geojson` | 36,648 | `eb02ecc86c82004fccbf979058bfabbbd6c2d07968c7844d38eb1c9152d2ffc9` |
  | `ne_110m_admin_0_boundary_lines_land.geojson` | 340,010 | `d42479fd79552cca4eec7f85fcdca717a790d29ff06be7676f1af0568c6d3f7c` |

  Lakes are drawn as water over the land. Borders follow Natural Earth's default, which draws a border where control is on the ground ([disputed boundaries policy](https://www.naturalearthdata.com/about/disputed-boundaries-policy/)): a border whose `FEATURECLA` is "International boundary (verify)" is solid, and every other kind (disputed, line of control, indefinite, claim, indeterminate frontier) is dashed. The map has no place names, so it needs no fonts or icons. Its credit reads "MapLibre | Natural Earth".
- **Drawing.** [MapLibre GL JS](https://maplibre.org/) 6.13.0 draws the map with WebGL2. The page loads it only when a result has a place, and its worker file comes from the web app's own build, so the map makes no request outside the web app.
- **When the map cannot be shown.** If MapLibre fails to load, the browser has no WebGL2, a map file fails to load, MapLibre has not arrived within 60 seconds, or the map has not loaded within 10 seconds of MapLibre arriving, the map gives way to "The map could not be shown in this browser. Every destination is listed below." The cards never depend on the map: they keep every estimate and drop their numbers and map buttons.
- **Checks.** `make smoke` checks that each map file is served with its SHA-256, then runs the browser tests in `frontend/tests/` (Playwright with headless Chromium) against the running apps: selecting from a pin and from a card, a shared pin, the reset on a new comparison, the refit after a resize, each failure above (the 60-second wait for MapLibre on a fake clock), a slow MapLibre download that still gets its full 10 seconds, and that no request leaves the web origin. The tests block such requests rather than only record them, refuse every redirect so none can be followed to another origin, and prove both with requests that would otherwise reach a second local origin.

## Trip links

After each comparison, the page's address holds the trip it compared, so the traveler can reload the page, bookmark it, or send the link to someone. Opening the link fills the form with the same trip and compares it again.

- **What a link holds.** The comparison request the results came from, exactly as the page sent it (see [API](#api)): every input, with `null` for a blank amount and each amount's currency. It follows the `#` of the address: `/compare#trip=1.` and then the request's UTF-8 JSON in base64url without padding. Browsers never send this part of an address to a server, so the trip reaches the API only in the comparison request the page makes.
- **When it changes.** Only when a comparison succeeds on this page. Editing the form, or a comparison the API rejects, leaves the address alone, so a reload brings back the last compared trip rather than later edits. The page never adds a history entry of its own: it replaces the address, so Back does not step through its comparisons. A link typed or pasted into the address bar is an entry the browser adds, and Back and Forward open such links again. A comparison still running when the traveler leaves the page is dropped, so it cannot change another page's address.
- **Opening a link.** A trip link in the address, when the page loads or when the address changes to another one, replaces whatever the page was doing: results and any comparison still running are dropped, and the form is busy. Once the place list has loaded or failed, the page fills the form from the link and compares the trip at once. Exchange rates and travel advice are fetched as for any comparison, so the results can differ from what the sender saw.
- **Currencies after opening.** An amount's currency stays put when the amount is filled in or its currency differs from the comparison currency. A blank amount in the comparison currency follows it again, as in a new form, so a blank estimate whose currency was chosen to match the comparison currency follows it after a reload.
- **Links that do not open.** A link opens only when every value fits the form exactly, so that the form shows what the next comparison sends: real dates as `YYYY-MM-DD`, 1 to 20 travelers, 2 or 3 destinations, supported currencies, places from the place list, amounts without surrounding spaces, and no line breaks in names or amounts (a text field drops them). Anything else, including an unknown format version or more than 16 KiB of JSON (the most the API accepts), shows "This trip link could not be opened. It may be incomplete or damaged." at once and changes nothing in the form. A link with places cannot open while the place list is unavailable. The API still validates every value when the trip is compared.
- **Versions.** `1` is the only format so far. A change to what a link holds gets a new number, and the page keeps opening every earlier version as before; `frontend/tests/trip.spec.ts` keeps a version 1 link that must keep opening.
- **Privacy.** Anyone with a link can see the trip's estimates and budget, and the link stays in the browser's history. The page says so under the results.
- **Checks.** `make smoke` runs the link tests in `frontend/tests/`: the encoding without a browser (the saved version 1 link, each refusal, seeded round trips through the link and the form, and damaged links), and the page in a browser (reopening a compared trip in a new page and on reload, currencies after opening, a second link in the same page, late answers in both orders and while the place list loads, links that cannot open, the address after a rejected comparison, and leaving the page while a comparison is running).

## Results checks

`make smoke` also checks the results' wording in a browser against the API's answers (`frontend/tests/compare-results.spec.ts`): each budget line, a budget in another currency, each kind of travel advice the saved copies hold (no warnings, parts of a country, the whole country, two warnings, and unavailable), typical weather across two months and in months with almost no precipitation, a trip with none of these, and field errors next to their fields until the trip is fixed. Wording the saved data cannot produce is not checked in a browser: unavailable exchange rates, a currency without an ECB rate, an advice check time, and advice against all but essential travel to a whole country.

## Money

Amounts are exact. The API reads each amount as a decimal string and keeps it as an integer count of the currency's minor unit (cents, or whole yen). The only rounding is the conversion rule above.

- Accepted: digits with an optional decimal point followed by at least one digit, such as `1250`, `1250.5`, or `0.99`.
- Rejected rather than rounded: more decimal places than the estimate's currency has (USD `12.345`, JPY `100.5`), signs, exponents, spaces, and separators such as `1,250`.
- At most 12 digits before the decimal point.

Results use each currency's exact number of decimal places, such as `975.85` or `74000`.

## Currencies

The supported currencies are the euro plus the currencies in the European Central Bank's daily euro reference rates as of 2026-10-09: AUD, BRL, CAD, CHF, CNY, CZK, DKK, EUR, GBP, HKD, HUF, IDR, ILS, INR, ISK, JPY, KRW, MXN, MYR, NOK, NZD, PHP, PLN, RON, SEK, SGD, THB, TRY, USD, and ZAR. Decimal places come from ISO 4217 List One (published 2026-09-17): JPY, ISK, and KRW have none, and the others have two.

The table lives in `backend/internal/money/money.go`. The page's currency menus and trip links use the same codes, listed in `frontend/app/compare/trip.ts`; the API rejects any code that is not in its table.

## API

`POST /api/trip-comparisons` takes a JSON body of at most 16 KiB. Amounts are strings, and `null` means unknown. `budget`, `budget_currency`, `place`, `country`, `flight_estimate_currency`, and `nightly_stay_estimate_currency` are optional. An omitted or `null` budget means none is set, an omitted or `null` currency means the comparison currency, and an omitted or `null` place or country means none. A place is an ID from [the place list](#place-list), and a country is a slug from [the country list](#country-list). With a place, the country may be omitted, `null`, or the place's own country:

```json
{
  "check_in": "2027-03-10",
  "check_out": "2027-03-13",
  "travelers": 2,
  "currency": "USD",
  "budget": "900",
  "budget_currency": "EUR",
  "destinations": [
    { "label": "Lisbon", "place": "lisbon", "flight_estimate": "600.10", "nightly_stay_estimate": "110", "nightly_stay_estimate_currency": "EUR" },
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
      "place": "lisbon",
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
      },
      "climate": {
        "place_name": "Lisbon",
        "period_start": 1991,
        "period_end": 2020,
        "months": [
          { "month": 3, "high_c": 17, "high_f": 63, "low_c": 11, "low_f": 51, "precipitation_mm": 55, "precipitation_in": "2.2" }
        ]
      },
      "location": { "latitude": 38.7247, "longitude": -9.1468 }
    },
    {
      "label": "Porto",
      "place": null,
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
      "travel_advice": null,
      "climate": null,
      "location": null
    }
  ]
}
```

- `stay_estimate` is in the nightly estimate's currency.
- `converted_flight_estimate`, `converted_stay_estimate`, and `flight_and_stay_estimate` are in the comparison currency. A converted value equals the estimate when no conversion was needed, and is `null` when the estimate is unknown or could not be converted.
- `budget` is in `budget_currency`, which is always given. `converted_budget` is the budget in the comparison currency: the same amount when no conversion was needed, and `null` when no budget is set or it could not be converted.
- `budget_status` is `not_set`, `unknown`, `within`, `over`, or `over_at_least`, as described under [Budget](#budget). `budget_difference` is in the comparison currency: the amount left for `within`, the amount over for `over`, the least amount over for `over_at_least`, and `null` otherwise.
- `exchange_rates` is `null` when nothing needed converting. Otherwise `available` says whether usable rates were loaded, `date` is their reference date (`null` when unavailable), and `per_euro` lists each currency other than the euro that a conversion used, with its rate exactly as the ECB published it.
- `place` is the place's ID, or `null`. With a place, `country` is the place's country.
- `climate` is `null` when the destination has no place. Otherwise it has `place_name`, the years `period_start` and `period_end` that the figures average, and `months`: one entry for each calendar month that the stay's nights fall in, in the order the stay reaches them, with `month` (1 for January to 12 for December), the integers `high_c`, `high_f`, `low_c`, `low_f`, and `precipitation_mm`, and `precipitation_in` as a decimal string with one decimal place. See [Places and typical weather](#places-and-typical-weather).
- `location` is `null` when the destination has no place. Otherwise it gives the place's `latitude` and `longitude` in degrees, as listed in `places.json`: the city's point from Natural Earth, not its climate point. The page uses it to put the destination on the [map](#map). It never changes an amount, a budget check, completeness, or the lowest label.
- `travel_advice` is `null` when the destination has no country. Otherwise `country_name` and `url` name the country and its advice page on GOV.UK, and `available` says whether current advice was received. When it was, `alerts` lists the warnings most severe first (empty when there are none), and `updated_at` and `checked_at` are RFC 3339 times in UTC, with `checked_at` `null` for a saved copy. When it was not, `alerts`, `updated_at`, and `checked_at` are `null`. See [Travel advice](#travel-advice).

Errors are JSON objects with a `message`:

| Status | When |
| --- | --- |
| 400 | The body is not one JSON object of the expected shape: malformed JSON, unknown fields, wrong types (such as an amount sent as a number, or a place or country that is not a string), or extra data after the object. |
| 405 | Any method other than POST. The response includes `Allow: POST`. |
| 413 | The body is larger than 16 KiB. |
| 422 | One or more fields are invalid. `field_errors` lists every problem as `{ "field", "message" }`, where `field` is a JSON path such as `check_out`, `destinations[1].flight_estimate`, or `destinations[0].nightly_stay_estimate_currency`. A place that is not in the list is reported on `destinations[i].place`, and a country that is not in the list, or that differs from the place's country, on `destinations[i].country`. An estimate or the budget is checked against its own currency, and only when that currency is valid. An estimate too large to convert is reported on its own field, a budget too large to convert on `budget`, and a flight + stay total too large to add up on `flight_estimate`. These are reported only once every field is otherwise valid. When more than three destinations are sent, only the first three are checked. |

A field error found before the calculation stops the request before any travel advice is fetched. The too-large errors are found during the calculation, after the advice requests have started, and those requests are then cancelled.

### Country list

`GET /api/travel-advice/countries` returns the countries and territories with travel advice, in slug order:

```json
{ "countries": [{ "slug": "afghanistan", "name": "Afghanistan" }, { "slug": "albania", "name": "Albania" }] }
```

Any method other than GET or HEAD gets a 405 with `Allow: GET, HEAD`.

### Place list

`GET /api/places` returns the supported places in name order, each with its country's slug and name:

```json
{ "places": [{ "id": "amsterdam", "name": "Amsterdam", "country": "netherlands", "country_name": "Netherlands" }, { "id": "athens", "name": "Athens", "country": "greece", "country_name": "Greece" }] }
```

Any method other than GET or HEAD gets a 405 with `Allow: GET, HEAD`.

Every response from these paths sets `Cache-Control: no-store`. Requests to unknown paths get the router's plain-text 404.

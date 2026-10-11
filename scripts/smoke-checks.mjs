// Checks the comparison page and API through a running Next.js server. The
// API serves saved ECB rates, saved GOV.UK travel advice and climate normals
// saved in the repository, so nothing here reaches an outside service.
// Usage: node scripts/smoke-checks.mjs http://127.0.0.1:13000
import assert from "node:assert/strict";

const origin = process.argv[2];
if (!origin) {
  console.error("usage: node scripts/smoke-checks.mjs <web origin>");
  process.exit(2);
}

async function compare(body) {
  const response = await fetch(`${origin}/api/trip-comparisons`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return { status: response.status, body: await response.json() };
}

const trip = {
  check_in: "2027-03-10",
  check_out: "2027-03-13",
  travelers: 2,
  currency: "USD",
  destinations: [
    { label: "Lisbon", flight_estimate: "600.10", nightly_stay_estimate: "125.25" },
    { label: "Porto", flight_estimate: null, nightly_stay_estimate: "90" },
  ],
};

const checks = [
  [
    "the compare page renders",
    async () => {
      const response = await fetch(`${origin}/compare`);
      assert.equal(response.status, 200);
      const html = await response.text();
      assert.match(html, /Compare trips/);
      assert.match(html, /Country for travel advice/);
    },
  ],
  [
    "the travel advice country list comes through the web server",
    async () => {
      const response = await fetch(`${origin}/api/travel-advice/countries`);
      assert.equal(response.status, 200);
      assert.equal(response.headers.get("cache-control"), "no-store");
      assert.match(response.headers.get("content-type"), /^application\/json/);
      const { countries } = await response.json();
      assert.ok(countries.length > 200, `${countries.length} countries`);
      assert.deepEqual(
        countries.find((c) => c.slug === "portugal"),
        { slug: "portugal", name: "Portugal" },
      );
      const slugs = countries.map((c) => c.slug);
      assert.deepEqual(slugs, [...new Set(slugs)].sort());
    },
  ],
  [
    "each destination's country gets the saved FCDO advice without changing totals",
    async () => {
      const destinations = [
        { label: "Lisbon", flight_estimate: "600.10", nightly_stay_estimate: "125.25" },
        { label: "Bangkok", flight_estimate: "1400", nightly_stay_estimate: "60" },
        { label: "Kyiv", flight_estimate: "450", nightly_stay_estimate: null },
      ];
      const countries = ["portugal", "thailand", "ukraine"];
      const plain = await compare({ ...trip, destinations });
      const advised = await compare({
        ...trip,
        destinations: destinations.map((d, i) => ({ ...d, country: countries[i] })),
      });
      assert.equal(advised.status, 200);
      const page = (slug) => `https://www.gov.uk/foreign-travel-advice/${slug}`;
      assert.deepEqual(
        advised.body.destinations.map((d) => d.travel_advice),
        [
          { country_name: "Portugal", url: page("portugal"), available: true, alerts: [], updated_at: "2026-08-11T09:21:44Z", checked_at: null },
          {
            country_name: "Thailand",
            url: page("thailand"),
            available: true,
            alerts: ["avoid_all_but_essential_travel_to_parts"],
            updated_at: "2026-09-28T05:10:02Z",
            checked_at: null,
          },
          {
            country_name: "Ukraine",
            url: page("ukraine"),
            available: true,
            alerts: ["avoid_all_travel_to_parts", "avoid_all_but_essential_travel_to_parts"],
            updated_at: "2026-10-02T08:30:25Z",
            checked_at: null,
          },
        ],
      );
      assert.deepEqual(
        advised.body.destinations.map((d) => d.country),
        countries,
      );
      const withoutAdvice = advised.body.destinations.map(({ country, travel_advice, ...rest }) => rest);
      const withoutCountry = plain.body.destinations.map(({ country, travel_advice, ...rest }) => rest);
      assert.deepEqual(withoutAdvice, withoutCountry);
    },
  ],
  [
    "advice without a saved copy is unavailable and the rest still shows",
    async () => {
      const { status, body } = await compare({
        ...trip,
        destinations: [
          { ...trip.destinations[0], label: "Kabul", country: "afghanistan" },
          { ...trip.destinations[1], label: "Madrid", country: "spain" },
        ],
      });
      assert.equal(status, 200);
      const [kabul, madrid] = body.destinations;
      assert.deepEqual(kabul.travel_advice.alerts, ["avoid_all_travel_to_whole_country"]);
      assert.equal(kabul.travel_advice.updated_at, "2026-07-30T10:36:13Z");
      assert.deepEqual(madrid.travel_advice, {
        country_name: "Spain",
        url: "https://www.gov.uk/foreign-travel-advice/spain",
        available: false,
        alerts: null,
        updated_at: null,
        checked_at: null,
      });
      assert.equal(madrid.stay_estimate, "270.00");
    },
  ],
  [
    "destinations without a country get no advice",
    async () => {
      const { body } = await compare(trip);
      assert.ok(body.destinations.every((d) => d.country === null && d.travel_advice === null));
    },
  ],
  [
    "an unknown country is rejected with a field error",
    async () => {
      const { status, body } = await compare({
        ...trip,
        destinations: [{ ...trip.destinations[0], country: "atlantis" }, trip.destinations[1]],
      });
      assert.equal(status, 422);
      assert.deepEqual(body.field_errors, [{ field: "destinations[0].country", message: "Choose a country from the list." }]);
    },
  ],
  [
    "the compare page offers a place for typical weather",
    async () => {
      const html = await (await fetch(`${origin}/compare`)).text();
      assert.match(html, /Place for typical weather/);
      assert.match(html, /Not listed/);
    },
  ],
  [
    "the place list comes through the web server",
    async () => {
      const response = await fetch(`${origin}/api/places`);
      assert.equal(response.status, 200);
      assert.equal(response.headers.get("cache-control"), "no-store");
      assert.match(response.headers.get("content-type"), /^application\/json/);
      const { places } = await response.json();
      assert.ok(places.length >= 25, `${places.length} places`);
      assert.deepEqual(
        places.find((p) => p.id === "lisbon"),
        { id: "lisbon", name: "Lisbon", country: "portugal", country_name: "Portugal" },
      );
      const names = places.map((p) => p.name);
      assert.deepEqual(names, [...names].sort());
    },
  ],
  [
    "a place sets the country and adds typical weather without changing totals",
    async () => {
      const plain = await compare(trip);
      const placed = await compare({
        ...trip,
        destinations: [{ ...trip.destinations[0], place: "lisbon" }, trip.destinations[1]],
      });
      assert.equal(placed.status, 200);
      const [lisbon, porto] = placed.body.destinations;
      assert.equal(lisbon.place, "lisbon");
      assert.equal(lisbon.country, "portugal");
      assert.equal(lisbon.travel_advice.updated_at, "2026-08-11T09:21:44Z");
      const { place_name, period_start, period_end, months } = lisbon.climate;
      assert.deepEqual([place_name, period_start, period_end], ["Lisbon", 1991, 2020]);
      // The trip is 10 to 13 March.
      assert.deepEqual(months.map((m) => m.month), [3]);
      const [march] = months;
      for (const key of ["high_c", "high_f", "low_c", "low_f", "precipitation_mm"]) {
        assert.ok(Number.isInteger(march[key]), `${key} ${march[key]}`);
      }
      assert.ok(march.high_c >= march.low_c && march.high_f >= march.low_f, JSON.stringify(march));
      assert.match(march.precipitation_in, /^\d+\.\d$/);
      assert.equal(porto.climate, null);
      const strip = ({ place, country, travel_advice, climate, ...rest }) => rest;
      assert.deepEqual(placed.body.destinations.map(strip), plain.body.destinations.map(strip));
    },
  ],
  [
    "a country that does not match the place is rejected with a field error",
    async () => {
      const { status, body } = await compare({
        ...trip,
        destinations: [{ ...trip.destinations[0], place: "lisbon", country: "spain" }, trip.destinations[1]],
      });
      assert.equal(status, 422);
      assert.deepEqual(body.field_errors, [
        { field: "destinations[0].country", message: "This place is in Portugal. Choose Portugal or leave the country empty." },
      ]);
    },
  ],
  [
    "an unknown place is rejected with a field error",
    async () => {
      const { status, body } = await compare({
        ...trip,
        destinations: [{ ...trip.destinations[0], place: "atlantis" }, trip.destinations[1]],
      });
      assert.equal(status, 422);
      assert.deepEqual(body.field_errors, [{ field: "destinations[0].place", message: "Choose a place from the list." }]);
    },
  ],
  [
    "destinations without a place get no typical weather",
    async () => {
      const { body } = await compare(trip);
      assert.ok(body.destinations.every((d) => d.place === null && d.climate === null));
    },
  ],
  [
    "totals are calculated and missing estimates stay unknown",
    async () => {
      const { status, body } = await compare(trip);
      assert.equal(status, 200);
      assert.equal(body.nights, 3);
      const [lisbon, porto] = body.destinations;
      assert.equal(lisbon.stay_estimate, "375.75");
      assert.equal(lisbon.flight_and_stay_estimate, "975.85");
      assert.equal(porto.stay_estimate, "270.00");
      assert.equal(porto.flight_and_stay_estimate, null);
      assert.equal(porto.complete, false);
      assert.equal(body.all_complete, false);
      assert.ok(body.destinations.every((d) => d.lowest_estimate === false));
    },
  ],
  [
    "the traveler count does not change whole-party totals",
    async () => {
      const { body } = await compare({ ...trip, travelers: 4 });
      assert.equal(body.destinations[0].flight_and_stay_estimate, "975.85");
    },
  ],
  [
    "amounts in other currencies are converted at the saved ECB rates",
    async () => {
      const { status, body } = await compare({
        ...trip,
        destinations: [
          { ...trip.destinations[0], nightly_stay_estimate: "110", nightly_stay_estimate_currency: "EUR" },
          trip.destinations[1],
        ],
      });
      assert.equal(status, 200);
      const [lisbon] = body.destinations;
      assert.equal(lisbon.stay_estimate, "330.00");
      assert.equal(lisbon.converted_stay_estimate, "369.80");
      assert.equal(lisbon.flight_and_stay_estimate, "969.90");
      assert.deepEqual(body.exchange_rates, { available: true, date: "2026-10-09", per_euro: { USD: "1.1206" } });
    },
  ],
  [
    "tied complete estimates are all marked lowest",
    async () => {
      const { status, body } = await compare({
        ...trip,
        currency: "JPY",
        destinations: [
          { label: "Osaka", flight_estimate: "50000", nightly_stay_estimate: "8000" },
          { label: "Sapporo", flight_estimate: "62000", nightly_stay_estimate: "4000" },
          { label: "Naha", flight_estimate: "40000", nightly_stay_estimate: "12000" },
        ],
      });
      assert.equal(status, 200);
      assert.deepEqual(
        body.destinations.map((d) => d.flight_and_stay_estimate),
        ["74000", "74000", "76000"],
      );
      assert.deepEqual(
        body.destinations.map((d) => d.lowest_estimate),
        [true, true, false],
      );
    },
  ],
  [
    "destinations are checked against a budget, including one in another currency",
    async () => {
      const usd = await compare({
        ...trip,
        budget: "1000",
        destinations: [
          trip.destinations[0],
          { label: "Porto", flight_estimate: "800", nightly_stay_estimate: "90" },
          { label: "Faro", flight_estimate: "1200", nightly_stay_estimate: null },
        ],
      });
      assert.equal(usd.status, 200);
      assert.equal(usd.body.converted_budget, "1000.00");
      assert.deepEqual(
        usd.body.destinations.map((d) => [d.budget_status, d.budget_difference]),
        [
          ["within", "24.15"],
          ["over", "70.00"],
          ["over_at_least", "200.00"],
        ],
      );

      const eur = await compare({ ...trip, budget: "900", budget_currency: "EUR" });
      assert.equal(eur.status, 200);
      assert.equal(eur.body.budget, "900.00");
      assert.equal(eur.body.converted_budget, "1008.54");
      assert.deepEqual(
        eur.body.destinations.map((d) => [d.budget_status, d.budget_difference]),
        [
          ["within", "32.69"],
          ["unknown", null],
        ],
      );
      assert.deepEqual(eur.body.exchange_rates, { available: true, date: "2026-10-09", per_euro: { USD: "1.1206" } });
    },
  ],
  [
    "invalid dates and amounts are rejected with field errors",
    async () => {
      const { status, body } = await compare({
        ...trip,
        check_out: "2027-03-09",
        destinations: [{ ...trip.destinations[0], flight_estimate: "12.345" }, trip.destinations[1]],
      });
      assert.equal(status, 422);
      assert.deepEqual(
        body.field_errors.map((e) => e.field),
        ["check_out", "destinations[0].flight_estimate"],
      );
    },
  ],
];

let failures = 0;
for (const [name, check] of checks) {
  try {
    await check();
    console.log(`ok   ${name}`);
  } catch (error) {
    failures += 1;
    console.error(`FAIL ${name}\n${error.message}`);
  }
}
process.exit(failures === 0 ? 0 : 1);

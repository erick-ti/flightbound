// Checks the comparison page and API through a running Next.js server.
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
      assert.match(await response.text(), /Compare trips/);
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

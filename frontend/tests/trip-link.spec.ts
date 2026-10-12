import type { Page, Request } from "@playwright/test";
import type { ComparisonRequest } from "../app/compare/api";
import { readTripLink, tripLink } from "../app/compare/trip";
import { card, compare, destination, expect, fillTrip, openCompare, pins, test, twoPlaces } from "./support";

const UNREADABLE = "This trip link could not be opened. It may be incomplete or damaged.";
const NO_PLACE_LIST =
  "The place list could not be loaded, so this trip link cannot be opened. Reload the page to try again.";

// Two places, a budget in its own currency, and each way an amount's
// currency can be set: typed amounts, a blank amount in the comparison
// currency (which follows it), and a blank amount in another currency.
const TRIP_A: ComparisonRequest = {
  check_in: "2027-03-10",
  check_out: "2027-03-13",
  travelers: 2,
  currency: "USD",
  budget: "1000",
  budget_currency: "EUR",
  destinations: [
    {
      label: "Lisbon",
      place: "lisbon",
      country: null,
      flight_estimate: "600.10",
      flight_estimate_currency: "USD",
      nightly_stay_estimate: null,
      nightly_stay_estimate_currency: "USD",
    },
    {
      label: "Bangkok",
      place: "bangkok",
      country: null,
      flight_estimate: null,
      flight_estimate_currency: "GBP",
      nightly_stay_estimate: "1500",
      nightly_stay_estimate_currency: "THB",
    },
  ],
};

const TRIP_B: ComparisonRequest = {
  check_in: "2027-05-01",
  check_out: "2027-05-08",
  travelers: 4,
  currency: "EUR",
  budget: null,
  budget_currency: "EUR",
  destinations: [
    {
      label: "Porto",
      place: null,
      country: "portugal",
      flight_estimate: "450.00",
      flight_estimate_currency: "EUR",
      nightly_stay_estimate: "90.00",
      nightly_stay_estimate_currency: "EUR",
    },
    {
      label: "Tokyo",
      place: "tokyo",
      country: null,
      flight_estimate: "1200.00",
      flight_estimate_currency: "EUR",
      nightly_stay_estimate: null,
      nightly_stay_estimate_currency: "EUR",
    },
  ],
};

test("a trip link brings back the same trip and results", async ({ page, context }) => {
  await openCompare(page);
  await fillTrip(page, [{ name: "Lisbon", place: "Lisbon" }, { name: "Bangkok", place: "Bangkok" }, { name: "Porto" }]);
  await page.getByLabel("Travelers").selectOption("3");
  await page.getByLabel("Trip budget for the whole party").fill("2500");
  await page.getByLabel("Currency of the budget").selectOption("EUR");
  const [lisbon, bangkok, porto] = [1, 2, 3].map((n) => destination(page, n));
  await lisbon.getByLabel("Round-trip flights for the whole party").fill("600.10");
  await lisbon.getByLabel("Accommodation per night for the whole party").fill("125.25");
  await lisbon.getByLabel("Currency of the accommodation estimate").selectOption("EUR");
  await bangkok.getByLabel("Round-trip flights for the whole party").fill("25000");
  await bangkok.getByLabel("Currency of the flight estimate").selectOption("THB");
  // Left blank, in a currency of its own.
  await bangkok.getByLabel("Currency of the accommodation estimate").selectOption("GBP");
  await porto.getByLabel("Country for travel advice").selectOption({ label: "Portugal" });
  await porto.getByLabel("Round-trip flights for the whole party").fill("0");

  const sent = page.waitForRequest(isComparison);
  await compare(page);
  const original = (await sent).postDataJSON();
  await expect(page).toHaveURL(/#trip=1\./);
  expect(tripIn(page)).toEqual(original);
  await expect(page.getByText("This page's address now holds this trip")).toBeVisible();
  await expect(page.getByRole("article")).toHaveCount(3);
  await expect(pins(page)).toHaveCount(2);
  const fields = await formValues(page);
  const cards = await page.getByRole("article").allInnerTexts();

  // In a new page, the link fills the same form and sends the same request.
  const other = await context.newPage();
  const resent = other.waitForRequest(isComparison);
  await other.goto(page.url());
  expect((await resent).postDataJSON()).toEqual(original);
  await expect(other.getByRole("article")).toHaveCount(3);
  expect(await other.getByRole("article").allInnerTexts()).toEqual(cards);
  expect(await formValues(other)).toEqual(fields);
  await expect(pins(other)).toHaveCount(2);

  // So does a reload.
  const reloaded = page.waitForRequest(isComparison);
  await page.reload();
  expect((await reloaded).postDataJSON()).toEqual(original);
  await expect(page.getByRole("article")).toHaveCount(3);
  expect(await page.getByRole("article").allInnerTexts()).toEqual(cards);
  expect(await formValues(page)).toEqual(fields);
});

test("after a link opens, only blank amounts in the comparison currency follow it", async ({ page }) => {
  await openLink(page, TRIP_A);
  await page.getByLabel("Currency for totals").selectOption("CHF");
  const [lisbon, bangkok] = [destination(page, 1), destination(page, 2)];
  await expect(lisbon.getByLabel("Currency of the flight estimate")).toHaveValue("USD");
  await expect(lisbon.getByLabel("Currency of the accommodation estimate")).toHaveValue("CHF");
  await expect(bangkok.getByLabel("Currency of the flight estimate")).toHaveValue("GBP");
  await expect(bangkok.getByLabel("Currency of the accommodation estimate")).toHaveValue("THB");
  await expect(page.getByLabel("Currency of the budget")).toHaveValue("EUR");
});

test("a second link opened in the same page replaces the trip", async ({ page }) => {
  await openLink(page, TRIP_A);
  await page.evaluate(() => Object.assign(window, { sameDocument: true }));
  const sent = page.waitForRequest(isComparison);
  await page.evaluate((hash) => (location.hash = hash), tripLink(TRIP_B));
  expect((await sent).postDataJSON()).toEqual(TRIP_B);
  await expect(card(page, 1)).toContainText("Porto");
  await expect(destination(page, 1).getByLabel("Name", { exact: true })).toHaveValue("Porto");
  await expect(page.getByLabel("Travelers")).toHaveValue("4");
  expect(tripIn(page)).toEqual(TRIP_B);
  // It opened without loading the page again.
  expect(await page.evaluate(() => "sameDocument" in window)).toBe(true);
});

test("an older comparison answering late never replaces a newer trip", async ({ page }) => {
  const release = await holdComparisons(page, ["Lisbon"]);
  await page.goto(`/compare${tripLink(TRIP_A)}`);
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeVisible();
  await page.evaluate((hash) => (location.hash = hash), tripLink(TRIP_B));
  await expect(card(page, 1)).toContainText("Porto");

  const late = page.waitForEvent("requestfinished", { predicate: (r) => firstLabel(r) === "Lisbon" });
  release("Lisbon");
  await late;
  await settle(page);
  await expect(card(page, 1)).toContainText("Porto");
  await expect(page.getByRole("article")).toHaveCount(2);
  expect(tripIn(page)).toEqual(TRIP_B);
});

test("the form stays busy until the newest comparison answers", async ({ page }) => {
  const release = await holdComparisons(page, ["Lisbon", "Porto"]);
  await page.goto(`/compare${tripLink(TRIP_A)}`);
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeVisible();
  const newer = page.waitForRequest((r) => firstLabel(r) === "Porto");
  await page.evaluate((hash) => (location.hash = hash), tripLink(TRIP_B));
  await newer;

  // The older answer arrives first and changes nothing.
  const older = page.waitForEvent("requestfinished", { predicate: (r) => firstLabel(r) === "Lisbon" });
  release("Lisbon");
  await older;
  await settle(page);
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeDisabled();
  await expect(page.getByRole("article")).toHaveCount(0);

  release("Porto");
  await expect(card(page, 1)).toContainText("Porto");
  await expect(page.getByRole("button", { name: "Compare estimates" })).toBeEnabled();
});

test("a link opened while the place list loads replaces a comparison already out", async ({ page }) => {
  const releasePlaces = await holdPlaces(page);
  const release = await holdComparisons(page, ["Faro"]);
  const sent = watchComparisons(page);
  await page.goto("/compare");
  // The place menus wait for the list, but the rest of the form works.
  await fillTrip(page, [{ name: "Faro" }, { name: "Évora" }]);
  await page.getByRole("button", { name: "Compare estimates" }).click();
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeVisible();
  await page.evaluate((hash) => (location.hash = hash), tripLink(TRIP_B));

  const older = page.waitForEvent("requestfinished", { predicate: (r) => firstLabel(r) === "Faro" });
  release("Faro");
  await older;
  await settle(page);
  expect(tripIn(page)).toEqual(TRIP_B);
  await expect(page.getByRole("article")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeDisabled();

  releasePlaces();
  await expect(card(page, 1)).toContainText("Porto");
  expect(tripIn(page)).toEqual(TRIP_B);
  expect(sent).toEqual(["Faro", "Porto"]);
});

test("a link that cannot be opened says so at once, even while the place list loads", async ({ page }) => {
  const releasePlaces = await holdPlaces(page);
  const release = await holdComparisons(page, ["Faro"]);
  const sent = watchComparisons(page);
  await page.goto("/compare");
  await fillTrip(page, [{ name: "Faro" }, { name: "Évora" }]);
  await page.getByRole("button", { name: "Compare estimates" }).click();
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeVisible();
  await page.evaluate(() => (location.hash = "#trip=1.x"));
  await expect(pageAlert(page)).toHaveText(UNREADABLE);
  await expect(page.getByRole("button", { name: "Compare estimates" })).toBeEnabled();

  // The comparison it replaced answers late and changes nothing.
  const older = page.waitForEvent("requestfinished", { predicate: (r) => firstLabel(r) === "Faro" });
  release("Faro");
  await older;
  await settle(page);
  await expect(pageAlert(page)).toHaveText(UNREADABLE);
  expect(new URL(page.url()).hash).toBe("#trip=1.x");
  releasePlaces();
  await expect(destination(page, 1).getByLabel("Place", { exact: true })).toBeEnabled();
  await settle(page);
  await expect(page.getByRole("article")).toHaveCount(0);
  expect(sent).toEqual(["Faro"]);
});

test("a comparison still out when the page is left never changes another page's address", async ({ page }) => {
  const release = await holdComparisons(page, ["Lisbon"]);
  await page.goto("/");
  await page.getByRole("link", { name: "Compare trips" }).click();
  await expect(destination(page, 1).getByLabel("Place", { exact: true })).toBeEnabled();
  await fillTrip(page, twoPlaces);
  await page.getByRole("button", { name: "Compare estimates" }).click();
  await expect(page.getByRole("button", { name: "Comparing…" })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Flightbound" })).toBeVisible();

  const older = page.waitForEvent("requestfinished", { predicate: (r) => firstLabel(r) === "Lisbon" });
  release("Lisbon");
  await older;
  await settle(page);
  expect(new URL(page.url()).pathname).toBe("/");
  expect(new URL(page.url()).hash).toBe("");
});

test("a link that cannot be opened says so and sends nothing", async ({ context }) => {
  const data = tripLink(TRIP_A).slice("#trip=1.".length);
  const lisbonFlight = TRIP_A.destinations[0];
  for (const hash of [
    "#trip=1.x",
    `#trip=2.${data}`,
    tripLink({ ...TRIP_A, destinations: [{ ...lisbonFlight, place: "atlantis" }, TRIP_A.destinations[1]] }),
    tripLink({ ...TRIP_A, destinations: [{ ...lisbonFlight, flight_estimate: "600\n.10" }, TRIP_A.destinations[1]] }),
  ]) {
    const page = await context.newPage();
    const comparisons = watchComparisons(page);
    await page.goto(`/compare${hash}`);
    await expect(pageAlert(page)).toHaveText(UNREADABLE);
    await expect(page.getByLabel("Check-in date")).toHaveValue("");
    await expect(destination(page, 1).getByLabel("Name", { exact: true })).toHaveValue("");
    await expect(page.getByRole("heading", { name: "Results" })).toHaveCount(0);
    expect(comparisons, hash).toEqual([]);
    await page.close();
  }
});

test("a link with places cannot open without the place list", async ({ page }) => {
  await page.route("**/api/places", (route) => route.abort());
  const comparisons = watchComparisons(page);
  await page.goto(`/compare${tripLink(TRIP_A)}`);
  await expect(pageAlert(page)).toHaveText(NO_PLACE_LIST);
  await expect(page.getByLabel("Check-in date")).toHaveValue("");
  expect(comparisons).toEqual([]);
});

test("a comparison the API refuses leaves the address alone", async ({ page }) => {
  await openLink(page, TRIP_A);
  await page.getByLabel("Check-out date").fill("2027-03-01");
  await page.getByRole("button", { name: "Compare estimates" }).click();
  await expect(pageAlert(page)).toBeVisible();
  await expect(page.locator("#check_out-error")).toBeVisible();
  expect(tripIn(page)).toEqual(TRIP_A);
});

test("a reload brings back the compared trip, not later edits", async ({ page }) => {
  await openLink(page, TRIP_A);
  await destination(page, 1).getByLabel("Name", { exact: true }).fill("Edited");
  await expect(page.getByText("You changed the trip after this comparison.")).toBeVisible();
  await page.reload();
  await expect(page.getByRole("article")).toHaveCount(2);
  await expect(destination(page, 1).getByLabel("Name", { exact: true })).toHaveValue("Lisbon");
  await expect(page.getByText("You changed the trip after this comparison.")).toHaveCount(0);
});

function isComparison(request: Request) {
  return request.method() === "POST" && new URL(request.url()).pathname === "/api/trip-comparisons";
}

function firstLabel(request: Request): string | undefined {
  return isComparison(request) ? request.postDataJSON().destinations[0].label : undefined;
}

// openLink opens the page at the link for request and waits for its results.
async function openLink(page: Page, request: ComparisonRequest) {
  await page.goto(`/compare${tripLink(request)}`);
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();
}

// pageAlert is the page's own alert. Next.js keeps an empty route announcer
// with the same role.
function pageAlert(page: Page) {
  return page.getByRole("alert").filter({ hasText: /\S/ });
}

// tripIn reads the trip in the page's address.
function tripIn(page: Page) {
  return readTripLink(new URL(page.url()).hash);
}

async function formValues(page: Page) {
  return page.locator("form").evaluate((form) =>
    Object.fromEntries([...form.querySelectorAll("input, select")].map((e) => [e.id, (e as HTMLInputElement).value])),
  );
}

// watchComparisons collects the first destination's label of each
// comparison request page sends.
function watchComparisons(page: Page) {
  const sent: string[] = [];
  page.on("request", (request) => {
    if (isComparison(request)) {
      sent.push(firstLabel(request) ?? "");
    }
  });
  return sent;
}

// holdPlaces holds the place list until the returned function is called.
async function holdPlaces(page: Page) {
  let open = () => {};
  const opened = new Promise<void>((resolve) => (open = resolve));
  await page.route("**/api/places", async (route) => {
    await opened;
    await route.fallback();
  });
  return open;
}

// holdComparisons holds each comparison whose first destination has one of
// labels until release is called with that label. Other requests pass on to
// the guard, which runs after page routes.
async function holdComparisons(page: Page, labels: string[]) {
  const gates = new Map(
    labels.map((label) => {
      let open = () => {};
      const opened = new Promise<void>((resolve) => (open = resolve));
      return [label, { opened, open }] as const;
    }),
  );
  await page.route("**/api/trip-comparisons", async (route) => {
    await gates.get(firstLabel(route.request()) ?? "")?.opened;
    await route.fallback();
  });
  return (label: string) => gates.get(label)?.open();
}

// settle gives the page time to handle an answer that just arrived.
async function settle(page: Page) {
  await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));
}

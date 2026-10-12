import type { Locator, Page } from "@playwright/test";
import type { Comparison, ClimateMonth } from "../app/compare/api";
import { card, compare, destination, expect, fillTrip, openCompare, test } from "./support";

// The results' wording for the budget check, travel advice, and typical
// weather, and the form's field errors. The API under test serves saved
// travel advice for Portugal, Thailand, Afghanistan, and Ukraine only, and
// the saved ECB rates. Amounts are checked against the API's answer, so the
// page must show its values as they are.

test("each destination says how it compares with the budget", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, [
    { name: "Lisbon", place: "Lisbon" },
    { name: "Tokyo", place: "Tokyo" },
    { name: "Porto" },
  ]);
  await page.getByLabel("Currency for totals").selectOption("EUR");
  await page.getByLabel("Trip budget for the whole party").fill("1000");
  // Three nights: 700, 1200, and a flight alone of 1100.
  await setEstimates(page, 1, "400", "100");
  await setEstimates(page, 2, "900", "100");
  await setEstimates(page, 3, "1100", "");
  let result = await submit(page);
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();
  expect(result.destinations.map((d) => d.budget_status)).toEqual(["within", "over", "over_at_least"]);

  await expect(page.getByText(/^Trip budget:/)).toHaveText("Trip budget: EUR 1000.00");
  await expect(card(page, 1).getByText(/budget/)).toHaveText("Leaves EUR 300.00 of your budget for everything else.");
  await expect(card(page, 2).getByText(/budget/)).toHaveText("EUR 200.00 over your budget on flights and stay alone.");
  await expect(card(page, 3).getByText(/budget/)).toHaveText("At least EUR 100.00 over your budget on flights alone.");
  await expect(page.getByText("This page's address now holds this trip")).toHaveText(
    "This page's address now holds this trip, so you can bookmark it or send it. Anyone with the link can see these estimates and your budget.",
  );

  // An incomplete destination under the budget cannot be checked yet; one
  // whose stay alone is over it can.
  await setEstimates(page, 1, "400", "");
  await setEstimates(page, 2, "", "400");
  result = await submit(page);
  await expect(page.getByText("You changed the trip after this comparison.")).toHaveCount(0);
  expect(result.destinations.map((d) => d.budget_status)).toEqual(["unknown", "over_at_least", "over_at_least"]);
  await expect(card(page, 1).getByText(/budget/)).toHaveText("Your budget can be checked once this destination has a total.");
  await expect(card(page, 2).getByText(/budget/)).toHaveText("At least EUR 200.00 over your budget on the stay alone.");
});

test("a budget in another currency shows its converted amount", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, [{ name: "Lisbon" }, { name: "Porto" }]);
  await page.getByLabel("Currency for totals").selectOption("EUR");
  await page.getByLabel("Trip budget for the whole party").fill("1000");
  await page.getByLabel("Currency of the budget").selectOption("USD");
  await setEstimates(page, 1, "400", "100");
  await setEstimates(page, 2, "900", "100");
  const result = await submit(page);
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();

  expect(result.converted_budget).not.toBeNull();
  const summary = page.getByText(/^Trip budget:/);
  await expect(summary).toContainText("Trip budget: USD 1000.00");
  await expect(summary.getByText(/^≈/)).toHaveText(`≈ EUR ${result.converted_budget}`);
  for (const [i, d] of result.destinations.entries()) {
    expect(d.budget_status, d.label).not.toBe("unknown");
    const line =
      d.budget_status === "within"
        ? `Leaves EUR ${d.budget_difference} of your budget for everything else.`
        : `EUR ${d.budget_difference} over your budget on flights and stay alone.`;
    await expect(card(page, i + 1).getByText(/budget/)).toHaveText(line);
  }
});

test("a trip without a budget, places, or countries shows none of their notes", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, [{ name: "Lisbon" }, { name: "Porto" }]);
  await setEstimates(page, 1, "400", "100");
  await setEstimates(page, 2, "900", "100");
  await submit(page);
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();

  await expect(results(page).getByText(/budget/)).toHaveText([
    "These are your own estimates, not live prices or supplier offers. They cover flights and accommodation only, not a full trip budget.",
  ]);
  await expect(page.getByText("This page's address now holds this trip")).toHaveText(
    "This page's address now holds this trip, so you can bookmark it or send it. Anyone with the link can see these estimates.",
  );
  await expect(results(page).getByText(/FCDO|Travel advice|GOV\.UK/)).toHaveCount(0);
  await expect(results(page).getByText(/Typical weather|Copernicus/)).toHaveCount(0);
});

test("each destination shows its country's travel advice", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, [
    { name: "Lisbon", place: "Lisbon" },
    { name: "Bangkok", place: "Bangkok" },
    { name: "Kabul" },
  ]);
  await country(page, 3, "Afghanistan");
  let result = await submit(page);
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();

  const lisbon = card(page, 1);
  await expect(lisbon.getByText(/FCDO/)).toHaveText("FCDO does not advise against travel to any part of Portugal.");
  await expect(lisbon.getByText(/^Updated/)).toHaveText("Updated 11 August 2026.");
  await expect(lisbon.getByText(/affected area|Checked/)).toHaveCount(0);
  await expect(lisbon.getByRole("link", { name: "Read the full advice on GOV.UK" })).toHaveAttribute(
    "href",
    advice(result, 0).url,
  );

  const bangkok = card(page, 2);
  await expect(bangkok.getByText(/FCDO/)).toHaveText("FCDO advises against all but essential travel to parts of Thailand.");
  await expect(bangkok.getByText(/affected area/)).toHaveText(
    "Check whether the places you plan to visit are in an affected area.",
  );
  await expect(bangkok.getByText(/^Updated/)).toHaveText("Updated 28 September 2026.");

  const kabul = card(page, 3);
  await expect(kabul.getByText(/FCDO/)).toHaveText("FCDO advises against all travel to Afghanistan.");
  await expect(kabul.getByText(/affected area/)).toHaveCount(0);
  await expect(kabul.getByText(/^Updated/)).toHaveText("Updated 30 July 2026.");
  await expect(page.getByText(/^Travel advice comes from/)).toContainText("Open Government Licence v3.0");

  // Two alerts in the order the API lists them, a country without a saved
  // copy, and a destination with no country.
  await destination(page, 1).getByLabel("Place", { exact: true }).selectOption({ label: "Not listed" });
  await destination(page, 1).getByLabel("Name", { exact: true }).fill("Kyiv");
  await country(page, 1, "Ukraine");
  await destination(page, 2).getByLabel("Place", { exact: true }).selectOption({ label: "Tokyo" });
  await destination(page, 2).getByLabel("Name", { exact: true }).fill("Tokyo");
  await destination(page, 3).getByLabel("Name", { exact: true }).fill("Porto");
  await country(page, 3, "None");
  result = await submit(page);
  await expect(page.getByText("You changed the trip after this comparison.")).toHaveCount(0);

  const kyiv = card(page, 1);
  const ukraine = advice(result, 0);
  expect(ukraine.alerts).toEqual(["avoid_all_travel_to_parts", "avoid_all_but_essential_travel_to_parts"]);
  await expect(kyiv.getByText(/FCDO/)).toHaveText([
    "FCDO advises against all travel to parts of Ukraine.",
    "FCDO advises against all but essential travel to parts of Ukraine.",
  ]);
  await expect(kyiv.getByText(/affected area/)).toHaveCount(1);
  await expect(kyiv.getByText(/^Updated/)).toHaveText("Updated 2 October 2026.");

  const tokyo = card(page, 2);
  expect(result.destinations[1].travel_advice?.available).toBe(false);
  await expect(tokyo.getByText(/Travel advice/)).toHaveText("Travel advice unavailable. Check the latest advice on GOV.UK.");
  await expect(tokyo.getByRole("link", { name: "Check the latest advice on GOV.UK" })).toHaveAttribute(
    "href",
    result.destinations[1].travel_advice!.url,
  );
  await expect(tokyo.getByText(/FCDO|Updated/)).toHaveCount(0);

  await expect(card(page, 3).getByText(/FCDO|Travel advice|GOV\.UK/)).toHaveCount(0);
});

test("each place shows its typical weather for the months of the stay", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, [
    { name: "Lisbon", place: "Lisbon" },
    { name: "Cairo", place: "Cairo" },
    { name: "Porto" },
  ]);
  await page.getByLabel("Check-in date").fill("2027-03-30");
  await page.getByLabel("Check-out date").fill("2027-04-02");
  let result = await submit(page);
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();

  const lisbon = climate(result, 0);
  expect(lisbon.months.map((m) => m.month)).toEqual([3, 4]);
  const lisbonLines = card(page, 1).getByText(/^Typical weather/);
  await expect(lisbonLines).toHaveText(lisbon.months.map((m) => weatherLine("Lisbon", m)));
  // toHaveText treats a non-breaking space as a space, so compare the raw text.
  expect(await lisbonLines.allTextContents()).toEqual(lisbon.months.map((m) => weatherLine("Lisbon", m)));
  await expect(card(page, 3).getByText(/Typical weather/)).toHaveCount(0);
  await expect(page.getByText(/^Typical weather is the/)).toContainText(
    `Typical weather is the ${lisbon.period_start}-${lisbon.period_end} average for a land area about 9\u00a0km wide`,
  );
  await expect(page.getByText(/Copernicus Climate Change Service/)).toBeVisible();

  // Cairo's driest months: under a millimetre, and under a tenth of an inch.
  await page.getByLabel("Check-in date").fill("2027-05-30");
  await page.getByLabel("Check-out date").fill("2027-06-02");
  result = await submit(page);
  await expect(page.getByText("You changed the trip after this comparison.")).toHaveCount(0);
  const [may, june] = climate(result, 1).months;
  expect([may.month, may.precipitation_mm, may.precipitation_in]).toEqual([5, 1, "0.0"]);
  expect([june.month, june.precipitation_mm]).toEqual([6, 0]);
  const cairo = card(page, 2).getByText(/^Typical weather/);
  await expect(cairo).toHaveText([weatherLine("Cairo", may), weatherLine("Cairo", june)]);
  expect(await cairo.allTextContents()).toEqual([weatherLine("Cairo", may), weatherLine("Cairo", june)]);
  await expect(cairo.first()).toContainText("about 1\u00a0mm (less than 0.1\u00a0in) of precipitation.");
  await expect(cairo.last()).toContainText("less than 1\u00a0mm of precipitation.");
});

test("field errors show next to their fields until the trip is fixed", async ({ page }) => {
  await openCompare(page);
  let body = await submitForError(page);
  // Next.js adds its own, empty alert: the route announcer.
  const alert = page.locator('p[role="alert"]');
  await expect(alert).toHaveText(body.message);
  await expect(page.getByRole("heading", { name: "Results" })).toHaveCount(0);
  await expectFieldError(page.getByLabel("Check-in date"), "Enter a check-in date.");
  await expectFieldError(page.getByLabel("Check-out date"), "Enter a check-out date.");
  for (const n of [1, 2]) {
    await expectFieldError(destination(page, n).getByLabel("Name", { exact: true }), "Enter a destination name.");
  }

  // Errors from the amounts and the dates; the names are fixed now.
  await fillTrip(page, [{ name: "Lisbon" }, { name: "Tokyo" }]);
  await page.getByLabel("Check-out date").fill("2027-03-09");
  await page.getByLabel("Currency for totals").selectOption("EUR");
  await page.getByLabel("Trip budget for the whole party").fill("1,000");
  await destination(page, 1).getByLabel("Round-trip flights for the whole party").fill("10.555");
  await destination(page, 2).getByLabel("Currency of the accommodation estimate").selectOption("JPY");
  await destination(page, 2).getByLabel("Accommodation per night for the whole party").fill("1.5");
  body = await submitForError(page);
  await expect(alert).toHaveText(body.message);
  await expectFieldError(page.getByLabel("Check-out date"), "Check-out must be after check-in.");
  await expectFieldError(
    page.getByLabel("Trip budget for the whole party"),
    "Use digits with an optional decimal point, like 1250.50.",
    "Optional. Each destination shows what its flights and stay leave for everything else.",
  );
  await expectFieldError(
    destination(page, 1).getByLabel("Round-trip flights for the whole party"),
    "EUR amounts can have at most 2 decimal places.",
    "Optional. Leave blank if unknown.",
  );
  await expectFieldError(
    destination(page, 2).getByLabel("Accommodation per night for the whole party"),
    "JPY amounts must be whole numbers.",
    "Optional. Leave blank if unknown.",
  );
  await expect(page.locator("[aria-invalid]")).toHaveCount(4);

  // Fixing every field and comparing again clears the errors.
  await page.getByLabel("Check-out date").fill("2027-03-13");
  await page.getByLabel("Trip budget for the whole party").fill("1000");
  await destination(page, 1).getByLabel("Round-trip flights for the whole party").fill("10.55");
  await destination(page, 2).getByLabel("Accommodation per night for the whole party").fill("15000");
  await compare(page);
  await expect(alert).toHaveCount(0);
  await expect(page.locator("[aria-invalid]")).toHaveCount(0);
  await expect(page.locator("[id$='-error']")).toHaveCount(0);

  // Removing a destination clears the errors, which name positions.
  await page.getByRole("button", { name: "Add a third destination" }).click();
  body = await submitForError(page);
  expect(body.field_errors).toEqual([{ field: "destinations[2].label", message: "Enter a destination name." }]);
  await expectFieldError(destination(page, 3).getByLabel("Name", { exact: true }), "Enter a destination name.");
  await page.getByRole("button", { name: "Remove destination 3" }).click();
  await expect(alert).toHaveCount(0);
  await expect(page.locator("[aria-invalid]")).toHaveCount(0);
});

function results(page: Page) {
  return page.getByRole("region", { name: "Results" });
}

// setEstimates fills destination n's flight and nightly stay estimates; an
// empty string leaves one blank.
async function setEstimates(page: Page, n: number, flight: string, nightly: string) {
  const group = destination(page, n);
  await group.getByLabel("Round-trip flights for the whole party").fill(flight);
  await group.getByLabel("Accommodation per night for the whole party").fill(nightly);
}

async function country(page: Page, n: number, name: string) {
  await destination(page, n).getByLabel("Country for travel advice").selectOption({ label: name });
}

// submit compares the trip and returns the comparison the API answered.
async function submit(page: Page): Promise<Comparison> {
  const response = page.waitForResponse(isComparison);
  await page.getByRole("button", { name: "Compare estimates" }).click();
  const answer = await response;
  expect(answer.status()).toBe(200);
  return answer.json();
}

// submitForError compares the trip and returns the API's refusal.
async function submitForError(page: Page): Promise<{ message: string; field_errors: { field: string; message: string }[] }> {
  const response = page.waitForResponse(isComparison);
  await page.getByRole("button", { name: "Compare estimates" }).click();
  const answer = await response;
  expect(answer.status()).toBe(422);
  return answer.json();
}

function isComparison(response: { url(): string; request(): { method(): string } }) {
  return new URL(response.url()).pathname === "/api/trip-comparisons" && response.request().method() === "POST";
}

// expectFieldError checks that a control is marked invalid, that its error
// is shown with its field, and that screen readers announce the error after
// the field's hint when it has one.
async function expectFieldError(control: Locator, message: string, hint?: string) {
  await expect(control).toHaveAttribute("aria-invalid", "true");
  const error = control.page().locator(`#${await control.getAttribute("id")}-error`);
  await expect(error).toBeVisible();
  await expect(error).toHaveText(message);
  await expect(control).toHaveAccessibleDescription(hint ? `${hint} ${message}` : message);
}

function advice(result: Comparison, i: number) {
  const a = result.destinations[i].travel_advice;
  if (a === null || !a.available) {
    throw new Error(`destination ${i + 1} has no travel advice`);
  }
  return a;
}

function climate(result: Comparison, i: number) {
  const c = result.destinations[i].climate;
  if (c === null) {
    throw new Error(`destination ${i + 1} has no typical weather`);
  }
  return c;
}

const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

// weatherLine is the sentence the page should show for one month, with the
// API's rounded values and a non-breaking space before each unit.
function weatherLine(place: string, m: ClimateMonth) {
  const precipitation =
    m.precipitation_mm === 0
      ? "less than 1\u00a0mm of precipitation"
      : `about ${m.precipitation_mm}\u00a0mm (${m.precipitation_in === "0.0" ? "less than 0.1\u00a0in" : `${m.precipitation_in}\u00a0in`}) of precipitation`;
  return (
    `Typical weather in ${place} in ${MONTHS[m.month - 1]}: highs ${m.high_c}\u00a0°C (${m.high_f}\u00a0°F), ` +
    `lows ${m.low_c}\u00a0°C (${m.low_f}\u00a0°F), ${precipitation}.`
  );
}

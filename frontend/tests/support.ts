import { expect, test as base, type BrowserContext, type Page } from "@playwright/test";

export type Guard = { allowed: string[]; blocked: string[] };

// installGuard keeps every request in context, from pages and their
// workers, on origin. It records each request and aborts any other before
// it leaves the browser. A browser follows a redirect without asking the
// route again, so the guard makes each allowed request itself, follows no
// redirect, and refuses any (recorded as "<url> -> <location>").
export async function installGuard(context: BrowserContext, origin: string): Promise<Guard> {
  const guard: Guard = { allowed: [], blocked: [] };
  await context.route("**/*", async (route) => {
    const url = route.request().url();
    if (new URL(url).origin !== origin) {
      guard.blocked.push(url);
      return route.abort("blockedbyclient");
    }
    let response;
    try {
      response = await route.fetch({ maxRedirects: 0 });
    } catch {
      // The page or its context closed while the request was out.
      return route.abort().catch(() => undefined);
    }
    const location = response.headers()["location"];
    if (response.status() >= 300 && response.status() < 400 && location !== undefined) {
      guard.blocked.push(`${url} -> ${new URL(location, url).href}`);
      return route.abort("blockedbyclient");
    }
    guard.allowed.push(url);
    return route.fulfill({ response });
  });
  return guard;
}

// Every test runs behind the guard for the web origin under test and fails
// if anything tried to leave it.
export const test = base.extend<{ guard: Guard }>({
  guard: [
    async ({ context, baseURL }, use) => {
      const guard = await installGuard(context, new URL(baseURL!).origin);
      await use(guard);
      expect(guard.blocked, "requests outside the web origin").toEqual([]);
    },
    { auto: true },
  ],
});

export { expect };

export type Destination = { name: string; place?: string };

export const FALLBACK = "The map could not be shown in this browser. Every destination is listed below.";

export const twoPlaces: Destination[] = [
  { name: "Lisbon", place: "Lisbon" },
  { name: "Tokyo", place: "Tokyo" },
];

export async function openCompare(page: Page) {
  await page.goto("/compare");
  // The place menus wait for the place list.
  await expect(destination(page, 1).getByLabel("Place", { exact: true })).toBeEnabled();
}

export async function fillTrip(page: Page, destinations: Destination[]) {
  await page.getByLabel("Check-in date").fill("2027-03-10");
  await page.getByLabel("Check-out date").fill("2027-03-13");
  if (destinations.length === 3) {
    await page.getByRole("button", { name: "Add a third destination" }).click();
  }
  for (const [i, d] of destinations.entries()) {
    const group = destination(page, i + 1);
    await group.getByLabel("Name", { exact: true }).fill(d.name);
    if (d.place) {
      await group.getByLabel("Place", { exact: true }).selectOption({ label: d.place });
    }
  }
}

export async function compare(page: Page) {
  await page.getByRole("button", { name: "Compare estimates" }).click();
  await expect(page.getByRole("heading", { name: "Results" })).toBeVisible();
}

export function destination(page: Page, n: number) {
  return page.getByRole("group", { name: `Destination ${n}`, exact: true });
}

export function pins(page: Page) {
  return page.getByRole("button", { name: /^Destinations? \d/ });
}

// pinsInsideMap reports whether every pin lies wholly inside the map's
// canvas, which MapLibre labels "Map".
export async function pinsInsideMap(page: Page) {
  const map = await page.getByRole("region", { name: "Map" }).boundingBox();
  const boxes = await Promise.all((await pins(page).all()).map((pin) => pin.boundingBox()));
  return (
    map !== null &&
    boxes.length > 0 &&
    boxes.every(
      (b) =>
        b !== null &&
        b.x >= map.x &&
        b.y >= map.y &&
        b.x + b.width <= map.x + map.width &&
        b.y + b.height <= map.y + map.height,
    )
  );
}

export function card(page: Page, n: number) {
  return page.getByRole("article").nth(n - 1);
}

export function showOnMap(page: Page, n: number) {
  return card(page, n).getByRole("button", { name: "Show on map" });
}

// expectFallback checks that the note stands in for the map, the cards
// still show their estimates, and with no map they drop their numbers and
// map controls. The default wait is well under the page's 10 s load
// timeout, so a failure the page should catch at once cannot pass by timing
// out instead.
export async function expectFallback(page: Page, { timeout = 5_000, cards = 2 } = {}) {
  await expect(page.getByText(FALLBACK)).toBeVisible({ timeout });
  await expect(pins(page)).toHaveCount(0);
  await expect(page.getByRole("article")).toHaveCount(cards);
  await expect(card(page, 1)).toContainText("Flight + stay estimate");
  await expect(card(page, 1).getByRole("heading")).toHaveText("Lisbon");
  await expect(page.getByRole("button", { name: "Show on map" })).toHaveCount(0);
  await expect(page.getByText("Not on the map")).toHaveCount(0);
}

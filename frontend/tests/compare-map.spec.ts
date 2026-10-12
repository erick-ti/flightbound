import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import type { Page } from "@playwright/test";
import {
  card,
  compare,
  destination,
  expect,
  expectFallback,
  fillTrip,
  installGuard,
  openCompare,
  pins,
  pinsInsideMap,
  showOnMap,
  test,
  twoPlaces,
} from "./support";

// Chromium refuses some requests on its own: a page the guard serves has no
// network address, so a request from it to a loopback name the guard lets
// through can still fail. The guard controls therefore check each failure's
// error. This one comes only from a block through the DevTools protocol,
// which is how the guard aborts.
const BLOCKED_BY_GUARD = "net::ERR_BLOCKED_BY_CLIENT.Inspector";

test("the guard blocks other origins, from the page and from a worker", async ({ page, guard, baseURL }) => {
  // The same local server under its other host name is another origin that
  // answers outside the browser.
  const alias = new URL("/api/places", baseURL);
  alias.hostname = alias.hostname === "localhost" ? "127.0.0.1" : "localhost";
  const other = alias.href;
  expect((await fetch(other)).ok, `${other} answers outside the browser`).toBe(true);
  const failures = failedRequests(page);

  await openCompare(page);
  await page.evaluate((url) => fetch(url, { mode: "no-cors" }).catch(() => undefined), other);
  await fetchFromWorker(page, other);
  await expect.poll(() => failures.filter((f) => f.startsWith(other))).toEqual([
    `${other} ${BLOCKED_BY_GUARD}`,
    `${other} ${BLOCKED_BY_GUARD}`,
  ]);
  expect(guard.blocked).toEqual([other, other]);
  guard.blocked.length = 0;
});

test("the guard refuses redirects, from the page and from a worker", async ({ browser }) => {
  // A local server plays a web origin whose /hop redirects to another
  // origin: the same server under its other host name, which counts
  // arrivals.
  let landings = 0;
  const server = createServer((request, response) => {
    const { port } = server.address() as AddressInfo;
    if (request.url === "/hop") {
      response.writeHead(302, { location: `http://localhost:${port}/landing` }).end();
    } else if (request.url === "/landing") {
      landings += 1;
      response.writeHead(200, { "content-type": "text/plain" }).end("landed");
    } else {
      response.writeHead(200, { "content-type": "text/html" }).end("<!doctype html><title>Redirects</title>");
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  const origin = `http://127.0.0.1:${port}`;
  const hop = `${origin}/hop`;
  try {
    // Without the guard both requests follow the redirect and land.
    const open = await browser.newContext();
    await requestTwice(await open.newPage(), origin, hop);
    await open.close();
    expect(landings).toBe(2);

    // With it, the guard refuses both redirects itself.
    const guarded = await browser.newContext();
    const guard = await installGuard(guarded, origin);
    const page = await guarded.newPage();
    const failures = failedRequests(page);
    await requestTwice(page, origin, hop);
    await expect.poll(() => failures).toEqual([`${hop} ${BLOCKED_BY_GUARD}`, `${hop} ${BLOCKED_BY_GUARD}`]);
    await guarded.close();
    expect(landings).toBe(2);
    const refused = `${hop} -> http://localhost:${port}/landing`;
    expect(guard.blocked).toEqual([refused, refused]);
  } finally {
    server.close();
  }
});

// failedRequests collects "<url> <error>" for each request of page or its
// workers that fails.
function failedRequests(page: Page) {
  const failures: string[] = [];
  page.on("requestfailed", (request) => failures.push(`${request.url()} ${request.failure()?.errorText}`));
  return failures;
}

// requestTwice opens origin and requests url from the page and from a
// dedicated worker.
async function requestTwice(page: Page, origin: string, url: string) {
  await page.goto(`${origin}/`);
  await page.evaluate((u) => fetch(u).catch(() => undefined), url);
  await fetchFromWorker(page, url);
}

// fetchFromWorker requests url from a dedicated worker and waits for it to
// settle.
async function fetchFromWorker(page: Page, url: string) {
  await page.evaluate(async (u) => {
    const source = `fetch(${JSON.stringify(u)}, { mode: "no-cors" }).catch(() => {}).finally(() => postMessage("done"));`;
    const worker = new Worker(URL.createObjectURL(new Blob([source], { type: "text/javascript" })));
    await new Promise((resolve) => worker.addEventListener("message", resolve));
    worker.terminate();
  }, url);
}

test("the map and the cards select the same place", async ({ page, guard }) => {
  await openCompare(page);
  await fillTrip(page, [{ name: "Lisbon", place: "Lisbon" }, { name: "Tokyo", place: "Tokyo" }, { name: "Porto" }]);
  await compare(page);

  const lisbon = page.getByRole("button", { name: "Destination 1: Lisbon" });
  const tokyo = page.getByRole("button", { name: "Destination 2: Tokyo" });
  await expect(lisbon).toBeVisible();
  await expect(tokyo).toBeVisible();
  await expect(pins(page)).toHaveCount(2);
  await expect(card(page, 3)).toContainText("Not on the map: no place chosen.");
  await expect(showOnMap(page, 3)).toHaveCount(0);

  // A pin selects its card, and only its card.
  await tokyo.click();
  await expect(tokyo).toHaveAttribute("aria-pressed", "true");
  await expect(showOnMap(page, 2)).toHaveAttribute("aria-pressed", "true");
  await expect(showOnMap(page, 1)).toHaveAttribute("aria-pressed", "false");
  await expect(lisbon).toHaveAttribute("aria-pressed", "false");

  // A card selects its pin and clears the other.
  await showOnMap(page, 1).click();
  await expect(lisbon).toHaveAttribute("aria-pressed", "true");
  await expect(tokyo).toHaveAttribute("aria-pressed", "false");
  await expect(showOnMap(page, 2)).toHaveAttribute("aria-pressed", "false");

  // Selecting it again clears the selection.
  await showOnMap(page, 1).click();
  await expect(lisbon).toHaveAttribute("aria-pressed", "false");
  await expect(showOnMap(page, 1)).toHaveAttribute("aria-pressed", "false");

  // Editing the trip moves no pin until the next comparison.
  await showOnMap(page, 1).click();
  await destination(page, 2).getByLabel("Name", { exact: true }).fill("Rome");
  await destination(page, 2).getByLabel("Place", { exact: true }).selectOption({ label: "Rome" });
  await expect(page.getByText("You changed the trip after this comparison.")).toBeVisible();
  await expect(tokyo).toBeVisible();
  await expect(pins(page)).toHaveCount(2);
  await expect(lisbon).toHaveAttribute("aria-pressed", "true");

  // A new comparison draws the new places with nothing selected.
  await compare(page);
  const rome = page.getByRole("button", { name: "Destination 2: Rome" });
  await expect(rome).toBeVisible();
  await expect(tokyo).toHaveCount(0);
  await expect(pins(page)).toHaveCount(2);
  await expect(lisbon).toHaveAttribute("aria-pressed", "false");
  await expect(rome).toHaveAttribute("aria-pressed", "false");
  await expect(showOnMap(page, 1)).toHaveAttribute("aria-pressed", "false");

  // The map files and MapLibre's worker came through the guard, so it sees
  // the worker's requests too.
  for (const path of [
    /\/map\/ne_110m_land\.geojson$/,
    /\/map\/ne_110m_lakes\.geojson$/,
    /\/map\/ne_110m_admin_0_boundary_lines_land\.geojson$/,
    /\/maplibre-gl-worker\.[^/]*\.mjs$/,
  ]) {
    expect(guard.allowed.some((url) => path.test(url)), String(path)).toBe(true);
  }
});

test("destinations at the same place share one pin", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, [{ name: "Lisbon", place: "Lisbon" }, { name: "Lisbon in May", place: "Lisbon" }]);
  await compare(page);

  const pin = page.getByRole("button", { name: "Destinations 1 and 2: Lisbon, Lisbon in May" });
  await expect(pin).toHaveText("1,2");
  await expect(pins(page)).toHaveCount(1);
  await pin.click();
  await expect(pin).toHaveAttribute("aria-pressed", "true");
  await expect(showOnMap(page, 1)).toHaveAttribute("aria-pressed", "true");
  await expect(showOnMap(page, 2)).toHaveAttribute("aria-pressed", "true");
});

test("the pins stay in view when the map gets narrower", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openCompare(page);
  await fillTrip(page, twoPlaces);
  await compare(page);
  await expect(pins(page)).toHaveCount(2);
  await expect.poll(() => pinsInsideMap(page)).toBe(true);
  // Lisbon and Tokyo sit near the two edges, so a narrower map loses them
  // unless it fits them again.
  await page.setViewportSize({ width: 820, height: 1180 });
  await expect.poll(() => pinsInsideMap(page)).toBe(true);
});

test("a map file that fails to load leaves the cards", async ({ page, context }) => {
  // Routes registered later run first, so this answers before the guard.
  await context.route("**/map/ne_110m_lakes.geojson", (route) => route.fulfill({ status: 404, body: "" }));
  await openCompare(page);
  await fillTrip(page, [...twoPlaces, { name: "Porto" }]);
  await compare(page);
  await expectFallback(page, { cards: 3 });
});

test("a map that never finishes loading gives way to the cards", async ({ page, context }) => {
  // Never answered, so only the page's load timeout can end the wait.
  await context.route("**/map/ne_110m_land.geojson", () => undefined);
  await openCompare(page);
  await fillTrip(page, twoPlaces);
  await compare(page);
  await expectFallback(page, { timeout: 15_000 });
});

test("a map library that fails to load leaves the cards", async ({ page, context }) => {
  await openCompare(page);
  // The page has its code by now, so any new chunk is the map library.
  await context.route("**/_next/static/chunks/**", (route) => route.abort());
  await fillTrip(page, twoPlaces);
  await compare(page);
  await expectFallback(page);
});

import { compare, expectFallback, fillTrip, openCompare, test, twoPlaces } from "./support";

// A separate file, because launch options apply to a whole file's browser.
test.use({ launchOptions: { args: ["--disable-webgl", "--disable-3d-apis"] } });

test("without WebGL, the cards stand in for the map", async ({ page }) => {
  await openCompare(page);
  await fillTrip(page, twoPlaces);
  await compare(page);
  await expectFallback(page);
});

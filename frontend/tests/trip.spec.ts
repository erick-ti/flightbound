// Trip links without a browser: the format, what readTripLink refuses, and
// the round trips through tripLink, readTripLink, toDraft and toRequest.
// These tests import test from Playwright directly, so they start no
// browser.
import { expect, test } from "@playwright/test";
import type { ComparisonRequest, Place } from "../app/compare/api";
import { CURRENCIES, readTripLink, toDraft, toRequest, tripLink } from "../app/compare/trip";

const PLACES: Place[] = [
  { id: "bangkok", name: "Bangkok", country: "thailand", country_name: "Thailand" },
  { id: "lisbon", name: "Lisbon", country: "portugal", country_name: "Portugal" },
  { id: "tokyo", name: "Tokyo", country: "japan", country_name: "Japan" },
];

// A version 1 link as the page first wrote it. It must keep opening as this
// request whatever happens to the encoder.
const VERSION_1_LINK =
  "#trip=1.eyJjaGVja19pbiI6IjIwMjctMDMtMTAiLCJjaGVja19vdXQiOiIyMDI3LTAzLTEzIiwidHJhdmVsZXJzIjoyLCJjdXJyZW5jeSI6IlVTRCIsImJ1ZGdldCI6IjEwMDAuMDAiLCJidWRnZXRfY3VycmVuY3kiOiJFVVIiLCJkZXN0aW5hdGlvbnMiOlt7ImxhYmVsIjoiTGlzYm9uIiwicGxhY2UiOiJsaXNib24iLCJjb3VudHJ5IjpudWxsLCJmbGlnaHRfZXN0aW1hdGUiOiI2MDAuMTAiLCJmbGlnaHRfZXN0aW1hdGVfY3VycmVuY3kiOiJVU0QiLCJuaWdodGx5X3N0YXlfZXN0aW1hdGUiOiIxMjUuMjUiLCJuaWdodGx5X3N0YXlfZXN0aW1hdGVfY3VycmVuY3kiOiJFVVIifSx7ImxhYmVsIjoiUG9ydG8sIFPDo28gQmVudG8g8J-ahiIsInBsYWNlIjpudWxsLCJjb3VudHJ5IjoicG9ydHVnYWwiLCJmbGlnaHRfZXN0aW1hdGUiOm51bGwsImZsaWdodF9lc3RpbWF0ZV9jdXJyZW5jeSI6IkdCUCIsIm5pZ2h0bHlfc3RheV9lc3RpbWF0ZSI6bnVsbCwibmlnaHRseV9zdGF5X2VzdGltYXRlX2N1cnJlbmN5IjoiVVNEIn1dfQ";

const VERSION_1_REQUEST: ComparisonRequest = {
  check_in: "2027-03-10",
  check_out: "2027-03-13",
  travelers: 2,
  currency: "USD",
  budget: "1000.00",
  budget_currency: "EUR",
  destinations: [
    {
      label: "Lisbon",
      place: "lisbon",
      country: null,
      flight_estimate: "600.10",
      flight_estimate_currency: "USD",
      nightly_stay_estimate: "125.25",
      nightly_stay_estimate_currency: "EUR",
    },
    {
      label: "Porto, São Bento 🚆",
      place: null,
      country: "portugal",
      flight_estimate: null,
      flight_estimate_currency: "GBP",
      nightly_stay_estimate: null,
      nightly_stay_estimate_currency: "USD",
    },
  ],
};

// A request with each kind of value: surrounding spaces and characters
// outside ASCII in a label, unknown estimates, every way an amount's
// currency can be set, a place and a country without a place.
const RICH: ComparisonRequest = {
  check_in: "2028-02-28",
  check_out: "2028-03-02",
  travelers: 20,
  currency: "JPY",
  budget: null,
  budget_currency: "CHF",
  destinations: [
    {
      label: "  Bangkok \"old town\" \\ <b>",
      place: "bangkok",
      country: null,
      flight_estimate: "0.10",
      flight_estimate_currency: "THB",
      nightly_stay_estimate: "600.1",
      nightly_stay_estimate_currency: "JPY",
    },
    {
      label: "Sétubal\t🌊",
      place: null,
      country: "portugal",
      flight_estimate: null,
      flight_estimate_currency: "JPY",
      nightly_stay_estimate: null,
      nightly_stay_estimate_currency: "EUR",
    },
    {
      label: "",
      place: null,
      country: null,
      flight_estimate: "0",
      flight_estimate_currency: "JPY",
      nightly_stay_estimate: "1,5",
      nightly_stay_estimate_currency: "JPY",
    },
  ],
};

test("a version 1 link opens as the request it was made from", () => {
  expect(readTripLink(VERSION_1_LINK)).toEqual(VERSION_1_REQUEST);
  expect(tripLink(VERSION_1_REQUEST)).toBe(VERSION_1_LINK);
});

test("a link round trips every kind of value", () => {
  const link = tripLink(RICH);
  expect(link.startsWith("#trip=1.")).toBe(true);
  expect(readTripLink(link)).toEqual(RICH);

  const draft = toDraft(RICH, PLACES);
  if (typeof draft === "string") {
    throw new Error(draft);
  }
  expect(toRequest(draft)).toEqual(RICH);
  // The place list gives a place's country.
  expect(draft.destinations[0].country).toBe("thailand");
  // A known amount keeps its currency, and so does a blank one in another
  // currency. A blank one in the comparison currency follows it again, as
  // does a budget that is not set.
  expect(draft.destinations[0].flightCurrency).toBe("THB");
  expect(draft.destinations[0].nightlyCurrency).toBe("JPY");
  expect(draft.destinations[1].nightlyCurrency).toBe("EUR");
  expect(draft.destinations[1].flightCurrency).toBeNull();
  expect(draft.budgetCurrency).toBe("CHF");
  expect(draft.budget).toBe("");
});

test("amounts stay exactly as typed and unknown stays unknown", () => {
  const amounts = ["0.10", "600.1", "007", "1e3", "12345678901234.567"];
  for (const amount of amounts) {
    const request = withDestination(VERSION_1_REQUEST, 0, { flight_estimate: amount, nightly_stay_estimate: null });
    const read = readTripLink(tripLink(request));
    expect(read).toEqual(request);
    if (typeof read !== "string") {
      expect(read.destinations[0].flight_estimate).toBe(amount);
      expect(read.destinations[0].nightly_stay_estimate).toBeNull();
    }
  }
});

test("fragments that are not trip links are left alone", () => {
  for (const hash of ["", "#", "#results", "#trip", "#Trip=1.e30", "#trips=1.e30"]) {
    expect(readTripLink(hash), hash).toBe("none");
  }
});

test("links that cannot be read exactly are refused", () => {
  const data = VERSION_1_LINK.slice("#trip=1.".length);
  const cases: [string, string][] = [
    ["a later version", `#trip=2.${data}`],
    ["no version", `#trip=${data}`],
    ["no data", "#trip=1."],
    ["padding", `${VERSION_1_LINK}==`],
    ["standard base64 characters", `#trip=1.${data.replace("-", "+")}`],
    ["a character outside base64url", `${VERSION_1_LINK}$`],
    ["a length base64 cannot have", `#trip=1.${data}AAA`],
    ["bytes that are not UTF-8", `#trip=1.${base64Url(new Uint8Array([0x7b, 0xff, 0x7d]))}`],
    ["text that is not JSON", rawLink("{check_in")],
    ["JSON that is not an object", rawLink("[]")],
    ["JSON null", rawLink("null")],
    ["a field name in another case", link({ ...omit(VERSION_1_REQUEST, "travelers"), Travelers: 2 })],
    ["an extra field", link({ ...VERSION_1_REQUEST, note: "x" })],
    ["a missing field", link(omit(VERSION_1_REQUEST, "budget"))],
    ["a __proto__ field", rawLink(JSON.stringify(VERSION_1_REQUEST).replace("{", '{"__proto__":{},'))],
    ["an extra destination field", link(withDestination(VERSION_1_REQUEST, 0, { notes: "x" }))],
    ["a missing destination field", link({ ...VERSION_1_REQUEST, destinations: [omit(VERSION_1_REQUEST.destinations[0], "place"), VERSION_1_REQUEST.destinations[1]] })],
    ["travelers as a string", link({ ...VERSION_1_REQUEST, travelers: "2" })],
    ["no travelers", link({ ...VERSION_1_REQUEST, travelers: 0 })],
    ["too many travelers", link({ ...VERSION_1_REQUEST, travelers: 21 })],
    ["part of a traveler", link({ ...VERSION_1_REQUEST, travelers: 2.5 })],
    ["one destination", link({ ...VERSION_1_REQUEST, destinations: [VERSION_1_REQUEST.destinations[0]] })],
    ["four destinations", link({ ...VERSION_1_REQUEST, destinations: [...RICH.destinations, VERSION_1_REQUEST.destinations[0]] })],
    ["destinations that are not a list", link({ ...VERSION_1_REQUEST, destinations: {} })],
    ["an unsupported currency", link({ ...VERSION_1_REQUEST, currency: "XXX" })],
    ["a lowercase currency", link({ ...VERSION_1_REQUEST, currency: "usd" })],
    ["an unsupported budget currency", link({ ...VERSION_1_REQUEST, budget_currency: "" })],
    ["an unsupported estimate currency", link(withDestination(VERSION_1_REQUEST, 1, { nightly_stay_estimate_currency: null }))],
    ["a day that does not exist", link({ ...VERSION_1_REQUEST, check_in: "2027-02-29" })],
    ["a month that does not exist", link({ ...VERSION_1_REQUEST, check_out: "2027-13-01" })],
    ["year zero", link({ ...VERSION_1_REQUEST, check_in: "0000-01-01" })],
    ["a date without leading zeros", link({ ...VERSION_1_REQUEST, check_in: "2027-3-10" })],
    ["a five-digit year", link({ ...VERSION_1_REQUEST, check_out: "10000-01-01" })],
    ["an empty date", link({ ...VERSION_1_REQUEST, check_in: "" })],
    ["a date as a number", link({ ...VERSION_1_REQUEST, check_in: 20270310 })],
    ["an empty amount", link({ ...VERSION_1_REQUEST, budget: "" })],
    ["an amount with a leading space", link({ ...VERSION_1_REQUEST, budget: " 1000" })],
    ["an amount with a trailing no-break space", link({ ...VERSION_1_REQUEST, budget: "1000\u00a0" })],
    ["a budget with a line break", link({ ...VERSION_1_REQUEST, budget: "1\n000" })],
    ["a flight estimate with a line break", link(withDestination(VERSION_1_REQUEST, 0, { flight_estimate: "600\n.10" }))],
    ["a nightly estimate with a carriage return", link(withDestination(VERSION_1_REQUEST, 0, { nightly_stay_estimate: "125\r.25" }))],
    ["an amount as a number", link(withDestination(VERSION_1_REQUEST, 0, { flight_estimate: 600.1 }))],
    ["an amount with an unpaired surrogate", link(withDestination(VERSION_1_REQUEST, 0, { flight_estimate: "6\ud800" }))],
    ["a label with a line break", link(withDestination(VERSION_1_REQUEST, 0, { label: "Lis\nbon" }))],
    ["a label with an unpaired surrogate", link(withDestination(VERSION_1_REQUEST, 0, { label: "Lisbon \udc00" }))],
    ["a label that is null", link(withDestination(VERSION_1_REQUEST, 0, { label: null }))],
    ["an empty place", link(withDestination(VERSION_1_REQUEST, 1, { place: "" }))],
    ["an empty country", link(withDestination(VERSION_1_REQUEST, 1, { country: "" }))],
    ["a country with a place", link(withDestination(VERSION_1_REQUEST, 0, { country: "portugal" }))],
    ["a place that is a number", link(withDestination(VERSION_1_REQUEST, 0, { place: 1 }))],
  ];
  for (const [name, hash] of cases) {
    expect(readTripLink(hash), name).toBe("unreadable");
  }
});

test("a link holds at most the 16 KiB the API accepts", () => {
  // The label grows the JSON one byte per character, and 2 bytes of JSON
  // become at most 3 characters of base64url.
  const base = new TextEncoder().encode(JSON.stringify(withLabel("")))
    .length;
  const atLimit = withLabel("a".repeat((16 << 10) - base));
  expect(new TextEncoder().encode(JSON.stringify(atLimit)).length).toBe(16 << 10);
  expect(readTripLink(tripLink(atLimit))).toEqual(atLimit);

  const overLimit = withLabel("a".repeat((16 << 10) - base + 1));
  expect(readTripLink(tripLink(overLimit))).toBe("unreadable");
  // A long link is refused before it is decoded, whatever it holds.
  expect(readTripLink(`#trip=1.${"A".repeat(21_847)}`)).toBe("unreadable");
});

test("a place must be on the place list", () => {
  const unknown = withDestination(VERSION_1_REQUEST, 0, { place: "atlantis" });
  expect(readTripLink(tripLink(unknown))).toEqual(unknown);
  expect(toDraft(unknown, PLACES)).toBe("unknown_place");
  expect(toDraft(VERSION_1_REQUEST, "failed")).toBe("no_place_list");
  // Without a place, a link opens even when the place list failed.
  const noPlace = withDestination(VERSION_1_REQUEST, 0, { place: null, country: "portugal" });
  const draft = toDraft(noPlace, "failed");
  expect(typeof draft).toBe("object");
  if (typeof draft !== "string") {
    expect(toRequest(draft)).toEqual(noPlace);
  }
});

test("generated requests round trip through the link and the form", () => {
  const next = random(1);
  for (let i = 0; i < 500; i++) {
    const request = generateRequest(next);
    const link = tripLink(request);
    expect(readTripLink(link), link).toEqual(request);
    const draft = toDraft(request, PLACES);
    if (typeof draft === "string") {
      throw new Error(`${draft}: ${link}`);
    }
    expect(toRequest(draft), link).toEqual(request);
  }
});

test("damaged links never throw and never open as something else", () => {
  const next = random(2);
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_+/=.%$ é";
  const pick = (s: string) => s[Math.floor(next() * s.length)];
  const outcomes = { none: 0, unreadable: 0, read: 0 };
  for (let i = 0; i < 3000; i++) {
    const link = tripLink(generateRequest(next));
    const at = Math.floor(next() * link.length);
    let damaged: string;
    switch (i % 4) {
      case 0:
        damaged = link.slice(0, at);
        break;
      case 1:
        damaged = link.slice(0, at) + pick(alphabet) + link.slice(at + 1);
        break;
      case 2:
        damaged = link.slice(0, at) + pick(alphabet) + link.slice(at);
        break;
      default:
        damaged = `#trip=1.${Array.from({ length: Math.floor(next() * 300) }, () => pick(alphabet)).join("")}`;
    }
    const read = readTripLink(damaged);
    if (read === "none" || read === "unreadable") {
      outcomes[read] += 1;
      continue;
    }
    // A damaged link that still reads is a valid request in its own right,
    // which writes back to a link that reads as the same request.
    outcomes.read += 1;
    expect(readTripLink(tripLink(read)), damaged).toEqual(read);
  }
  // Both checks above ran on real cases.
  expect(outcomes.unreadable).toBeGreaterThan(0);
  expect(outcomes.read).toBeGreaterThan(0);
});

// link writes any value as a version 1 link, valid or not.
function link(value: unknown): string {
  return tripLink(value as ComparisonRequest);
}

function rawLink(json: string): string {
  return `#trip=1.${base64Url(new TextEncoder().encode(json))}`;
}

function base64Url(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64url");
}

function omit<T extends object>(value: T, key: keyof T): object {
  const copy: Partial<T> = { ...value };
  delete copy[key];
  return copy;
}

function withDestination(request: ComparisonRequest, index: number, patch: Record<string, unknown>): ComparisonRequest {
  return {
    ...request,
    destinations: request.destinations.map((d, i) => (i === index ? { ...d, ...patch } : d)) as ComparisonRequest["destinations"],
  };
}

function withLabel(label: string): ComparisonRequest {
  return withDestination(VERSION_1_REQUEST, 0, { label });
}

// random returns a seeded generator of numbers in [0, 1) (mulberry32), so
// every run checks the same cases.
function random(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// Characters for generated text: ASCII, punctuation that JSON escapes, a
// tab, a no-break space, a line separator, NUL, accented letters, CJK, and
// emoji (surrogate pairs). Never a line break, which no text field can
// hold.
const TEXT = [..."aZ09 .,-'\"\\/<>&\t\u00a0éøß日本語🌴🚆\u2028\u0000"];

function generateRequest(next: () => number): ComparisonRequest {
  const pick = <T>(list: T[]) => list[Math.floor(next() * list.length)];
  const text = (max: number) => Array.from({ length: Math.floor(next() * (max + 1)) }, () => pick(TEXT)).join("");
  const amount = () => {
    if (next() < 0.3) {
      return null;
    }
    const value = next() < 0.7 ? `${Math.floor(next() * 1e6)}${next() < 0.5 ? `.${Math.floor(next() * 100)}` : ""}` : text(12).trim();
    return value === "" ? "1" : value;
  };
  const date = () => {
    const year = 1 + Math.floor(next() * 9999);
    const month = 1 + Math.floor(next() * 12);
    const day = 1 + Math.floor(next() * 28);
    return `${String(year).padStart(4, "0")}-${String(month).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
  };
  const count = next() < 0.5 ? 2 : 3;
  return {
    check_in: date(),
    check_out: date(),
    travelers: 1 + Math.floor(next() * 20),
    currency: pick(CURRENCIES),
    budget: amount(),
    budget_currency: pick(CURRENCIES),
    destinations: Array.from({ length: count }, () => {
      const place = next() < 0.4 ? pick(PLACES).id : null;
      return {
        label: text(30),
        place,
        country: place === null && next() < 0.5 ? text(10).trim() || "portugal" : null,
        flight_estimate: amount(),
        flight_estimate_currency: pick(CURRENCIES),
        nightly_stay_estimate: amount(),
        nightly_stay_estimate_currency: pick(CURRENCIES),
      };
    }),
  };
}

// The trip as the comparison form holds it, the request the form sends, and
// trip links: the request a comparison was made from, kept in the page
// address so a trip can be reloaded, bookmarked, or sent.

import type { ComparisonRequest, DestinationInput, Place } from "./api";

// Currency codes the API accepts. The API validates the choice and owns each
// currency's number of decimal places.
export const CURRENCIES = [
  "AUD", "BRL", "CAD", "CHF", "CNY", "CZK", "DKK", "EUR", "GBP", "HKD",
  "HUF", "IDR", "ILS", "INR", "ISK", "JPY", "KRW", "MXN", "MYR", "NOK",
  "NZD", "PHP", "PLN", "RON", "SEK", "SGD", "THB", "TRY", "USD", "ZAR",
];
export const MAX_TRAVELERS = 20;
export const MIN_DESTINATIONS = 2;
export const MAX_DESTINATIONS = 3;

export type DestinationDraft = {
  label: string;
  // A place ID from the place list; "" means not listed. A place sets
  // country to its own country, and the country menu stays locked until
  // the place is cleared.
  place: string;
  // An FCDO country slug for travel advice; "" means none.
  country: string;
  flight: string;
  // An amount's currency is null while it follows the comparison currency.
  // Typing the amount or choosing its currency sets it for good.
  flightCurrency: string | null;
  nightly: string;
  nightlyCurrency: string | null;
};

export type Draft = {
  checkIn: string;
  checkOut: string;
  travelers: number;
  currency: string;
  budget: string;
  // Follows the comparison currency while null, like an estimate's currency.
  budgetCurrency: string | null;
  destinations: DestinationDraft[];
};

export const blankDestination: DestinationDraft = {
  label: "",
  place: "",
  country: "",
  flight: "",
  flightCurrency: null,
  nightly: "",
  nightlyCurrency: null,
};

export const initialDraft: Draft = {
  checkIn: "",
  checkOut: "",
  travelers: 2,
  currency: "USD",
  budget: "",
  budgetCurrency: null,
  destinations: [blankDestination, blankDestination],
};

export function toRequest(draft: Draft): ComparisonRequest {
  return {
    check_in: draft.checkIn,
    check_out: draft.checkOut,
    travelers: draft.travelers,
    currency: draft.currency,
    budget: optionalAmount(draft.budget),
    budget_currency: draft.budgetCurrency ?? draft.currency,
    destinations: draft.destinations.map((d) => ({
      label: d.label,
      // With a place, the API takes the country from the place.
      place: d.place === "" ? null : d.place,
      country: d.place !== "" || d.country === "" ? null : d.country,
      flight_estimate: optionalAmount(d.flight),
      flight_estimate_currency: d.flightCurrency ?? draft.currency,
      nightly_stay_estimate: optionalAmount(d.nightly),
      nightly_stay_estimate_currency: d.nightlyCurrency ?? draft.currency,
    })),
  };
}

// A blank estimate is unknown, and a blank budget is not set. The API
// validates everything else.
function optionalAmount(value: string): string | null {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}

// A trip link is the page address fragment "#trip=", the format version, a
// dot, and the request as UTF-8 JSON in base64url without padding. Version 1
// is the only format so far. A new format gets a new number, and links in
// every earlier format must keep opening, so what version 1 means never
// changes.
const LINK_PREFIX = "#trip=";
const LINK_VERSION = "1";

// The API refuses request bodies over 16 KiB, so no comparison can make a
// longer link, and a longer one is refused before it is decoded.
const MAX_JSON_BYTES = 16 << 10;
const MAX_DATA_LENGTH = Math.ceil((MAX_JSON_BYTES * 4) / 3);

const REQUEST_KEYS = ["check_in", "check_out", "travelers", "currency", "budget", "budget_currency", "destinations"];
const DESTINATION_KEYS = [
  "label",
  "place",
  "country",
  "flight_estimate",
  "flight_estimate_currency",
  "nightly_stay_estimate",
  "nightly_stay_estimate_currency",
];

// tripLink returns the address fragment for a request built by toRequest.
export function tripLink(request: ComparisonRequest): string {
  const bytes = new TextEncoder().encode(JSON.stringify(request));
  return `${LINK_PREFIX}${LINK_VERSION}.${toBase64Url(bytes)}`;
}

// readTripLink reads an address fragment, as location.hash gives it: "none"
// when it is not a trip link, "unreadable" when it is one that cannot be
// read, and otherwise the request. The request must be in the form toRequest
// builds, and every value must fit the form's control for it exactly, so the
// form shows what the next comparison sends: a text field drops line breaks,
// and a date field drops a date that is not a real day. Whether a place is
// on the place list is checked by toDraft. The API validates the rest.
export function readTripLink(hash: string): ComparisonRequest | "none" | "unreadable" {
  if (!hash.startsWith(LINK_PREFIX)) {
    return "none";
  }
  const rest = hash.slice(LINK_PREFIX.length);
  const dot = rest.indexOf(".");
  if (dot < 0 || rest.slice(0, dot) !== LINK_VERSION) {
    return "unreadable";
  }
  const data = rest.slice(dot + 1);
  if (data.length > MAX_DATA_LENGTH) {
    return "unreadable";
  }
  const bytes = fromBase64Url(data);
  if (bytes === null || bytes.length > MAX_JSON_BYTES) {
    return "unreadable";
  }
  let value: unknown;
  try {
    value = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  } catch {
    return "unreadable";
  }
  return isLinkRequest(value) ? value : "unreadable";
}

// toDraft turns a request from a trip link back into the form's draft, the
// inverse of toRequest. A place must be on the place list, which also gives
// the destination's country. An amount's currency follows the comparison
// currency again only when the amount is blank and its currency is the
// comparison currency.
export function toDraft(
  request: ComparisonRequest,
  places: Place[] | "failed",
): Draft | "unknown_place" | "no_place_list" {
  const destinations: DestinationDraft[] = [];
  for (const d of request.destinations) {
    let country = d.country ?? "";
    if (d.place !== null) {
      if (places === "failed") {
        return "no_place_list";
      }
      const place = places.find((p) => p.id === d.place);
      if (place === undefined) {
        return "unknown_place";
      }
      country = place.country;
    }
    destinations.push({
      label: d.label,
      place: d.place ?? "",
      country,
      flight: d.flight_estimate ?? "",
      flightCurrency: ownCurrency(d.flight_estimate, d.flight_estimate_currency, request.currency),
      nightly: d.nightly_stay_estimate ?? "",
      nightlyCurrency: ownCurrency(d.nightly_stay_estimate, d.nightly_stay_estimate_currency, request.currency),
    });
  }
  return {
    checkIn: request.check_in,
    checkOut: request.check_out,
    travelers: request.travelers,
    currency: request.currency,
    budget: request.budget ?? "",
    budgetCurrency: ownCurrency(request.budget, request.budget_currency, request.currency),
    destinations,
  };
}

function ownCurrency(amount: string | null, currency: string, comparison: string): string | null {
  return amount !== null || currency !== comparison ? currency : null;
}

function isLinkRequest(value: unknown): value is ComparisonRequest {
  if (!hasExactKeys(value, REQUEST_KEYS)) {
    return false;
  }
  const r = value as ComparisonRequest;
  return (
    isDate(r.check_in) &&
    isDate(r.check_out) &&
    Number.isInteger(r.travelers) &&
    r.travelers >= 1 &&
    r.travelers <= MAX_TRAVELERS &&
    isCurrency(r.currency) &&
    isAmount(r.budget) &&
    isCurrency(r.budget_currency) &&
    Array.isArray(r.destinations) &&
    r.destinations.length >= MIN_DESTINATIONS &&
    r.destinations.length <= MAX_DESTINATIONS &&
    r.destinations.every(isLinkDestination)
  );
}

function isLinkDestination(value: unknown): value is DestinationInput {
  if (!hasExactKeys(value, DESTINATION_KEYS)) {
    return false;
  }
  const d = value as DestinationInput;
  return (
    isText(d.label) &&
    (d.place === null || isChoice(d.place)) &&
    (d.country === null || (d.place === null && isChoice(d.country))) &&
    isAmount(d.flight_estimate) &&
    isCurrency(d.flight_estimate_currency) &&
    isAmount(d.nightly_stay_estimate) &&
    isCurrency(d.nightly_stay_estimate_currency)
  );
}

function hasExactKeys(value: unknown, keys: string[]): boolean {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  const own = Object.keys(value);
  return own.length === keys.length && keys.every((k) => Object.hasOwn(value, k));
}

// isText accepts what a text field holds unchanged: no line breaks, and no
// unpaired surrogate, which the request's JSON could not carry exactly.
function isText(value: unknown): value is string {
  if (typeof value !== "string" || /[\r\n]/.test(value)) {
    return false;
  }
  try {
    encodeURIComponent(value);
    return true;
  } catch {
    return false;
  }
}

// An amount is unknown (null) or what toRequest sends for a typed one: not
// blank, with no surrounding space.
function isAmount(value: unknown): value is string | null {
  return value === null || (isText(value) && value !== "" && value.trim() === value);
}

// A place or country is a menu value, so it is null when none is chosen,
// never "".
function isChoice(value: unknown): value is string {
  return isText(value) && value !== "";
}

function isCurrency(value: unknown): value is string {
  return typeof value === "string" && CURRENCIES.includes(value);
}

// isDate accepts a real calendar day as YYYY-MM-DD with a year from 1 to
// 9999, what a date field shows and the API reads. It uses calendar
// arithmetic only, so no time zone is involved.
function isDate(value: unknown): value is string {
  const m = typeof value === "string" ? /^(\d{4})-(\d{2})-(\d{2})$/.exec(value) : null;
  if (m === null) {
    return false;
  }
  const [year, month, day] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const leap = (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0;
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return year >= 1 && month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1];
}

function toBase64Url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) {
    binary += String.fromCharCode(b);
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// fromBase64Url decodes unpadded base64url, or returns null.
function fromBase64Url(data: string): Uint8Array | null {
  if (!/^[A-Za-z0-9_-]*$/.test(data)) {
    return null;
  }
  let binary: string;
  try {
    binary = atob(data.replace(/-/g, "+").replace(/_/g, "/"));
  } catch {
    return null;
  }
  return Uint8Array.from(binary, (c) => c.charCodeAt(0));
}

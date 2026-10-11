// Types and request helpers for POST /api/trip-comparisons, GET
// /api/travel-advice/countries and GET /api/places. The API validates every
// field, converts amounts, does all arithmetic, and rounds typical weather.
// Amounts are decimal strings in the currency named beside them, and null
// means an estimate is unknown.

export type DestinationInput = {
  label: string;
  // A place ID from the place list, or null. A place sets the country, so
  // country must then be null or the place's country.
  place: string | null;
  // An FCDO country slug from the travel advice country list, or null.
  country: string | null;
  flight_estimate: string | null;
  flight_estimate_currency: string;
  nightly_stay_estimate: string | null;
  nightly_stay_estimate_currency: string;
};

export type ComparisonRequest = {
  check_in: string;
  check_out: string;
  travelers: number;
  // The comparison currency, which every total uses.
  currency: string;
  // An optional budget for the whole trip and the whole party; null when none.
  budget: string | null;
  budget_currency: string;
  destinations: DestinationInput[];
};

// How a destination compares with the budget, in the comparison currency.
// budget_difference is the amount left (within), the amount over (over), or
// the least amount over for an incomplete destination whose known part alone
// is over the budget (over_at_least).
export type BudgetCheck =
  | { budget_status: "not_set" | "unknown"; budget_difference: null }
  | { budget_status: "within" | "over" | "over_at_least"; budget_difference: string };

export type DestinationEstimate = DestinationInput &
  BudgetCheck & {
    // In the nightly estimate's currency.
    stay_estimate: string | null;
    // In the comparison currency; null when unknown or not converted.
    converted_flight_estimate: string | null;
    converted_stay_estimate: string | null;
    flight_and_stay_estimate: string | null;
    complete: boolean;
    lowest_estimate: boolean;
    // null when the destination has no country.
    travel_advice: TravelAdvice | null;
    // null when the destination has no place, or its place has no typical
    // weather.
    climate: Climate | null;
  };

// Typical weather at a destination's place for each calendar month its stay
// touches, in the order the stay reaches them, averaged over the years from
// period_start to period_end.
export type Climate = {
  place_name: string;
  period_start: number;
  period_end: number;
  months: ClimateMonth[];
};

// One month's typical weather: the average daily high and low, and the
// average precipitation (rain and snow, as liquid water) for the whole month.
export type ClimateMonth = {
  // 1 for January to 12 for December.
  month: number;
  high_c: number;
  high_f: number;
  low_c: number;
  low_f: number;
  precipitation_mm: number;
  // Inches with one decimal place, such as "3.5".
  precipitation_in: string;
};

// FCDO's warnings against travel, as GOV.UK names them. The API lists a
// country's alerts most severe first.
export type Alert =
  | "avoid_all_travel_to_whole_country"
  | "avoid_all_travel_to_parts"
  | "avoid_all_but_essential_travel_to_whole_country"
  | "avoid_all_but_essential_travel_to_parts";

// FCDO travel advice for a destination's country. Times are RFC 3339 in UTC.
// checked_at is when the API received the advice from GOV.UK, or null when
// it came from a saved copy.
export type TravelAdvice = { country_name: string; url: string } & (
  | { available: true; alerts: Alert[]; updated_at: string; checked_at: string | null }
  | { available: false; alerts: null; updated_at: null; checked_at: null }
);

export type Country = {
  slug: string;
  name: string;
};

// A supported place. Choosing one sets the destination's country for travel
// advice (country, named country_name) and shows its typical weather.
export type Place = {
  id: string;
  name: string;
  country: string;
  country_name: string;
};

// The ECB reference rates behind a comparison's conversions.
export type ExchangeRates = {
  available: boolean;
  date: string | null;
  // Published rates per euro, such as { USD: "1.1206" }.
  per_euro: Record<string, string>;
};

export type Comparison = {
  check_in: string;
  check_out: string;
  nights: number;
  travelers: number;
  currency: string;
  // The budget as entered, or null when none is set.
  budget: string | null;
  budget_currency: string;
  // The budget in the comparison currency; null when none is set or it could
  // not be converted.
  converted_budget: string | null;
  all_complete: boolean;
  // null when nothing needed converting.
  exchange_rates: ExchangeRates | null;
  destinations: DestinationEstimate[];
};

export type FieldError = {
  field: string;
  message: string;
};

export type ComparisonOutcome =
  | { kind: "ok"; comparison: Comparison }
  | { kind: "invalid"; message: string; fieldErrors: FieldError[] }
  | { kind: "failed"; message: string };

const unavailable = "The comparison service is unavailable right now. Try again in a moment.";
const rejected = "The comparison request could not be processed. Reload the page and try again.";

export async function requestComparison(body: ComparisonRequest): Promise<ComparisonOutcome> {
  let response: Response;
  try {
    response = await fetch("/api/trip-comparisons", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return { kind: "failed", message: unavailable };
  }

  const data: unknown = await response.json().catch(() => null);
  if (response.ok && isComparison(data)) {
    return { kind: "ok", comparison: data };
  }
  if (response.status === 422 && isFieldErrorBody(data)) {
    return { kind: "invalid", message: data.message, fieldErrors: data.field_errors };
  }
  if (response.status === 400 || response.status === 413) {
    return { kind: "failed", message: rejected };
  }
  return { kind: "failed", message: unavailable };
}

// requestCountries returns the countries with travel advice, or null when
// they cannot be loaded or signal aborts the request.
export async function requestCountries(signal: AbortSignal): Promise<Country[] | null> {
  try {
    const response = await fetch("/api/travel-advice/countries", { signal });
    const data: unknown = await response.json();
    return response.ok && isCountryList(data) ? data.countries : null;
  } catch {
    return null;
  }
}

function isCountryList(data: unknown): data is { countries: Country[] } {
  if (typeof data !== "object" || data === null) {
    return false;
  }
  const countries = (data as { countries?: unknown }).countries;
  return (
    Array.isArray(countries) &&
    countries.every(
      (c: unknown) =>
        typeof c === "object" &&
        c !== null &&
        typeof (c as Country).slug === "string" &&
        typeof (c as Country).name === "string",
    )
  );
}

// requestPlaces returns the supported places in name order, or null when
// they cannot be loaded or signal aborts the request.
export async function requestPlaces(signal: AbortSignal): Promise<Place[] | null> {
  try {
    const response = await fetch("/api/places", { signal });
    const data: unknown = await response.json();
    return response.ok && isPlaceList(data) ? data.places : null;
  } catch {
    return null;
  }
}

function isPlaceList(data: unknown): data is { places: Place[] } {
  if (typeof data !== "object" || data === null) {
    return false;
  }
  const places = (data as { places?: unknown }).places;
  return (
    Array.isArray(places) &&
    places.every(
      (p: unknown) =>
        typeof p === "object" &&
        p !== null &&
        typeof (p as Place).id === "string" &&
        typeof (p as Place).name === "string" &&
        typeof (p as Place).country === "string" &&
        typeof (p as Place).country_name === "string",
    )
  );
}

function isComparison(data: unknown): data is Comparison {
  return typeof data === "object" && data !== null && Array.isArray((data as Comparison).destinations);
}

function isFieldErrorBody(data: unknown): data is { message: string; field_errors: FieldError[] } {
  if (typeof data !== "object" || data === null) {
    return false;
  }
  const body = data as { message?: unknown; field_errors?: unknown };
  return typeof body.message === "string" && Array.isArray(body.field_errors);
}

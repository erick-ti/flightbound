"use client";

import { useEffect, useMemo, useRef, useState, type ReactNode, type SubmitEvent } from "react";
import {
  requestComparison,
  requestCountries,
  requestPlaces,
  type Alert,
  type Climate,
  type ClimateMonth,
  type Comparison,
  type ComparisonRequest,
  type Country,
  type DestinationEstimate,
  type ExchangeRates,
  type TravelAdvice,
} from "./api";
import ResultsMap, { type Pin } from "./ResultsMap";
import {
  CURRENCIES,
  MAX_DESTINATIONS,
  MAX_TRAVELERS,
  MIN_DESTINATIONS,
  blankDestination,
  initialDraft,
  readTripLink,
  toDraft,
  toRequest,
  tripLink,
  type DestinationDraft,
  type Draft,
} from "./trip";
import styles from "./TripComparison.module.css";

const UNREADABLE_LINK = "This trip link could not be opened. It may be incomplete or damaged.";
const NO_PLACE_LIST =
  "The place list could not be loaded, so this trip link cannot be opened. Reload the page to try again.";
const ECB_RATES_URL =
  "https://www.ecb.europa.eu/stats/policy_and_exchange_rates/euro_reference_exchange_rates/html/index.en.html";
const OGL_URL = "https://www.nationalarchives.gov.uk/doc/open-government-licence/version/3/";
const ERA5_LAND_DOI_URL = "https://doi.org/10.24381/ee82e357";
const CC_BY_URL = "https://creativecommons.org/licenses/by/4.0/";
const MONTHS = [
  "January", "February", "March", "April", "May", "June",
  "July", "August", "September", "October", "November", "December",
];

type Outcome =
  | { kind: "none" }
  // id is new for every comparison, so the results start over, with nothing
  // selected on the map.
  | { kind: "result"; comparison: Comparison; submitted: Draft; id: number }
  | { kind: "invalid"; message: string; fieldErrors: Record<string, string> }
  | { kind: "failed"; message: string };

export default function TripComparison() {
  const [draft, setDraft] = useState(initialDraft);
  const [pending, setPending] = useState(false);
  const [outcome, setOutcome] = useState<Outcome>({ kind: "none" });
  // Each comparison gets the next number, which is also its result's id.
  // Only the latest may show its answer or change the address. A trip link
  // takes a number too, so a comparison still out when it arrives is
  // dropped.
  const latest = useRef(0);
  // A trip link read from the address, waiting for the place list to open.
  const [linkToOpen, setLinkToOpen] = useState<{ request: ComparisonRequest } | null>(null);
  const linkOpened = useRef<{ request: ComparisonRequest } | null>(null);
  // The two lists load separately, so either can fail on its own.
  const countries = useList(requestCountries);
  const places = useList(requestPlaces);

  // The page reads the trip link in the address when it loads and whenever
  // the fragment changes, and the link replaces whatever the page was doing
  // from that moment, even while the place list is still loading. A link
  // that cannot be read changes nothing in the form. Leaving the page drops
  // any comparison still out, so its answer cannot write another page's
  // address.
  useEffect(() => {
    const read = () => {
      const request = readTripLink(window.location.hash);
      if (request === "none") {
        return;
      }
      latest.current += 1;
      if (request === "unreadable") {
        setLinkToOpen(null);
        setPending(false);
        setOutcome({ kind: "failed", message: UNREADABLE_LINK });
        return;
      }
      setLinkToOpen({ request });
      setPending(true);
      setOutcome({ kind: "none" });
    };
    read();
    window.addEventListener("hashchange", read);
    return () => {
      window.removeEventListener("hashchange", read);
      latest.current += 1;
    };
  }, []);

  // A link's places need the place list, so it opens once the list has
  // loaded or failed. The ref keeps an effect run again for the same link,
  // as Fast Refresh does, from opening it twice.
  useEffect(() => {
    if (linkToOpen === null || places === "loading" || linkOpened.current === linkToOpen) {
      return;
    }
    linkOpened.current = linkToOpen;
    const restored = toDraft(linkToOpen.request, places);
    if (typeof restored === "string") {
      setPending(false);
      setOutcome({ kind: "failed", message: restored === "no_place_list" ? NO_PLACE_LIST : UNREADABLE_LINK });
      return;
    }
    setDraft(restored);
    void runComparison(restored);
  }, [linkToOpen, places]);

  const errors: Record<string, string> = outcome.kind === "invalid" ? outcome.fieldErrors : {};
  const stale = outcome.kind === "result" && !sameDraft(outcome.submitted, draft);
  const placeHint =
    places === "failed"
      ? "The place list could not be loaded, so the map and typical weather are off. Reload the page to try again."
      : "Optional. Puts the destination on the map, shows typical weather for your dates, and sets the country for travel advice.";

  function countryHint(destination: DestinationDraft) {
    if (destination.place !== "") {
      return "Set by the place. To choose another country, set the place to Not listed.";
    }
    if (countries === "failed") {
      return Array.isArray(places)
        ? "The country list could not be loaded. Travel advice still works for places from the list. Reload the page to try again."
        : "The country list could not be loaded, so travel advice is off. Reload the page to try again.";
    }
    return "Optional. Shows the UK government's FCDO travel advice for this country.";
  }

  // countryOptions is the country list, plus the destination's own country
  // when the list does not have it: a country a place set stays visible
  // while the list is loading or after it failed, named as the place list
  // names it.
  function countryOptions(destination: DestinationDraft): Country[] {
    const list = Array.isArray(countries) ? countries : [];
    const own = destination.country;
    if (own === "" || list.some((c) => c.slug === own)) {
      return list;
    }
    const name = Array.isArray(places) ? places.find((p) => p.country === own)?.country_name : undefined;
    return [...list, { slug: own, name: name ?? own }];
  }

  function update(patch: Partial<Draft>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  // change receives the destination and the comparison currency as they are
  // when the update applies.
  function updateDestination(
    index: number,
    change: (destination: DestinationDraft, currency: string) => Partial<DestinationDraft>,
  ) {
    setDraft((current) => ({
      ...current,
      destinations: current.destinations.map((d, i) => (i === index ? { ...d, ...change(d, current.currency) } : d)),
    }));
  }

  function setDestinations(destinations: DestinationDraft[]) {
    update({ destinations });
    // Field errors name destination positions, which just changed.
    if (outcome.kind === "invalid") {
      setOutcome({ kind: "none" });
    }
  }

  async function handleSubmit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    await runComparison(draft);
  }

  async function runComparison(submitted: Draft) {
    const id = ++latest.current;
    setPending(true);
    const request = toRequest(submitted);
    const result = await requestComparison(request);
    if (id !== latest.current) {
      return;
    }
    setPending(false);
    if (result.kind === "ok") {
      setOutcome({ kind: "result", comparison: result.comparison, submitted, id });
      // The address now holds the trip these results came from. Next.js
      // keeps its router in step with a replaceState given null state.
      window.history.replaceState(null, "", tripLink(request));
    } else if (result.kind === "invalid") {
      const fieldErrors = Object.fromEntries(result.fieldErrors.map((e) => [e.field, e.message]));
      setOutcome({ kind: "invalid", message: result.message, fieldErrors });
    } else {
      setOutcome({ kind: "failed", message: result.message });
    }
  }

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1>Compare trips</h1>
        <p>
          Compare flight and stay estimates for two or three destinations on the same dates. Enter your
          own estimates for the whole party, each in the currency you have it in, and leave one blank if
          you do not know it yet.
        </p>
      </header>

      <form className={styles.form} onSubmit={handleSubmit} noValidate>
        <fieldset className={styles.bare} disabled={pending}>
          <fieldset className={styles.group}>
            <legend>Trip</legend>
            <div className={styles.grid}>
              <Field path="check_in" label="Check-in date" error={errors.check_in}>
                <input
                  {...controlProps("check_in", errors.check_in)}
                  type="date"
                  value={draft.checkIn}
                  onChange={(e) => update({ checkIn: e.target.value })}
                />
              </Field>
              <Field path="check_out" label="Check-out date" error={errors.check_out}>
                <input
                  {...controlProps("check_out", errors.check_out)}
                  type="date"
                  value={draft.checkOut}
                  onChange={(e) => update({ checkOut: e.target.value })}
                />
              </Field>
              <Field path="travelers" label="Travelers" error={errors.travelers}>
                <select
                  {...controlProps("travelers", errors.travelers)}
                  value={draft.travelers}
                  onChange={(e) => update({ travelers: Number(e.target.value) })}
                >
                  {Array.from({ length: MAX_TRAVELERS }, (_, i) => i + 1).map((n) => (
                    <option key={n} value={n}>
                      {plural(n, "traveler")}
                    </option>
                  ))}
                </select>
              </Field>
              <Field
                path="currency"
                label="Currency for totals"
                hint="Totals use this currency. Amounts in other currencies are converted at ECB reference rates."
                error={errors.currency}
              >
                <select
                  {...controlProps("currency", errors.currency, true)}
                  value={draft.currency}
                  onChange={(e) => update({ currency: e.target.value })}
                >
                  <CurrencyOptions />
                </select>
              </Field>
              <AmountField
                path="budget"
                label="Trip budget for the whole party"
                hint="Optional. Each destination shows what its flights and stay leave for everything else."
                currencyLabel="Currency of the budget"
                errors={errors}
                value={draft.budget}
                currency={draft.budgetCurrency ?? draft.currency}
                onValue={(budget) =>
                  setDraft((current) => ({ ...current, budget, budgetCurrency: current.budgetCurrency ?? current.currency }))
                }
                onCurrency={(budgetCurrency) => update({ budgetCurrency })}
              />
            </div>
          </fieldset>

          {draft.destinations.map((destination, i) => {
            const prefix = `destinations[${i}].`;
            const labelPath = `${prefix}label`;
            const placePath = `${prefix}place`;
            const countryPath = `${prefix}country`;
            return (
              <fieldset key={i} className={styles.group}>
                <legend>Destination {i + 1}</legend>
                <div className={`${styles.grid} ${styles.aligned}`}>
                  <Field path={labelPath} label="Name" error={errors[labelPath]}>
                    <input
                      {...controlProps(labelPath, errors[labelPath])}
                      type="text"
                      autoComplete="off"
                      value={destination.label}
                      onChange={(e) => {
                        const label = e.target.value;
                        updateDestination(i, () => ({ label }));
                      }}
                    />
                  </Field>
                  <Field path={placePath} label="Place" hint={placeHint} error={errors[placePath]}>
                    <select
                      {...controlProps(placePath, errors[placePath], true)}
                      value={destination.place}
                      disabled={places === "loading"}
                      onChange={(e) => {
                        const place = e.target.value;
                        const chosen = Array.isArray(places) ? places.find((p) => p.id === place) : undefined;
                        // Clearing the place keeps the country it set.
                        updateDestination(i, () => (chosen ? { place, country: chosen.country } : { place }));
                      }}
                    >
                      <option value="">Not listed</option>
                      {Array.isArray(places) &&
                        places.map((p) => (
                          <option key={p.id} value={p.id}>
                            {p.name}
                          </option>
                        ))}
                    </select>
                  </Field>
                  <Field
                    path={countryPath}
                    label="Country for travel advice"
                    hint={countryHint(destination)}
                    error={errors[countryPath]}
                  >
                    <select
                      {...controlProps(countryPath, errors[countryPath], true)}
                      value={destination.country}
                      disabled={countries === "loading" || destination.place !== ""}
                      onChange={(e) => {
                        const country = e.target.value;
                        updateDestination(i, () => ({ country }));
                      }}
                    >
                      <option value="">None</option>
                      {countryOptions(destination).map((c) => (
                        <option key={c.slug} value={c.slug}>
                          {c.name}
                        </option>
                      ))}
                    </select>
                  </Field>
                </div>
                <div className={`${styles.grid} ${styles.aligned}`}>
                  <AmountField
                    path={`${prefix}flight_estimate`}
                    label="Round-trip flights for the whole party"
                    currencyLabel="Currency of the flight estimate"
                    errors={errors}
                    value={destination.flight}
                    currency={destination.flightCurrency ?? draft.currency}
                    onValue={(flight) =>
                      updateDestination(i, (d, currency) => ({ flight, flightCurrency: d.flightCurrency ?? currency }))
                    }
                    onCurrency={(flightCurrency) => updateDestination(i, () => ({ flightCurrency }))}
                  />
                  <AmountField
                    path={`${prefix}nightly_stay_estimate`}
                    label="Accommodation per night for the whole party"
                    currencyLabel="Currency of the accommodation estimate"
                    errors={errors}
                    value={destination.nightly}
                    currency={destination.nightlyCurrency ?? draft.currency}
                    onValue={(nightly) =>
                      updateDestination(i, (d, currency) => ({ nightly, nightlyCurrency: d.nightlyCurrency ?? currency }))
                    }
                    onCurrency={(nightlyCurrency) => updateDestination(i, () => ({ nightlyCurrency }))}
                  />
                </div>
                {draft.destinations.length > MIN_DESTINATIONS && (
                  <button
                    type="button"
                    className={`${styles.button} ${styles.secondary}`}
                    onClick={() => setDestinations(draft.destinations.filter((_, j) => j !== i))}
                  >
                    Remove destination {i + 1}
                  </button>
                )}
              </fieldset>
            );
          })}
          {errors.destinations && <p className={styles.error}>{errors.destinations}</p>}

          <div className={styles.actions}>
            {draft.destinations.length < MAX_DESTINATIONS && (
              <button
                type="button"
                className={`${styles.button} ${styles.secondary}`}
                onClick={() => setDestinations([...draft.destinations, blankDestination])}
              >
                Add a third destination
              </button>
            )}
            <button type="submit" className={`${styles.button} ${styles.primary}`}>
              {pending ? "Comparing…" : "Compare estimates"}
            </button>
          </div>
        </fieldset>
      </form>

      {(outcome.kind === "invalid" || outcome.kind === "failed") && (
        <p role="alert" className={styles.alert}>
          {outcome.message}
        </p>
      )}
      {outcome.kind === "result" && <Results key={outcome.id} comparison={outcome.comparison} stale={stale} />}
    </div>
  );
}

// AmountField is an optional amount with its own currency menu. path is the
// amount's API field; its currency's field is path + "_currency".
function AmountField({
  path,
  label,
  hint = "Optional. Leave blank if unknown.",
  currencyLabel,
  errors,
  value,
  currency,
  onValue,
  onCurrency,
}: {
  path: string;
  label: string;
  hint?: string;
  currencyLabel: string;
  errors: Record<string, string>;
  value: string;
  currency: string;
  onValue: (value: string) => void;
  onCurrency: (currency: string) => void;
}) {
  const amountError = errors[path];
  const currencyPath = `${path}_currency`;
  const currencyError = errors[currencyPath];
  return (
    <Field path={path} label={label} hint={hint} error={amountError ?? currencyError}>
      <div className={styles.amount}>
        <input
          {...controlProps(path, amountError, true)}
          type="text"
          autoComplete="off"
          value={value}
          onChange={(e) => onValue(e.target.value)}
        />
        <select
          id={domId(currencyPath)}
          aria-label={currencyLabel}
          aria-invalid={currencyError ? true : undefined}
          aria-describedby={currencyError ? `${domId(path)}-error` : undefined}
          value={currency}
          onChange={(e) => onCurrency(e.target.value)}
        >
          <CurrencyOptions />
        </select>
      </div>
    </Field>
  );
}

function CurrencyOptions() {
  return CURRENCIES.map((code) => (
    <option key={code} value={code}>
      {code}
    </option>
  ));
}

function Results({ comparison, stale }: { comparison: Comparison; stale: boolean }) {
  const { currency, nights, travelers, exchange_rates: rates } = comparison;
  const jointLowest = comparison.destinations.filter((d) => d.lowest_estimate).length > 1;
  const climate = comparison.destinations.find((d) => d.climate !== null)?.climate;
  const pins = useMemo(() => mapPins(comparison.destinations), [comparison]);
  // The place selected on the map and on its cards, if any.
  const [selected, setSelected] = useState<string | null>(null);
  // Once the map has failed, the cards drop their numbers and map controls.
  const [mapFailed, setMapFailed] = useState(false);
  const hasMap = pins.length > 0 && !mapFailed;
  const dropMap = () => {
    setMapFailed(true);
    setSelected(null);
  };
  const toggle = (place: string) => setSelected((current) => (current === place ? null : place));
  // A pin that becomes selected brings its first card into view.
  const selectFromPin = (place: string) => {
    if (selected !== place) {
      const i = comparison.destinations.findIndex((d) => d.place === place);
      document.getElementById(cardId(i))?.scrollIntoView({
        block: "nearest",
        behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth",
      });
    }
    toggle(place);
  };
  return (
    <section className={styles.results} aria-labelledby="results-heading">
      <h2 id="results-heading">Results</h2>
      <p className={styles.summary} role="status">
        {plural(nights, "night")} · {plural(travelers, "traveler")} · totals in {currency} · amounts are for the
        whole party
      </p>
      {comparison.budget !== null && <BudgetSummary comparison={comparison} />}
      {stale && (
        <p className={styles.notice} role="status">
          You changed the trip after this comparison. Select Compare estimates to update it.
        </p>
      )}
      {pins.length > 0 && (
        <div className={stale ? styles.stale : undefined}>
          <ResultsMap pins={pins} selected={selected} onPinClick={selectFromPin} onFail={dropMap} />
        </div>
      )}
      <div className={stale ? `${styles.cards} ${styles.stale}` : styles.cards}>
        {comparison.destinations.map((destination, i) => (
          <DestinationCard
            key={i}
            id={cardId(i)}
            number={hasMap ? i + 1 : null}
            map={cardMap(destination, hasMap, selected, toggle)}
            destination={destination}
            currency={currency}
            nights={nights}
            jointLowest={jointLowest}
            ratesAvailable={rates?.available ?? true}
            budgetConverted={comparison.converted_budget !== null}
          />
        ))}
      </div>
      {!comparison.all_complete && (
        <p className={styles.note}>No lowest estimate is marked until every destination has a total.</p>
      )}
      {rates && <RatesNote rates={rates} />}
      {comparison.destinations.some((d) => d.travel_advice !== null) && (
        <p className={styles.note}>
          Travel advice comes from the UK Foreign, Commonwealth &amp; Development Office (FCDO) on GOV.UK and is
          written for British travellers. Contains public sector information licensed under the{" "}
          <a href={OGL_URL}>Open Government Licence v3.0</a>.
        </p>
      )}
      {climate && <ClimateSource climate={climate} />}
      <p className={styles.note}>
        These are your own estimates, not live prices or supplier offers. They cover flights and
        accommodation only, not a full trip budget.
      </p>
      <p className={styles.note}>
        This page&apos;s address now holds this trip, so you can bookmark it or send it. Anyone with the link
        can see these estimates{comparison.budget !== null && " and your budget"}.
      </p>
    </section>
  );
}

function cardId(i: number) {
  return `result-${i + 1}`;
}

// CardMap is a card's part in the map: null when the result has no map,
// "off" for a destination without a place, or its selection otherwise.
type CardMap = null | "off" | { selected: boolean; onToggle: () => void };

function cardMap(
  d: DestinationEstimate,
  hasMap: boolean,
  selected: string | null,
  toggle: (place: string) => void,
): CardMap {
  if (!hasMap) {
    return null;
  }
  const place = d.location === null ? null : d.place;
  if (place === null) {
    return "off";
  }
  return { selected: place === selected, onToggle: () => toggle(place) };
}

// mapPins groups the destinations that have a place into one pin per place,
// in the order the result lists them.
function mapPins(destinations: DestinationEstimate[]): Pin[] {
  const pins: Pin[] = [];
  destinations.forEach((d, i) => {
    if (d.place === null || d.location === null) {
      return;
    }
    let pin = pins.find((p) => p.place === d.place);
    if (pin === undefined) {
      pin = { place: d.place, latitude: d.location.latitude, longitude: d.location.longitude, numbers: [], labels: [] };
      pins.push(pin);
    }
    pin.numbers.push(i + 1);
    pin.labels.push(d.label);
  });
  return pins;
}

// BudgetSummary shows the budget as entered and in the comparison currency,
// and says why no destination is checked when it could not be converted.
function BudgetSummary({ comparison: c }: { comparison: Comparison }) {
  const ratesAvailable = c.exchange_rates?.available ?? true;
  return (
    <>
      <p className={styles.summary}>
        Trip budget: {c.budget_currency} {c.budget}
        <Converted from={c.budget_currency} to={c.currency} known value={c.converted_budget} />
      </p>
      {c.converted_budget === null && (
        <p className={styles.notice}>
          {ratesAvailable
            ? `There is no ECB reference rate to convert your budget to ${c.currency}, so no destination is checked against it.`
            : `Exchange rates are unavailable right now, so your budget could not be converted to ${c.currency} and no destination is checked against it.`}
        </p>
      )}
    </>
  );
}

function RatesNote({ rates }: { rates: ExchangeRates }) {
  if (!rates.available) {
    return (
      <p className={styles.note}>
        Exchange rates could not be loaded, so amounts in other currencies were not converted. Try again
        later.
      </p>
    );
  }
  const used = Object.entries(rates.per_euro)
    .map(([code, rate]) => `1 EUR = ${rate} ${code}`)
    .join(", ");
  return (
    <p className={styles.note}>
      {used && (
        <>
          Amounts in other currencies were converted with the European Central Bank&apos;s euro reference rates
          for {rates.date}: {used}.{" "}
        </>
      )}
      Reference rates are for information only; your bank or card rate will differ.{" "}
      <a href={ECB_RATES_URL}>Source: ECB statistics</a>.
    </p>
  );
}

function DestinationCard({
  id,
  number,
  map,
  destination: d,
  currency,
  nights,
  jointLowest,
  ratesAvailable,
  budgetConverted,
}: {
  id: string;
  // The destination's number on the map, or null when there is no map.
  number: number | null;
  map: CardMap;
  destination: DestinationEstimate;
  currency: string;
  nights: number;
  jointLowest: boolean;
  ratesAvailable: boolean;
  budgetConverted: boolean;
}) {
  const nightly =
    d.nightly_stay_estimate === null ? "unknown" : `${d.nightly_stay_estimate_currency} ${d.nightly_stay_estimate}`;
  const missing = d.flight_estimate === null || d.nightly_stay_estimate === null;
  const notConverted =
    (d.flight_estimate !== null && d.converted_flight_estimate === null) ||
    (d.stay_estimate !== null && d.converted_stay_estimate === null);
  let incomplete = "";
  if (missing) {
    incomplete = "Add the missing estimate to see a total.";
  } else if (notConverted) {
    incomplete = ratesAvailable
      ? "There is no ECB reference rate for one of these currencies, so this total cannot be calculated."
      : "Exchange rates are unavailable right now, so this total cannot be calculated.";
  }
  return (
    <article id={id} className={map !== null && map !== "off" && map.selected ? `${styles.card} ${styles.selectedCard}` : styles.card}>
      <h3>
        {number !== null && <span className={styles.number}>{number}</span>}
        {number !== null && " "}
        {d.label}
      </h3>
      {d.lowest_estimate && <p className={styles.badge}>{jointLowest ? "Joint lowest estimate" : "Lowest estimate"}</p>}
      {map === "off" && <p className={styles.offMap}>Not on the map: no place chosen.</p>}
      {map !== null && map !== "off" && (
        <p className={styles.mapLine}>
          <button
            type="button"
            className={`${styles.button} ${styles.secondary} ${styles.mapButton}`}
            aria-pressed={map.selected}
            onClick={map.onToggle}
          >
            Show on map
          </button>
        </p>
      )}
      <dl className={styles.amounts}>
        <div>
          <dt>Round-trip flights</dt>
          <dd>
            {amount(d.flight_estimate_currency, d.flight_estimate)}
            <Converted
              from={d.flight_estimate_currency}
              to={currency}
              known={d.flight_estimate !== null}
              value={d.converted_flight_estimate}
            />
          </dd>
        </div>
        <div>
          <dt>
            Stay: {plural(nights, "night")} × {nightly}
          </dt>
          <dd>
            {amount(d.nightly_stay_estimate_currency, d.stay_estimate)}
            <Converted
              from={d.nightly_stay_estimate_currency}
              to={currency}
              known={d.stay_estimate !== null}
              value={d.converted_stay_estimate}
            />
          </dd>
        </div>
        <div className={styles.total}>
          <dt>Flight + stay estimate</dt>
          <dd>{amount(currency, d.flight_and_stay_estimate)}</dd>
        </div>
      </dl>
      <BudgetLine destination={d} currency={currency} budgetConverted={budgetConverted} />
      {!d.complete && incomplete && <p className={styles.missing}>{incomplete}</p>}
      {d.climate && <ClimateNote climate={d.climate} />}
      {d.travel_advice && <AdviceNote advice={d.travel_advice} />}
    </article>
  );
}

// ClimateNote shows the typical weather at a destination's place, one
// sentence for each month of the stay, with the values the API rounded.
function ClimateNote({ climate: c }: { climate: Climate }) {
  return (
    <div className={styles.climate}>
      {c.months.map((m) => (
        <p key={m.month}>{climateText(c.place_name, m)}</p>
      ))}
    </div>
  );
}

function climateText(place: string, m: ClimateMonth) {
  return (
    `Typical weather in ${place} in ${MONTHS[m.month - 1]}: highs ${degrees(m.high_c, m.high_f)}, ` +
    `lows ${degrees(m.low_c, m.low_f)}, ${precipitation(m)}.`
  );
}

// A non-breaking space keeps each number on the same line as its unit.
function degrees(c: number, f: number) {
  return `${c}\u00a0°C (${f}\u00a0°F)`;
}

function precipitation(m: ClimateMonth) {
  if (m.precipitation_mm === 0) {
    return "less than 1\u00a0mm of precipitation";
  }
  const inches = m.precipitation_in === "0.0" ? "less than 0.1\u00a0in" : `${m.precipitation_in}\u00a0in`;
  return `about ${m.precipitation_mm}\u00a0mm (${inches}) of precipitation`;
}

// ClimateSource says what typical weather is and credits its source, as the
// dataset's CC BY 4.0 licence requires.
function ClimateSource({ climate: c }: { climate: Climate }) {
  return (
    <>
      <p className={styles.note}>
        Typical weather is the {c.period_start}-{c.period_end} average for a land area about 9&nbsp;km wide near
        each place, from ERA5-Land hourly data. Precipitation includes rain and snow, with snow counted as melted
        water. It is not a forecast, and local weather can differ, especially on coasts and in mountains.
      </p>
      <p className={styles.note}>
        The results contain modified Copernicus Climate Change Service information 2026. Neither the European
        Commission nor ECMWF is responsible for any use that may be made of the Copernicus information or data it
        contains. Source:{" "}
        <a href={ERA5_LAND_DOI_URL}>ERA5 Land hourly time-series data from 1950 to present</a>, licensed
        under <a href={CC_BY_URL}>CC BY 4.0</a>.
      </p>
    </>
  );
}

// AdviceNote shows FCDO's travel advice for a destination's country, with one
// sentence for each alert the API returned.
function AdviceNote({ advice: a }: { advice: TravelAdvice }) {
  if (!a.available) {
    return (
      <div className={styles.advice}>
        <p>
          Travel advice unavailable. <a href={a.url}>Check the latest advice on GOV.UK</a>.
        </p>
      </div>
    );
  }
  const parts = a.alerts.some(
    (alert) => alert === "avoid_all_travel_to_parts" || alert === "avoid_all_but_essential_travel_to_parts",
  );
  return (
    <div className={styles.advice}>
      {a.alerts.length === 0 ? (
        <p>FCDO does not advise against travel to any part of {a.country_name}.</p>
      ) : (
        a.alerts.map((alert) => (
          <p key={alert} className={styles.warning}>
            {alertText(alert, a.country_name)}
          </p>
        ))
      )}
      {parts && <p>Check whether the places you plan to visit are in an affected area.</p>}
      <p className={styles.adviceDates}>
        Updated {formatUpdated(a.updated_at)}.{a.checked_at !== null && ` Checked ${formatChecked(a.checked_at)}.`}
      </p>
      <p>
        <a href={a.url}>Read the full advice on GOV.UK</a>
      </p>
    </div>
  );
}

function alertText(alert: Alert, country: string): string {
  switch (alert) {
    case "avoid_all_travel_to_whole_country":
      return `FCDO advises against all travel to ${country}.`;
    case "avoid_all_travel_to_parts":
      return `FCDO advises against all travel to parts of ${country}.`;
    case "avoid_all_but_essential_travel_to_whole_country":
      return `FCDO advises against all but essential travel to ${country}.`;
    case "avoid_all_but_essential_travel_to_parts":
      return `FCDO advises against all but essential travel to parts of ${country}.`;
  }
}

// GOV.UK dates its advice in UK time, so the update date uses UK time too.
function formatUpdated(time: string) {
  return new Intl.DateTimeFormat("en-GB", { dateStyle: "long", timeZone: "Europe/London" }).format(new Date(time));
}

// The check time is shown in the viewer's own time zone.
function formatChecked(time: string) {
  return new Intl.DateTimeFormat("en-GB", { dateStyle: "long", timeStyle: "short" }).format(new Date(time));
}

// BudgetLine says how a destination compares with the budget, with the
// difference the API returned. When the budget itself was not converted,
// BudgetSummary explains why nothing is checked.
function BudgetLine({
  destination: d,
  currency,
  budgetConverted,
}: {
  destination: DestinationEstimate;
  currency: string;
  budgetConverted: boolean;
}) {
  switch (d.budget_status) {
    case "within":
      return (
        <p className={styles.budget}>
          Leaves {currency} {d.budget_difference} of your budget for everything else.
        </p>
      );
    case "over":
      return (
        <p className={`${styles.budget} ${styles.over}`}>
          {currency} {d.budget_difference} over your budget on flights and stay alone.
        </p>
      );
    case "over_at_least": {
      // An incomplete destination has at most one converted part, and that is
      // the part the API checked.
      const part = d.converted_flight_estimate !== null ? "flights" : "the stay";
      return (
        <p className={`${styles.budget} ${styles.over}`}>
          At least {currency} {d.budget_difference} over your budget on {part} alone.
        </p>
      );
    }
    case "unknown":
      return budgetConverted ? (
        <p className={styles.budget}>Your budget can be checked once this destination has a total.</p>
      ) : null;
    case "not_set":
      return null;
  }
}

// Converted shows a known amount in the comparison currency, below the
// amount as entered, when the two currencies differ.
function Converted({ from, to, known, value }: { from: string; to: string; known: boolean; value: string | null }) {
  if (!known || from === to) {
    return null;
  }
  return <span className={styles.converted}>{value === null ? "Not converted" : `≈ ${to} ${value}`}</span>;
}

function Field({
  path,
  label,
  hint,
  error,
  children,
}: {
  path: string;
  label: string;
  hint?: string;
  error?: string;
  children: ReactNode;
}) {
  const id = domId(path);
  return (
    <div className={styles.field}>
      <label htmlFor={id}>{label}</label>
      {hint && (
        <p id={`${id}-hint`} className={styles.hint}>
          {hint}
        </p>
      )}
      {error && (
        <p id={`${id}-error`} className={styles.error}>
          {error}
        </p>
      )}
      {children}
    </div>
  );
}

function controlProps(path: string, error?: string, hasHint = false) {
  const id = domId(path);
  const describedBy = [hasHint ? `${id}-hint` : "", error ? `${id}-error` : ""].filter(Boolean).join(" ");
  return {
    id,
    "aria-invalid": error ? true : undefined,
    "aria-describedby": describedBy || undefined,
  };
}

// Field paths from the API, such as "destinations[0].label", become element
// ids such as "destinations-0-label".
function domId(path: string) {
  return path.replace(/[[\].]+/g, "-").replace(/-$/, "");
}

// useList loads a list once: "loading" until it arrives, then the list, or
// "failed" when it cannot be loaded. Like a comparison request, it gives up
// after 10 seconds.
function useList<T>(request: (signal: AbortSignal) => Promise<T[] | null>): T[] | "loading" | "failed" {
  const [list, setList] = useState<T[] | "loading" | "failed">("loading");
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 10_000);
    request(controller.signal).then((result) => {
      clearTimeout(timer);
      if (active) {
        setList(result ?? "failed");
      }
    });
    return () => {
      active = false;
      clearTimeout(timer);
      controller.abort();
    };
  }, [request]);
  return list;
}

function sameDraft(a: Draft, b: Draft) {
  return JSON.stringify(a) === JSON.stringify(b);
}

function amount(currency: string, value: string | null): ReactNode {
  return value === null ? <span className={styles.unknown}>Unknown</span> : `${currency} ${value}`;
}

function plural(count: number, noun: string) {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

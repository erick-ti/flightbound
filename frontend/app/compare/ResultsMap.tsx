import { useEffect, useRef, useState } from "react";
import type { Map as MapLibreMap, MapOptions, StyleSpecification } from "maplibre-gl";
import "maplibre-gl/dist/maplibre-gl.css";
import styles from "./TripComparison.module.css";

// Pin is one place on the map with the result's destinations at it.
// numbers are those destinations' positions in the result, from 1.
export type Pin = {
  place: string;
  latitude: number;
  longitude: number;
  numbers: number[];
  labels: string[];
};

// Natural Earth 1:110m land, lakes and land borders (release v5.1.2), served
// by this app from public/map exactly as published.
const LAND_URL = "/map/ne_110m_land.geojson";
const LAKES_URL = "/map/ne_110m_lakes.geojson";
const BORDERS_URL = "/map/ne_110m_admin_0_boundary_lines_land.geojson";
// Natural Earth's class for a settled border. The other classes (disputed,
// line of control, indefinite, claim, indeterminate frontier) are drawn
// dashed.
const SETTLED_BORDER = "International boundary (verify)";
const ATTRIBUTION =
  '<a href="https://maplibre.org/">MapLibre</a> | <a href="https://www.naturalearthdata.com/">Natural Earth</a>';
const LOAD_TIMEOUT_MS = 10_000;
const WATER = "#d5e3ec";
const LAND = "#fbfaf6";
const BORDER = "#9aa5b1";

// ResultsMap shows a pin for each place in pins. A pin's button reports
// clicks through onPinClick and is pressed while its place is selected. If
// the map cannot be shown (no WebGL2, a file that fails to load, or no map
// within LOAD_TIMEOUT_MS), it says so instead and calls onFail; the cards
// carry everything.
export default function ResultsMap({
  pins,
  selected,
  onPinClick,
  onFail,
}: {
  pins: Pin[];
  selected: string | null;
  onPinClick: (place: string) => void;
  onFail: () => void;
}) {
  const container = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const buttons = useRef(new Map<string, HTMLButtonElement>());
  const onPinClickRef = useRef(onPinClick);
  const onFailRef = useRef(onFail);
  const selectedRef = useRef(selected);
  const [state, setState] = useState<"loading" | "ready" | "failed">("loading");

  useEffect(() => {
    onPinClickRef.current = onPinClick;
    onFailRef.current = onFail;
    selectedRef.current = selected;
  });

  useEffect(() => {
    const element = container.current;
    if (element === null) {
      return;
    }
    setState("loading");
    const markers = buttons.current;
    let active = true;
    let map: MapLibreMap | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const stop = () => {
      clearTimeout(timer);
      map?.remove();
      map = null;
      mapRef.current = null;
      markers.clear();
    };
    const fail = () => {
      if (active) {
        active = false;
        stop();
        setState("failed");
        onFailRef.current();
      }
    };
    timer = setTimeout(fail, LOAD_TIMEOUT_MS);

    import("maplibre-gl").then((maplibre) => {
      if (!active) {
        return;
      }
      maplibre.setWorkerUrl(new URL("maplibre-gl/dist/maplibre-gl-worker.mjs", import.meta.url).toString());
      let created: MapLibreMap;
      try {
        created = new maplibre.Map({
          container: element,
          style: mapStyle(window.location.origin),
          ...initialView(pins),
          maxZoom: 6,
          renderWorldCopies: false,
          dragRotate: false,
          pitchWithRotate: false,
          touchPitch: false,
          cooperativeGestures: true,
          attributionControl: { customAttribution: ATTRIBUTION },
        });
      } catch {
        // No WebGL2, among other things.
        fail();
        return;
      }
      map = created;
      mapRef.current = created;
      created.touchZoomRotate.disableRotation();
      created.keyboard.disableRotation();
      created.addControl(new maplibre.NavigationControl({ showCompass: false }), "top-right");
      created.on("error", fail);
      if (pins.length > 1) {
        // A resize, such as a turned phone or a narrower window, keeps the
        // center and zoom, which can leave pins near the edges outside.
        created.on("resize", () => created.fitBounds(pinBounds(pins), { ...FIT, animate: false }));
      }
      created.once("load", () => {
        if (!active) {
          return;
        }
        clearTimeout(timer);
        for (const pin of pins) {
          const pressed = pin.place === selectedRef.current;
          const button = pinButton(pin, pressed, () => onPinClickRef.current(pin.place));
          new maplibre.Marker({ element: button }).setLngLat([pin.longitude, pin.latitude]).addTo(created);
          markers.set(pin.place, button);
        }
        setState("ready");
      });
    }, fail);

    return () => {
      active = false;
      stop();
    };
  }, [pins]);

  useEffect(() => {
    for (const [place, button] of buttons.current) {
      button.setAttribute("aria-pressed", String(place === selected));
    }
    const map = mapRef.current;
    const pin = pins.find((p) => p.place === selected);
    if (state === "ready" && map !== null && pin !== undefined) {
      const at: [number, number] = [pin.longitude, pin.latitude];
      if (!map.getBounds().contains(at)) {
        // MapLibre jumps instead of easing when the reader prefers reduced motion.
        map.easeTo({ center: at });
      }
    }
  }, [pins, selected, state]);

  // The container stays in the page when the map fails, so new pins can try
  // again.
  return (
    <>
      {state === "failed" && (
        <p className={styles.mapNote}>The map could not be shown in this browser. Every destination is listed below.</p>
      )}
      <div ref={container} className={styles.map} hidden={state === "failed"} aria-busy={state === "loading"} />
    </>
  );
}

// pinName is a pin's accessible name, such as "Destination 1: Lisbon" or
// "Destinations 1 and 3: Lisbon, Lisbon in May".
export function pinName(pin: Pin): string {
  const n = pin.numbers;
  const which =
    n.length === 1 ? `Destination ${n[0]}` : `Destinations ${n.slice(0, -1).join(", ")} and ${n[n.length - 1]}`;
  return `${which}: ${pin.labels.join(", ")}`;
}

// pinButton builds a pin. Labels are typed by the traveler, so they only
// ever go into text and attributes, never into HTML.
function pinButton(pin: Pin, pressed: boolean, onClick: () => void): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.className = styles.pin;
  button.textContent = pin.numbers.join(",");
  button.setAttribute("aria-label", pinName(pin));
  button.setAttribute("aria-pressed", String(pressed));
  button.addEventListener("click", onClick);
  return button;
}

// How several pins are fitted into view.
const FIT = { padding: 48, maxZoom: 4 };

// initialView fits every pin in view, closer in for a single place.
function initialView(pins: Pin[]): Partial<MapOptions> {
  if (pins.length === 1) {
    return { center: [pins[0].longitude, pins[0].latitude], zoom: 3 };
  }
  return { bounds: pinBounds(pins), fitBoundsOptions: FIT };
}

// pinBounds is the smallest longitude and latitude box around the pins.
function pinBounds(pins: Pin[]): [[number, number], [number, number]] {
  const longitudes = pins.map((p) => p.longitude);
  const latitudes = pins.map((p) => p.latitude);
  return [
    [Math.min(...longitudes), Math.min(...latitudes)],
    [Math.max(...longitudes), Math.max(...latitudes)],
  ];
}

// mapStyle draws water, land, lakes and borders from this app's own files,
// with no labels, so the map needs no fonts or icons from anywhere.
function mapStyle(origin: string): StyleSpecification {
  const file = (path: string) => ({ type: "geojson" as const, data: new URL(path, origin).href });
  return {
    version: 8,
    sources: {
      land: file(LAND_URL),
      lakes: file(LAKES_URL),
      borders: file(BORDERS_URL),
    },
    layers: [
      { id: "water", type: "background", paint: { "background-color": WATER } },
      { id: "land", type: "fill", source: "land", paint: { "fill-color": LAND } },
      { id: "lakes", type: "fill", source: "lakes", paint: { "fill-color": WATER } },
      {
        id: "borders",
        type: "line",
        source: "borders",
        filter: ["==", ["get", "FEATURECLA"], SETTLED_BORDER],
        paint: { "line-color": BORDER, "line-width": 0.75 },
      },
      {
        id: "unsettled-borders",
        type: "line",
        source: "borders",
        filter: ["!=", ["get", "FEATURECLA"], SETTLED_BORDER],
        paint: { "line-color": BORDER, "line-width": 0.75, "line-dasharray": [3, 2] },
      },
    ],
  };
}

import type { NextConfig } from "next";

// The trip comparison API is a separate Go server. These rewrites forward its
// paths so the browser only talks to this origin. `next dev` reads API_ORIGIN
// at startup; `next start` uses the value captured by `next build`.
const apiOrigin = process.env.API_ORIGIN ?? "http://127.0.0.1:8080";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      {
        source: "/api/trip-comparisons",
        destination: `${apiOrigin}/api/trip-comparisons`,
      },
      {
        source: "/api/travel-advice/countries",
        destination: `${apiOrigin}/api/travel-advice/countries`,
      },
    ];
  },
};

export default nextConfig;

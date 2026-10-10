import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // The dashboard talks to the Go API. In development the API runs on :8080 and
  // Next on :3000, so requests are proxied rather than made cross-origin: the
  // workspace admin key then never travels as a CORS preflight-exposed header.
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${process.env.CRIAISIS_API_URL ?? "http://localhost:8088"}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;

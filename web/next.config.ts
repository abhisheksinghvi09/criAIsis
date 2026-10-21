import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  // Self-contained server bundle for the container image.
  output: "standalone",
  // The dashboard talks to the Go API. In development the API runs on :8080 and
  // Next on :3000, so requests are proxied rather than made cross-origin: the
  // workspace admin key then never travels as a CORS preflight-exposed header.
  async headers() {
    return [
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "X-Frame-Options", value: "DENY" },
          { key: "Referrer-Policy", value: "no-referrer" },
          // Next injects inline bootstrap scripts, hence 'unsafe-inline' for scripts and styles.
          // ponytail: swap for nonce-based CSP via middleware if XSS exposure grows.
          {
            key: "Content-Security-Policy",
            value:
              "default-src 'self'; script-src 'self' 'unsafe-inline'" +
              (process.env.NODE_ENV === "development" ? " 'unsafe-eval'" : "") +
              "; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'",
          },
        ],
      },
    ];
  },
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

import type { NextConfig } from "next";
const config: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${process.env.BALLAST_SERVER ?? "http://localhost:8080"}/:path*` },
    ];
  },
};
export default config;

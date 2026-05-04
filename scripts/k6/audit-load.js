import http from "k6/http";
import { check } from "k6";

const mode = (__ENV.MODE || "verify-missing").trim();
const rate = Number(__ENV.RATE || "100");
const duration = __ENV.DURATION || "30s";
const baseURL = (__ENV.BASE_URL || "http://traefik").replace(/\/+$/, "");
const preAllocatedVUs = Number(__ENV.PRE_ALLOCATED_VUS || Math.max(50, Math.ceil(rate / 10)));
const maxVUs = Number(__ENV.MAX_VUS || Math.max(preAllocatedVUs * 2, Math.ceil(rate / 2)));

if (!Number.isFinite(rate) || rate <= 0) {
  throw new Error(`RATE must be a positive number, got ${__ENV.RATE}`);
}

if (mode === "verify-missing") {
  http.setResponseCallback(http.expectedStatuses(401));
} else if (mode === "exchange-valid") {
  http.setResponseCallback(http.expectedStatuses(200));
} else {
  throw new Error(`unsupported MODE ${mode}`);
}

export const options = {
  scenarios: {
    load: {
      executor: "constant-arrival-rate",
      rate,
      timeUnit: "1s",
      duration,
      preAllocatedVUs,
      maxVUs,
    },
  },
  tags: {
    mode,
    rate: String(rate),
  },
  summaryTrendStats: ["avg", "min", "med", "p(90)", "p(95)", "p(99)", "max"],
};

export default function () {
  if (mode === "verify-missing") {
    const res = http.get(`${baseURL}/appcheck/api/v1/verify`, {
      tags: { endpoint: "verify-missing" },
    });
    check(res, {
      "verify missing returns 401": (r) => r.status === 401,
    });
    return;
  }

  const payload = JSON.stringify({
    turnstileToken: "e2e-turnstile-pass",
    limitedUse: false,
  });
  const res = http.post(`${baseURL}/appcheck/api/v1/exchange`, payload, {
    headers: { "Content-Type": "application/json" },
    tags: { endpoint: "exchange-valid" },
  });
  check(res, {
    "exchange valid returns 200": (r) => r.status === 200,
    "exchange valid has token": (r) => {
      try {
        return Boolean(r.json("token"));
      } catch {
        return false;
      }
    },
  });
}

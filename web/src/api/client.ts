import { Api } from "@zeroth/api";

export type Client = Api<unknown>;

export function apiOrigin(): string {
  if (typeof window !== "undefined" && window.location?.origin) {
    return "";
  }
  return "http://127.0.0.1:8420";
}

export function httpOrigin(): string {
  if (typeof window !== "undefined" && window.location?.origin) {
    return window.location.origin;
  }
  return "http://127.0.0.1:8420";
}

export function createApi(customFetch: typeof fetch = fetch.bind(globalThis)): Client {
  return new Api<unknown>({
    baseUrl: apiOrigin(),
    customFetch,
  });
}

export function errorMessage(err: unknown): string {
  if (err && typeof err === "object") {
    const rec = err as { error?: { message?: string }; message?: string };
    if (rec.error?.message) {
      return rec.error.message;
    }
    if (typeof rec.message === "string" && rec.message) {
      return rec.message;
    }
  }
  return "request failed";
}

import createClient from "openapi-fetch";
import type { paths } from "./gen";

// In dev, Vite proxies /api; in prod (embedded), origin is the same as the page.
export const api = createClient<paths>({ baseUrl: "" });

// Module-level token storage for SSE and other non-fetch callers.
let currentToken = "";

export function getAuthToken(): string { return currentToken; }

// Attach a Bearer token to every outgoing request. Called once on app boot
// after the /api/auth/info bootstrap resolves the auth mode.
export function setAuthToken(token: string): void {
  currentToken = token;
  api.use({
    onRequest({ request }) {
      request.headers.set("Authorization", "Bearer " + token);
      return request;
    },
  });
}

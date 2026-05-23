import createClient from "openapi-fetch";
import type { paths } from "./gen";

// In dev, Vite proxies /api; in prod (embedded), origin is the same as the page.
export const api = createClient<paths>({ baseUrl: "" });

// Attach a Bearer token to every outgoing request. Called once on app boot
// after the /api/auth/info bootstrap resolves the auth mode.
export function setAuthToken(token: string): void {
  api.use({
    onRequest({ request }) {
      request.headers.set("Authorization", "Bearer " + token);
      return request;
    },
  });
}

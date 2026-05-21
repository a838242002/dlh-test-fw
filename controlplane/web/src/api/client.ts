import createClient from "openapi-fetch";
import type { paths } from "./gen";

// In dev, Vite proxies /api; in prod (embedded), origin is the same as the page.
export const api = createClient<paths>({ baseUrl: "" });

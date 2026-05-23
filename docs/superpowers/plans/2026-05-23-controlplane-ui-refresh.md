# Controlplane UI Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh the dlh-controlplane embedded React UI with a shadcn/ui design system, dark-default theme + light toggle, a dashboard-forward Runs landing, and cross-cutting UX fixes (toasts, dialogs, readable verdict, auto-refresh, client-side nav) across all five pages.

**Architecture:** Foundation-first. Hand-vendor a minimal shadcn/ui component set (Radix-backed, copied into `src/components/ui/`) plus an indigo CSS-variable theme, then migrate each page onto it. Two pure-logic modules (`computeStats`, `parseVerdict`) are built test-first with Vitest; everything else is gated by `tsc -b && vite build` and manual verification. No backend/API changes.

**Tech Stack:** React 18 + react-router 6 + Vite 5 + Tailwind CSS 3.4 (ESM config, pnpm), openapi-fetch client, shadcn/ui (Radix `dialog`/`alert-dialog`/`select` + `slot`), `sonner` toasts, `lucide-react` icons, `class-variance-authority`/`clsx`/`tailwind-merge`, `tailwindcss-animate`, Vitest (node env).

---

## Conventions for this plan

- All commands run from `controlplane/web` unless stated otherwise. The repo root is `/Users/allen/repo/dlh-test-fw`; the Go module + Makefile live in `controlplane/`.
- Package manager is **pnpm** (there is a `pnpm-lock.yaml`, and `make ui-install` uses `--frozen-lockfile`). Every dependency add MUST update the lockfile, which MUST be committed — otherwise CI's `make ui-build` fails on the frozen lockfile.
- Build/typecheck gate after most tasks: `pnpm build` (which is `tsc -b && vite build`). `tsc -b` only typechecks `src/` (see `tsconfig.json` `include`), so `vite.config.ts` is not typechecked.
- `web/dist`, `web/node_modules`, and `controlplane/internal/api/dist` are git-ignored — built assets are never committed; only source + config + lockfile.
- **Worktree:** This is multi-commit plan work touching the chart-adjacent service. Per `CLAUDE.md`, create a feature branch + worktree at execution start (via `superpowers:using-git-worktrees` if available, else `git worktree add ../dlh-test-fw-ui-refresh -b feat/controlplane-ui-refresh main`). All commits land there; merge to `main` with `--no-ff` at the end.

### API types (from `src/api/gen.ts` — do not redefine, import them)

```ts
type Run = components["schemas"]["Run"];
// { id; scenario; status: "Pending"|"Running"|"Succeeded"|"Failed"|"Error"|"Unknown";
//   startedAt: string; finishedAt?: string; score?: number | null; workflowName?: string;
//   target?: string; triggeredBy?: { kind?: string; id?: string } }
type RunDetail = components["schemas"]["RunDetail"]; // Run & { parameters?; steps?: {name;phase;startedAt?;finishedAt?;message?}[];
//   verdict?: Record<string, unknown> | null; grafanaUrls?: {label;url}[] }
type Schedule = components["schemas"]["Schedule"];   // { id; scenario; target?; cron; timezone?; suspended?; lastScheduledAt?; activeCount?; ... }
type Scenario = components["schemas"]["Scenario"];   // { id; displayName; description?; targetType?; parameters? }
type Target   = components["schemas"]["Target"];     // { id; displayName?; namespace?; allowedTargetTypes?; configured? }
```

### Verdict `report.json` shape (from `verdict-job/internal/eval/eval.go`)

```jsonc
{
  "overall": true,
  "thresholds": [
    { "metric": "p95-latency-chaos", "query": "...", "window": "chaos",
      "window_start": "...", "window_end": "...", "value": 0.0000025,
      "lt": 2.5, "passed": true }       // OR "gt": 100
  ],
  "raw_promql": "...", "raw_promql_value": 1, "raw_promql_pass": true,
  "chaos_window_start": "...", "chaos_window_end": "..."
}
```

---

## Task 1: Dependencies, path alias, and Vitest setup

**Files:**
- Modify: `controlplane/web/package.json` (via `pnpm add`)
- Modify: `controlplane/web/vite.config.ts`
- Modify: `controlplane/web/tsconfig.json`
- Create: `controlplane/web/vitest.config.ts`

- [ ] **Step 1: Add runtime + dev dependencies**

```bash
cd controlplane/web
pnpm add @radix-ui/react-dialog @radix-ui/react-alert-dialog @radix-ui/react-select @radix-ui/react-slot sonner lucide-react class-variance-authority clsx tailwind-merge
pnpm add -D tailwindcss-animate vitest @types/node
```

Expected: both commands succeed and `pnpm-lock.yaml` is updated.

- [ ] **Step 2: Add the `@/*` path alias to `vite.config.ts`**

Replace the file with:

```ts
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
});
```

- [ ] **Step 3: Add `baseUrl` + `paths` to `tsconfig.json`**

In `controlplane/web/tsconfig.json`, add these two keys inside `compilerOptions` (keep everything else as-is):

```jsonc
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] },
```

- [ ] **Step 4: Create `vitest.config.ts`**

```ts
import { defineConfig } from "vitest/config";
import path from "node:path";

export default defineConfig({
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
```

- [ ] **Step 5: Add test scripts to `package.json`**

In the `"scripts"` block add:

```jsonc
    "test": "vitest run",
    "test:watch": "vitest"
```

- [ ] **Step 6: Verify build still passes and Vitest is installed**

```bash
pnpm build
pnpm exec vitest --version
```

Expected: `pnpm build` succeeds (produces `dist/`). `vitest --version` prints a version string (exit 0). Do not run `pnpm test` yet — with no test files, `vitest run` exits non-zero ("No test files found"); the first real tests arrive in Task 8.

- [ ] **Step 7: Commit**

```bash
git add controlplane/web/package.json controlplane/web/pnpm-lock.yaml controlplane/web/vite.config.ts controlplane/web/tsconfig.json controlplane/web/vitest.config.ts
git commit -m "build(web): add shadcn/Radix deps, @ alias, and Vitest"
```

---

## Task 2: Tailwind theme tokens, global CSS, and `cn` util

**Files:**
- Modify: `controlplane/web/tailwind.config.js`
- Modify: `controlplane/web/src/index.css`
- Create: `controlplane/web/src/lib/utils.ts`

- [ ] **Step 1: Replace `tailwind.config.js` with the shadcn theme mapping**

```js
/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        status: {
          success: "hsl(var(--status-success) / <alpha-value>)",
          running: "hsl(var(--status-running) / <alpha-value>)",
          failed: "hsl(var(--status-failed) / <alpha-value>)",
          pending: "hsl(var(--status-pending) / <alpha-value>)",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
};
```

- [ ] **Step 2: Replace `src/index.css` with the token definitions (light + dark)**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 222 47% 11%;
    --card: 0 0% 100%;
    --card-foreground: 222 47% 11%;
    --popover: 0 0% 100%;
    --popover-foreground: 222 47% 11%;
    --primary: 239 84% 67%;
    --primary-foreground: 0 0% 100%;
    --secondary: 210 40% 96%;
    --secondary-foreground: 222 47% 11%;
    --muted: 210 40% 96%;
    --muted-foreground: 215 16% 47%;
    --accent: 210 40% 96%;
    --accent-foreground: 222 47% 11%;
    --destructive: 0 84% 60%;
    --destructive-foreground: 0 0% 100%;
    --border: 214 32% 91%;
    --input: 214 32% 91%;
    --ring: 239 84% 67%;
    --radius: 0.5rem;

    --status-success: 142 71% 45%;
    --status-running: 217 91% 60%;
    --status-failed: 0 84% 60%;
    --status-pending: 215 16% 47%;
  }

  .dark {
    --background: 222 47% 11%;
    --foreground: 210 40% 98%;
    --card: 222 47% 13%;
    --card-foreground: 210 40% 98%;
    --popover: 222 47% 11%;
    --popover-foreground: 210 40% 98%;
    --primary: 239 84% 74%;
    --primary-foreground: 222 47% 11%;
    --secondary: 217 33% 18%;
    --secondary-foreground: 210 40% 98%;
    --muted: 217 33% 18%;
    --muted-foreground: 215 20% 65%;
    --accent: 217 33% 18%;
    --accent-foreground: 210 40% 98%;
    --destructive: 0 63% 50%;
    --destructive-foreground: 210 40% 98%;
    --border: 217 33% 20%;
    --input: 217 33% 20%;
    --ring: 239 84% 74%;

    --status-success: 142 69% 58%;
    --status-running: 213 94% 68%;
    --status-failed: 0 91% 71%;
    --status-pending: 215 20% 65%;
  }
}

@layer base {
  * {
    @apply border-border;
  }
  body {
    @apply bg-background text-foreground;
  }
}
```

- [ ] **Step 3: Create `src/lib/utils.ts`**

```ts
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
```

- [ ] **Step 4: Verify build**

Run: `pnpm build`
Expected: PASS. (Existing pages still render with the new token-based base colors; some bespoke `slate-*` classes remain until pages are migrated — that is fine.)

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/tailwind.config.js controlplane/web/src/index.css controlplane/web/src/lib/utils.ts
git commit -m "feat(web): indigo CSS-variable theme + status tokens + cn util"
```

---

## Task 3: Vendor base UI primitives (button, card, badge, input, skeleton, table)

These are the standard shadcn implementations (no Radix except `Slot` for Button).

**Files:**
- Create: `controlplane/web/src/components/ui/button.tsx`
- Create: `controlplane/web/src/components/ui/card.tsx`
- Create: `controlplane/web/src/components/ui/badge.tsx`
- Create: `controlplane/web/src/components/ui/input.tsx`
- Create: `controlplane/web/src/components/ui/skeleton.tsx`
- Create: `controlplane/web/src/components/ui/table.tsx`

- [ ] **Step 1: Create `button.tsx`**

```tsx
import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        outline: "border border-input bg-background hover:bg-accent hover:text-accent-foreground",
        secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        ghost: "hover:bg-accent hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 rounded-md px-3 text-xs",
        lg: "h-10 rounded-md px-8",
        icon: "h-9 w-9",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
    );
  }
);
Button.displayName = "Button";

export { Button, buttonVariants };
```

- [ ] **Step 2: Create `card.tsx`**

```tsx
import * as React from "react";
import { cn } from "@/lib/utils";

const Card = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("rounded-lg border bg-card text-card-foreground shadow-sm", className)} {...props} />
  )
);
Card.displayName = "Card";

const CardHeader = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex flex-col space-y-1.5 p-6", className)} {...props} />
  )
);
CardHeader.displayName = "CardHeader";

const CardTitle = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("text-lg font-semibold leading-none tracking-tight", className)} {...props} />
  )
);
CardTitle.displayName = "CardTitle";

const CardDescription = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("text-sm text-muted-foreground", className)} {...props} />
  )
);
CardDescription.displayName = "CardDescription";

const CardContent = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => <div ref={ref} className={cn("p-6 pt-0", className)} {...props} />
);
CardContent.displayName = "CardContent";

const CardFooter = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("flex items-center p-6 pt-0", className)} {...props} />
  )
);
CardFooter.displayName = "CardFooter";

export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter };
```

- [ ] **Step 3: Create `badge.tsx`**

```tsx
import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary: "border-transparent bg-secondary text-secondary-foreground",
        destructive: "border-transparent bg-destructive text-destructive-foreground",
        outline: "text-foreground",
      },
    },
    defaultVariants: { variant: "default" },
  }
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
```

- [ ] **Step 4: Create `input.tsx`**

```tsx
import * as React from "react";
import { cn } from "@/lib/utils";

const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type, ...props }, ref) => (
    <input
      type={type}
      ref={ref}
      className={cn(
        "flex h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50",
        className
      )}
      {...props}
    />
  )
);
Input.displayName = "Input";

export { Input };
```

- [ ] **Step 5: Create `skeleton.tsx`**

```tsx
import * as React from "react";
import { cn } from "@/lib/utils";

function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("animate-pulse rounded-md bg-muted", className)} {...props} />;
}

export { Skeleton };
```

- [ ] **Step 6: Create `table.tsx`**

```tsx
import * as React from "react";
import { cn } from "@/lib/utils";

const Table = React.forwardRef<HTMLTableElement, React.HTMLAttributes<HTMLTableElement>>(
  ({ className, ...props }, ref) => (
    <div className="relative w-full overflow-auto">
      <table ref={ref} className={cn("w-full caption-bottom text-sm", className)} {...props} />
    </div>
  )
);
Table.displayName = "Table";

const TableHeader = React.forwardRef<HTMLTableSectionElement, React.HTMLAttributes<HTMLTableSectionElement>>(
  ({ className, ...props }, ref) => <thead ref={ref} className={cn("[&_tr]:border-b", className)} {...props} />
);
TableHeader.displayName = "TableHeader";

const TableBody = React.forwardRef<HTMLTableSectionElement, React.HTMLAttributes<HTMLTableSectionElement>>(
  ({ className, ...props }, ref) => (
    <tbody ref={ref} className={cn("[&_tr:last-child]:border-0", className)} {...props} />
  )
);
TableBody.displayName = "TableBody";

const TableRow = React.forwardRef<HTMLTableRowElement, React.HTMLAttributes<HTMLTableRowElement>>(
  ({ className, ...props }, ref) => (
    <tr
      ref={ref}
      className={cn("border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted", className)}
      {...props}
    />
  )
);
TableRow.displayName = "TableRow";

const TableHead = React.forwardRef<HTMLTableCellElement, React.ThHTMLAttributes<HTMLTableCellElement>>(
  ({ className, ...props }, ref) => (
    <th
      ref={ref}
      className={cn(
        "h-10 px-2 text-left align-middle text-xs font-medium uppercase tracking-wide text-muted-foreground",
        className
      )}
      {...props}
    />
  )
);
TableHead.displayName = "TableHead";

const TableCell = React.forwardRef<HTMLTableCellElement, React.TdHTMLAttributes<HTMLTableCellElement>>(
  ({ className, ...props }, ref) => (
    <td ref={ref} className={cn("p-2 align-middle", className)} {...props} />
  )
);
TableCell.displayName = "TableCell";

export { Table, TableHeader, TableBody, TableRow, TableHead, TableCell };
```

- [ ] **Step 7: Verify build**

Run: `pnpm build`
Expected: PASS (unused-component warnings are not errors; `noUnusedLocals` applies to locals within a file, not unused exports across files).

- [ ] **Step 8: Commit**

```bash
git add controlplane/web/src/components/ui/
git commit -m "feat(web): vendor base shadcn primitives (button/card/badge/input/skeleton/table)"
```

---

## Task 4: Vendor Radix-backed primitives (dialog, alert-dialog, select, sonner)

**Files:**
- Create: `controlplane/web/src/components/ui/dialog.tsx`
- Create: `controlplane/web/src/components/ui/alert-dialog.tsx`
- Create: `controlplane/web/src/components/ui/select.tsx`
- Create: `controlplane/web/src/components/ui/sonner.tsx`

- [ ] **Step 1: Create `dialog.tsx`**

```tsx
import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { X } from "lucide-react";
import { cn } from "@/lib/utils";

const Dialog = DialogPrimitive.Root;
const DialogTrigger = DialogPrimitive.Trigger;
const DialogPortal = DialogPrimitive.Portal;
const DialogClose = DialogPrimitive.Close;

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-black/60 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className
    )}
    {...props}
  />
));
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed left-1/2 top-1/2 z-50 grid w-full max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4 border bg-background p-6 shadow-lg duration-200 sm:rounded-lg",
        className
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring">
        <X className="h-4 w-4" />
        <span className="sr-only">Close</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
));
DialogContent.displayName = DialogPrimitive.Content.displayName;

const DialogHeader = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col space-y-1.5 text-center sm:text-left", className)} {...props} />
);
DialogHeader.displayName = "DialogHeader";

const DialogFooter = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2", className)} {...props} />
);
DialogFooter.displayName = "DialogFooter";

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn("text-lg font-semibold leading-none tracking-tight", className)} {...props} />
));
DialogTitle.displayName = DialogPrimitive.Title.displayName;

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description ref={ref} className={cn("text-sm text-muted-foreground", className)} {...props} />
));
DialogDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Dialog, DialogPortal, DialogOverlay, DialogClose, DialogTrigger,
  DialogContent, DialogHeader, DialogFooter, DialogTitle, DialogDescription,
};
```

- [ ] **Step 2: Create `alert-dialog.tsx`**

```tsx
import * as React from "react";
import * as AlertDialogPrimitive from "@radix-ui/react-alert-dialog";
import { cn } from "@/lib/utils";
import { buttonVariants } from "@/components/ui/button";

const AlertDialog = AlertDialogPrimitive.Root;
const AlertDialogTrigger = AlertDialogPrimitive.Trigger;
const AlertDialogPortal = AlertDialogPrimitive.Portal;

const AlertDialogOverlay = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Overlay ref={ref} className={cn("fixed inset-0 z-50 bg-black/60", className)} {...props} />
));
AlertDialogOverlay.displayName = AlertDialogPrimitive.Overlay.displayName;

const AlertDialogContent = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Content>
>(({ className, ...props }, ref) => (
  <AlertDialogPortal>
    <AlertDialogOverlay />
    <AlertDialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed left-1/2 top-1/2 z-50 grid w-full max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4 border bg-background p-6 shadow-lg sm:rounded-lg",
        className
      )}
      {...props}
    />
  </AlertDialogPortal>
));
AlertDialogContent.displayName = AlertDialogPrimitive.Content.displayName;

const AlertDialogHeader = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col space-y-2 text-center sm:text-left", className)} {...props} />
);
AlertDialogHeader.displayName = "AlertDialogHeader";

const AlertDialogFooter = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2", className)} {...props} />
);
AlertDialogFooter.displayName = "AlertDialogFooter";

const AlertDialogTitle = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Title ref={ref} className={cn("text-lg font-semibold", className)} {...props} />
));
AlertDialogTitle.displayName = AlertDialogPrimitive.Title.displayName;

const AlertDialogDescription = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Description ref={ref} className={cn("text-sm text-muted-foreground", className)} {...props} />
));
AlertDialogDescription.displayName = AlertDialogPrimitive.Description.displayName;

const AlertDialogAction = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Action>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Action>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Action ref={ref} className={cn(buttonVariants(), className)} {...props} />
));
AlertDialogAction.displayName = AlertDialogPrimitive.Action.displayName;

const AlertDialogCancel = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Cancel>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Cancel>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Cancel ref={ref} className={cn(buttonVariants({ variant: "outline" }), "mt-2 sm:mt-0", className)} {...props} />
));
AlertDialogCancel.displayName = AlertDialogPrimitive.Cancel.displayName;

export {
  AlertDialog, AlertDialogPortal, AlertDialogOverlay, AlertDialogTrigger,
  AlertDialogContent, AlertDialogHeader, AlertDialogFooter, AlertDialogTitle,
  AlertDialogDescription, AlertDialogAction, AlertDialogCancel,
};
```

- [ ] **Step 3: Create `select.tsx`**

```tsx
import * as React from "react";
import * as SelectPrimitive from "@radix-ui/react-select";
import { Check, ChevronDown } from "lucide-react";
import { cn } from "@/lib/utils";

const Select = SelectPrimitive.Root;
const SelectGroup = SelectPrimitive.Group;
const SelectValue = SelectPrimitive.Value;

const SelectTrigger = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Trigger
    ref={ref}
    className={cn(
      "flex h-9 w-full items-center justify-between rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50 [&>span]:line-clamp-1",
      className
    )}
    {...props}
  >
    {children}
    <SelectPrimitive.Icon asChild>
      <ChevronDown className="h-4 w-4 opacity-50" />
    </SelectPrimitive.Icon>
  </SelectPrimitive.Trigger>
));
SelectTrigger.displayName = SelectPrimitive.Trigger.displayName;

const SelectContent = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Content>
>(({ className, children, position = "popper", ...props }, ref) => (
  <SelectPrimitive.Portal>
    <SelectPrimitive.Content
      ref={ref}
      className={cn(
        "relative z-50 max-h-96 min-w-[8rem] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md",
        position === "popper" && "data-[side=bottom]:translate-y-1",
        className
      )}
      position={position}
      {...props}
    >
      <SelectPrimitive.Viewport
        className={cn("p-1", position === "popper" && "w-full min-w-[var(--radix-select-trigger-width)]")}
      >
        {children}
      </SelectPrimitive.Viewport>
    </SelectPrimitive.Content>
  </SelectPrimitive.Portal>
));
SelectContent.displayName = SelectPrimitive.Content.displayName;

const SelectItem = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Item>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex w-full cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <SelectPrimitive.ItemIndicator>
        <Check className="h-4 w-4" />
      </SelectPrimitive.ItemIndicator>
    </span>
    <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
  </SelectPrimitive.Item>
));
SelectItem.displayName = SelectPrimitive.Item.displayName;

export { Select, SelectGroup, SelectValue, SelectTrigger, SelectContent, SelectItem };
```

- [ ] **Step 4: Create `sonner.tsx`**

```tsx
import { type ComponentProps } from "react";
import { useTheme } from "@/lib/theme";
import { Toaster as Sonner } from "sonner";

type ToasterProps = ComponentProps<typeof Sonner>;

export function Toaster(props: ToasterProps) {
  const { resolved } = useTheme();
  return (
    <Sonner
      theme={resolved}
      className="toaster group"
      toastOptions={{
        classNames: {
          toast:
            "group toast group-[.toaster]:bg-background group-[.toaster]:text-foreground group-[.toaster]:border-border group-[.toaster]:shadow-lg",
          description: "group-[.toast]:text-muted-foreground",
        },
      }}
      {...props}
    />
  );
}
```

Note: `@/lib/theme` (the `useTheme` hook) is created in Task 5. This file will not typecheck until then — that is expected; Task 5 immediately follows and the build is verified there.

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/components/ui/
git commit -m "feat(web): vendor Radix primitives (dialog/alert-dialog/select/sonner)"
```

---

## Task 5: Theme provider + hook

**Files:**
- Create: `controlplane/web/src/lib/theme.tsx`

- [ ] **Step 1: Create `src/lib/theme.tsx`**

```tsx
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

type Theme = "dark" | "light";
const STORAGE_KEY = "dlh-theme";

type ThemeContextValue = {
  theme: Theme;
  resolved: Theme;
  setTheme: (t: Theme) => void;
  toggle: () => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

function readStored(): Theme {
  const v = localStorage.getItem(STORAGE_KEY);
  return v === "light" ? "light" : "dark"; // default dark
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(readStored);

  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle("dark", theme === "dark");
    localStorage.setItem(STORAGE_KEY, theme);
  }, [theme]);

  const setTheme = (t: Theme) => setThemeState(t);
  const toggle = () => setThemeState((t) => (t === "dark" ? "light" : "dark"));

  return (
    <ThemeContext.Provider value={{ theme, resolved: theme, setTheme, toggle }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used within ThemeProvider");
  return ctx;
}
```

- [ ] **Step 2: Wrap the app with `ThemeProvider` in `main.tsx`**

Replace `controlplane/web/src/main.tsx` with:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import App from "./App";
import { ThemeProvider } from "@/lib/theme";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ThemeProvider>
  </StrictMode>
);
```

- [ ] **Step 3: Verify build (sonner.tsx now resolves)**

Run: `pnpm build`
Expected: PASS — `src/components/ui/sonner.tsx` now finds `@/lib/theme`.

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/lib/theme.tsx controlplane/web/src/main.tsx
git commit -m "feat(web): dark-default ThemeProvider with light toggle"
```

---

## Task 6: Custom presentational primitives

**Files:**
- Create: `controlplane/web/src/components/StatusBadge.tsx` (replaces the old one)
- Create: `controlplane/web/src/components/StatCard.tsx`
- Create: `controlplane/web/src/components/PageHeader.tsx`
- Create: `controlplane/web/src/components/EmptyState.tsx`
- Create: `controlplane/web/src/components/ErrorState.tsx`

- [ ] **Step 1: Overwrite `src/components/StatusBadge.tsx`**

```tsx
import { cn } from "@/lib/utils";

const STATUS_STYLES: Record<string, string> = {
  Succeeded: "bg-status-success/15 text-status-success",
  Running: "bg-status-running/15 text-status-running",
  Failed: "bg-status-failed/15 text-status-failed",
  Error: "bg-status-failed/15 text-status-failed",
  Pending: "bg-status-pending/15 text-status-pending",
  Unknown: "bg-status-pending/15 text-status-pending",
};

export function StatusBadge({ status }: { status: string }) {
  const style = STATUS_STYLES[status] ?? STATUS_STYLES.Unknown;
  return (
    <span className={cn("inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium", style)}>
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {status}
    </span>
  );
}
```

- [ ] **Step 2: Create `src/components/StatCard.tsx`**

```tsx
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function StatCard({
  label,
  value,
  accent,
}: {
  label: string;
  value: string;
  accent?: "primary" | "success" | "running" | "failed";
}) {
  const accentClass =
    accent === "success"
      ? "text-status-success"
      : accent === "running"
      ? "text-status-running"
      : accent === "failed"
      ? "text-status-failed"
      : accent === "primary"
      ? "text-primary"
      : "text-foreground";
  return (
    <Card>
      <CardContent className="p-4">
        <div className="text-sm text-muted-foreground">{label}</div>
        <div className={cn("mt-1 text-2xl font-bold", accentClass)}>{value}</div>
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 3: Create `src/components/PageHeader.tsx`**

```tsx
import { type ReactNode } from "react";

export function PageHeader({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <div className="mb-6 flex items-center justify-between">
      <h1 className="text-xl font-semibold">{title}</h1>
      {action}
    </div>
  );
}
```

- [ ] **Step 4: Create `src/components/EmptyState.tsx`**

```tsx
import { type ReactNode } from "react";
import { Inbox } from "lucide-react";

export function EmptyState({ message, hint }: { message: string; hint?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-16 text-center">
      <Inbox className="mb-3 h-8 w-8 text-muted-foreground" />
      <p className="font-medium">{message}</p>
      {hint && <p className="mt-1 text-sm text-muted-foreground">{hint}</p>}
    </div>
  );
}
```

- [ ] **Step 5: Create `src/components/ErrorState.tsx`**

```tsx
import { AlertTriangle } from "lucide-react";

export function ErrorState({ message, details }: { message: string; details?: unknown }) {
  const detailText =
    details == null ? "" : typeof details === "string" ? details : JSON.stringify(details, null, 2);
  return (
    <div className="rounded-lg border border-destructive/40 bg-destructive/10 p-4">
      <div className="flex items-center gap-2 font-medium text-destructive">
        <AlertTriangle className="h-4 w-4" />
        {message}
      </div>
      {detailText && (
        <details className="mt-2 text-sm text-muted-foreground">
          <summary className="cursor-pointer">Details</summary>
          <pre className="mt-2 overflow-auto whitespace-pre-wrap text-xs">{detailText}</pre>
        </details>
      )}
    </div>
  );
}
```

- [ ] **Step 6: Verify build**

Run: `pnpm build`
Expected: PASS. (The old `StatusBadge` API — a `{ status }` prop — is unchanged, so existing pages still compile.)

- [ ] **Step 7: Commit**

```bash
git add controlplane/web/src/components/
git commit -m "feat(web): custom primitives (StatusBadge/StatCard/PageHeader/EmptyState/ErrorState)"
```

---

## Task 7: App shell — nav, theme toggle, Toaster, identity

**Files:**
- Modify: `controlplane/web/src/App.tsx`

The existing `App.tsx` already does the `/api/auth/info` bootstrap and renders the routes (see current file). Preserve the bootstrap logic; replace only the chrome (header/nav) and mount the Toaster + theme toggle. Capture the identity email during bootstrap.

- [ ] **Step 1: Replace `src/App.tsx`**

```tsx
import { useEffect, useState } from "react";
import { Routes, Route, NavLink } from "react-router-dom";
import { Moon, Sun } from "lucide-react";
import { ScenariosPage } from "./pages/ScenariosPage";
import { RunsPage } from "./pages/RunsPage";
import { RunDetailPage } from "./pages/RunDetailPage";
import { TargetsPage } from "./pages/TargetsPage";
import { SchedulesPage } from "./pages/SchedulesPage";
import { setAuthToken } from "./api/client";
import { useTheme } from "@/lib/theme";
import { Toaster } from "@/components/ui/sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const TOKEN_KEY = "dlh-token";
const FAKE_TOKEN = "fake:admin:admin@local:dlh-admin";

const NAV = [
  { to: "/runs", label: "Runs" },
  { to: "/scenarios", label: "Scenarios" },
  { to: "/targets", label: "Targets" },
  { to: "/schedules", label: "Schedules" },
];

function ThemeToggle() {
  const { theme, toggle } = useTheme();
  return (
    <Button variant="ghost" size="icon" onClick={toggle} aria-label="Toggle theme">
      {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
    </Button>
  );
}

export default function App() {
  const [ready, setReady] = useState(false);
  const [authErr, setAuthErr] = useState<string | null>(null);
  const [identity, setIdentity] = useState<string>("");

  useEffect(() => {
    fetch("/api/auth/info")
      .then((r) => r.json())
      .then((info: { authDisabled?: boolean }) => {
        if (info.authDisabled) {
          setAuthToken(FAKE_TOKEN);
          setIdentity("admin@local");
        } else {
          const tok = localStorage.getItem(TOKEN_KEY);
          if (tok) {
            setAuthToken(tok);
          } else {
            setAuthErr("Not authenticated. Run `dlh login` or set a session token.");
            return;
          }
        }
        setReady(true);
      })
      .catch((e) => setAuthErr(String(e)));
  }, []);

  if (authErr) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <p className="max-w-md rounded border border-destructive/40 bg-destructive/10 px-6 py-4 text-destructive">
          {authErr}
        </p>
      </div>
    );
  }

  if (!ready) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <p className="text-muted-foreground">Connecting…</p>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <header className="border-b bg-card">
        <nav className="mx-auto flex max-w-6xl items-center gap-4 px-6 py-3 text-sm">
          <span className="font-semibold text-primary">◆ dlh</span>
          {NAV.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              className={({ isActive }) =>
                cn(
                  "border-b-2 pb-0.5 transition-colors",
                  isActive
                    ? "border-primary font-medium text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                )
              }
            >
              {n.label}
            </NavLink>
          ))}
          <div className="ml-auto flex items-center gap-3">
            {identity && <span className="text-xs text-muted-foreground">{identity}</span>}
            <ThemeToggle />
          </div>
        </nav>
      </header>
      <main className="mx-auto max-w-6xl px-6 py-8">
        <Routes>
          <Route path="/" element={<RunsPage />} />
          <Route path="/scenarios" element={<ScenariosPage />} />
          <Route path="/runs" element={<RunsPage />} />
          <Route path="/runs/:id" element={<RunDetailPage />} />
          <Route path="/targets" element={<TargetsPage />} />
          <Route path="/schedules" element={<SchedulesPage />} />
        </Routes>
      </main>
      <Toaster />
    </div>
  );
}
```

- [ ] **Step 2: Verify build + manual smoke**

Run: `pnpm build`
Expected: PASS.

Manual (optional but recommended): `DLH_AUTH_DISABLED=true` against a running controlplane, or `pnpm dev` with the proxy. Confirm: nav active-link underline tracks the route, the theme toggle flips dark/light and persists across reload, and `admin@local` shows at the right.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/App.tsx
git commit -m "feat(web): app shell with active nav, theme toggle, identity, Toaster"
```

---

## Task 8: `computeStats` pure-logic module (TDD)

**Files:**
- Create: `controlplane/web/src/lib/stats.ts`
- Test: `controlplane/web/src/lib/stats.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { computeStats } from "@/lib/stats";
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

const iso = (d: Date) => d.toISOString();
const daysAgo = (n: number) => new Date(Date.now() - n * 86_400_000);
const hoursAgo = (n: number) => new Date(Date.now() - n * 3_600_000);

function run(partial: Partial<Run>): Run {
  return { id: "x", scenario: "s", status: "Succeeded", startedAt: iso(new Date()), ...partial } as Run;
}

describe("computeStats", () => {
  it("pass rate counts Succeeded over terminal runs in last 7d", () => {
    const runs = [
      run({ status: "Succeeded", startedAt: iso(daysAgo(1)) }),
      run({ status: "Succeeded", startedAt: iso(daysAgo(2)) }),
      run({ status: "Failed", startedAt: iso(daysAgo(3)) }),
      run({ status: "Error", startedAt: iso(daysAgo(4)) }),
      run({ status: "Running", startedAt: iso(daysAgo(1)) }), // ignored (not terminal)
      run({ status: "Succeeded", startedAt: iso(daysAgo(9)) }), // ignored (>7d)
    ];
    const s = computeStats(runs, []);
    expect(s.passRate7d).toBeCloseTo(0.5); // 2 succeeded / 4 terminal
  });

  it("passRate7d is null when there are no terminal runs in window", () => {
    const s = computeStats([run({ status: "Running", startedAt: iso(hoursAgo(1)) })], []);
    expect(s.passRate7d).toBeNull();
  });

  it("runsToday counts runs started since local midnight", () => {
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    const runs = [
      run({ startedAt: iso(new Date(midnight.getTime() + 3_600_000)) }), // today
      run({ startedAt: iso(new Date(midnight.getTime() - 3_600_000)) }), // yesterday
    ];
    expect(computeStats(runs, []).runsToday).toBe(1);
  });

  it("runningNow counts Running status", () => {
    const runs = [run({ status: "Running" }), run({ status: "Running" }), run({ status: "Succeeded" })];
    expect(computeStats(runs, []).runningNow).toBe(2);
  });

  it("activeSchedules counts non-suspended schedules", () => {
    const schedules = [
      { id: "a", scenario: "s", cron: "* * * * *", suspended: false } as Schedule,
      { id: "b", scenario: "s", cron: "* * * * *", suspended: true } as Schedule,
      { id: "c", scenario: "s", cron: "* * * * *" } as Schedule, // undefined => active
    ];
    expect(computeStats([], schedules).activeSchedules).toBe(2);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `computeStats` cannot be imported (module/function does not exist).

- [ ] **Step 3: Implement `src/lib/stats.ts`**

```ts
import type { components } from "@/api/gen";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

export interface Stats {
  passRate7d: number | null;
  runsToday: number;
  runningNow: number;
  activeSchedules: number;
}

const SEVEN_DAYS_MS = 7 * 86_400_000;

export function computeStats(runs: Run[], schedules: Schedule[]): Stats {
  const now = Date.now();
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);

  let succeeded = 0;
  let terminal = 0;
  let runsToday = 0;
  let runningNow = 0;

  for (const r of runs) {
    const started = new Date(r.startedAt).getTime();
    if (r.status === "Running") runningNow++;
    if (started >= midnight.getTime()) runsToday++;
    if (now - started <= SEVEN_DAYS_MS) {
      if (r.status === "Succeeded") {
        succeeded++;
        terminal++;
      } else if (r.status === "Failed" || r.status === "Error") {
        terminal++;
      }
    }
  }

  const activeSchedules = schedules.filter((s) => !s.suspended).length;

  return {
    passRate7d: terminal === 0 ? null : succeeded / terminal,
    runsToday,
    runningNow,
    activeSchedules,
  };
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS (5 tests in `stats.test.ts`).

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/lib/stats.ts controlplane/web/src/lib/stats.test.ts
git commit -m "feat(web): computeStats module with Vitest coverage"
```

---

## Task 9: Migrate Runs page (dashboard + table + polling)

**Files:**
- Modify: `controlplane/web/src/pages/RunsPage.tsx`

- [ ] **Step 1: Replace `src/pages/RunsPage.tsx`**

```tsx
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { StatCard } from "@/components/StatCard";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { computeStats } from "@/lib/stats";

type Run = components["schemas"]["Run"];
type Schedule = components["schemas"]["Schedule"];

const POLL_MS = 5000;

export function RunsPage() {
  const navigate = useNavigate();
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [secondsAgo, setSecondsAgo] = useState(0);

  const reload = useCallback(async () => {
    const [runsRes, schedRes] = await Promise.all([
      api.GET("/api/runs", { params: { query: { limit: 100 } } }),
      api.GET("/api/schedules", {}),
    ]);
    if (runsRes.error) {
      setError(runsRes.error);
      return;
    }
    setError(null);
    setRuns(runsRes.data?.items ?? []);
    setSchedules(schedRes.data?.items ?? []);
    setSecondsAgo(0);
  }, []);

  useEffect(() => {
    reload();
    const poll = setInterval(reload, POLL_MS);
    const tick = setInterval(() => setSecondsAgo((n) => n + 1), 1000);
    return () => {
      clearInterval(poll);
      clearInterval(tick);
    };
  }, [reload]);

  if (error) return <ErrorState message="Failed to load runs" details={error} />;

  const stats = runs ? computeStats(runs, schedules) : null;

  return (
    <section>
      <PageHeader title="Runs" />

      <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatCard
          label="Pass rate (7d)"
          value={stats == null ? "—" : stats.passRate7d == null ? "—" : `${Math.round(stats.passRate7d * 100)}%`}
          accent="success"
        />
        <StatCard label="Runs today" value={stats == null ? "—" : String(stats.runsToday)} />
        <StatCard label="Running now" value={stats == null ? "—" : String(stats.runningNow)} accent="running" />
        <StatCard label="Active schedules" value={stats == null ? "—" : String(stats.activeSchedules)} />
      </div>

      <Card>
        <div className="flex items-center justify-between border-b px-4 py-3">
          <span className="font-medium">Recent runs</span>
          {runs && (
            <span className="text-xs text-muted-foreground">● live · updated {secondsAgo}s ago</span>
          )}
        </div>
        {!runs ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-8 w-full" />
            ))}
          </div>
        ) : runs.length === 0 ? (
          <div className="p-4">
            <EmptyState message="No runs yet" hint="Submit a scenario from the Scenarios page." />
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scenario</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Started</TableHead>
                <TableHead>Score</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((r) => (
                <TableRow
                  key={r.id}
                  className="cursor-pointer"
                  onClick={() => navigate(`/runs/${r.id}`)}
                >
                  <TableCell className="font-medium">{r.scenario}</TableCell>
                  <TableCell className="text-muted-foreground">{r.target || "local"}</TableCell>
                  <TableCell>
                    <StatusBadge status={String(r.status)} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {new Date(r.startedAt).toLocaleString()}
                  </TableCell>
                  <TableCell>{r.score == null ? "—" : r.score.toFixed(2)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>
    </section>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/pages/RunsPage.tsx
git commit -m "feat(web): dashboard Runs page — stat cards, polling, skeleton/empty/error"
```

---

## Task 10: Migrate Scenarios page + TargetPicker on Select

**Files:**
- Modify: `controlplane/web/src/components/TargetPicker.tsx`
- Modify: `controlplane/web/src/pages/ScenariosPage.tsx`

- [ ] **Step 1: Reimplement `src/components/TargetPicker.tsx` on shadcn Select**

```tsx
import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type Target = components["schemas"]["Target"];

const LOCAL_VALUE = "__local__";

export function TargetPicker({
  value,
  onChange,
  filterType,
}: {
  value: string;
  onChange: (id: string) => void;
  filterType?: string;
}) {
  const [items, setItems] = useState<Target[] | null>(null);

  useEffect(() => {
    api.GET("/api/targets", {}).then(({ data }) => setItems(data?.items ?? []));
  }, []);

  const filtered = (items ?? [])
    .filter((t) => t.configured)
    .filter(
      (t) => !filterType || !t.allowedTargetTypes?.length || t.allowedTargetTypes.includes(filterType)
    );

  return (
    <Select
      value={value === "" ? LOCAL_VALUE : value}
      onValueChange={(v) => onChange(v === LOCAL_VALUE ? "" : v)}
    >
      <SelectTrigger className="h-8 w-[200px] text-xs">
        <SelectValue placeholder="Select target" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={LOCAL_VALUE}>local — framework cluster</SelectItem>
        {filtered.map((t) => (
          <SelectItem key={t.id} value={t.id}>
            {t.displayName ?? t.id}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
```

Note: Radix `Select` forbids an empty-string item value, so "local" uses the `__local__` sentinel mapped back to `""` for the API.

- [ ] **Step 2: Replace `src/pages/ScenariosPage.tsx`**

```tsx
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { TargetPicker } from "@/components/TargetPicker";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

type Scenario = components["schemas"]["Scenario"];

export function ScenariosPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<Scenario[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [submitTarget, setSubmitTarget] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/scenarios", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
  }, []);

  const handleRun = async (s: Scenario) => {
    setSubmitting(s.id);
    try {
      const targetId = submitTarget[s.id] || undefined;
      const { data, error } = await api.POST("/api/runs", { body: { scenarioId: s.id, targetId } });
      if (error) {
        toast.error("Submit failed", { description: JSON.stringify(error) });
      } else if (data?.id) {
        toast.success(`Run ${data.id} submitted`);
        navigate(`/runs/${data.id}`);
      }
    } finally {
      setSubmitting(null);
    }
  };

  if (error) return <ErrorState message="Failed to load scenarios" details={error} />;

  return (
    <section>
      <PageHeader title="Scenarios" />
      {!items ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-40 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <EmptyState message="No scenarios available" />
      ) : (
        <ul className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
          {items.map((s) => (
            <li key={s.id}>
              <Card className="flex h-full flex-col">
                <CardHeader>
                  <CardTitle className="text-base">{s.displayName}</CardTitle>
                  {s.targetType && <CardDescription>{s.targetType}</CardDescription>}
                </CardHeader>
                <CardContent className="flex flex-1 flex-col justify-between gap-3">
                  {s.description && <p className="text-sm text-muted-foreground">{s.description}</p>}
                  <div className="flex items-center gap-2">
                    <TargetPicker
                      value={submitTarget[s.id] ?? ""}
                      onChange={(v) => setSubmitTarget((r) => ({ ...r, [s.id]: v }))}
                      filterType={s.targetType ?? undefined}
                    />
                    <Button size="sm" disabled={submitting === s.id} onClick={() => handleRun(s)}>
                      {submitting === s.id ? "Submitting…" : "Run"}
                    </Button>
                  </div>
                </CardContent>
              </Card>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
```

- [ ] **Step 3: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/components/TargetPicker.tsx controlplane/web/src/pages/ScenariosPage.tsx
git commit -m "feat(web): Scenarios page on Card/Select with toast + client-side navigate"
```

---

## Task 11: Migrate Targets page

**Files:**
- Modify: `controlplane/web/src/pages/TargetsPage.tsx`

- [ ] **Step 1: Replace `src/pages/TargetsPage.tsx`**

```tsx
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

type Target = components["schemas"]["Target"];

export function TargetsPage() {
  const [items, setItems] = useState<Target[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [testing, setTesting] = useState<string | null>(null);

  useEffect(() => {
    api.GET("/api/targets", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });
  }, []);

  const testConn = async (id: string) => {
    setTesting(id);
    try {
      const { data, error } = await api.POST("/api/targets/{id}/test", { params: { path: { id } } });
      if (error) {
        toast.error(`Test failed: ${id}`, { description: JSON.stringify(error) });
      } else if (data?.ok) {
        toast.success(`${id} OK (${Math.round((data.latencyNanos ?? 0) / 1_000_000)} ms)`);
      } else {
        toast.error(`${id} unreachable`, { description: data?.error ?? "unknown" });
      }
    } finally {
      setTesting(null);
    }
  };

  if (error) return <ErrorState message="Failed to load targets" details={error} />;

  return (
    <section>
      <PageHeader title="Targets" />
      {!items ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : items.length === 0 ? (
        <EmptyState
          message="No targets registered"
          hint={<>Targets are added by PR — see <code>docs/operations/register-target.md</code>.</>}
        />
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Display Name</TableHead>
                <TableHead>Namespace</TableHead>
                <TableHead>Allowed Types</TableHead>
                <TableHead>Configured</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((t) => (
                <TableRow key={t.id}>
                  <TableCell className="font-medium">{t.id}</TableCell>
                  <TableCell>{t.displayName ?? t.id}</TableCell>
                  <TableCell className="text-muted-foreground">{t.namespace ?? "—"}</TableCell>
                  <TableCell>{(t.allowedTargetTypes ?? []).join(", ") || "—"}</TableCell>
                  <TableCell>
                    {t.configured ? (
                      <Badge className="bg-status-success/15 text-status-success" variant="outline">
                        configured
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="bg-status-failed/15 text-status-failed">
                        missing
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <Button variant="outline" size="sm" disabled={testing === t.id} onClick={() => testConn(t.id)}>
                      {testing === t.id ? "Testing…" : "Test"}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/pages/TargetsPage.tsx
git commit -m "feat(web): Targets page on Table with toast test-connection"
```

---

## Task 12: Migrate Schedules page (Dialog create + AlertDialog confirm)

**Files:**
- Modify: `controlplane/web/src/pages/SchedulesPage.tsx`

- [ ] **Step 1: Replace `src/pages/SchedulesPage.tsx`**

```tsx
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { PageHeader } from "@/components/PageHeader";
import { EmptyState } from "@/components/EmptyState";
import { ErrorState } from "@/components/ErrorState";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger,
} from "@/components/ui/dialog";
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, AlertDialogTrigger,
} from "@/components/ui/alert-dialog";

type Schedule = components["schemas"]["Schedule"];

export function SchedulesPage() {
  const [items, setItems] = useState<Schedule[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const [newId, setNewId] = useState("");
  const [newScenario, setNewScenario] = useState("");
  const [newTarget, setNewTarget] = useState("");
  const [newCron, setNewCron] = useState("");
  const [newTimezone, setNewTimezone] = useState("");

  const reload = () =>
    api.GET("/api/schedules", {}).then(({ data, error }) => {
      if (error) setError(error);
      else setItems(data?.items ?? []);
    });

  useEffect(() => {
    reload();
  }, []);

  const doPause = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/pause", { params: { path: { id } } });
      toast.success(`Paused ${id}`);
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doResume = async (id: string) => {
    setBusy(id);
    try {
      await api.POST("/api/schedules/{id}/resume", { params: { path: { id } } });
      toast.success(`Resumed ${id}`);
      await reload();
    } finally {
      setBusy(null);
    }
  };
  const doDelete = async (id: string) => {
    setBusy(id);
    try {
      await api.DELETE("/api/schedules/{id}", { params: { path: { id } } });
      toast.success(`Deleted ${id}`);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  const doCreate = async () => {
    if (!newId || !newScenario || !newCron) {
      toast.error("id, scenario, and cron are required");
      return;
    }
    setBusy("__create__");
    try {
      const body: components["schemas"]["CreateScheduleRequest"] = {
        id: newId,
        scenarioId: newScenario,
        cron: newCron,
        ...(newTarget ? { targetId: newTarget } : {}),
        ...(newTimezone ? { timezone: newTimezone } : {}),
      };
      const { error } = await api.POST("/api/schedules", { body });
      if (error) {
        toast.error("Create failed", { description: JSON.stringify(error) });
        return;
      }
      toast.success(`Created ${newId}`);
      setNewId(""); setNewScenario(""); setNewTarget(""); setNewCron(""); setNewTimezone("");
      setCreateOpen(false);
      await reload();
    } finally {
      setBusy(null);
    }
  };

  if (error) return <ErrorState message="Failed to load schedules" details={error} />;

  const createButton = (
    <Dialog open={createOpen} onOpenChange={setCreateOpen}>
      <DialogTrigger asChild>
        <Button size="sm">+ New schedule</Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New schedule</DialogTitle>
          <DialogDescription>Create a recurring CronWorkflow for a scenario.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-2">
          <Input placeholder="id (e.g. nightly-mysql)" value={newId} onChange={(e) => setNewId(e.target.value)} />
          <Input placeholder="scenario (e.g. mysql-pod-delete)" value={newScenario} onChange={(e) => setNewScenario(e.target.value)} />
          <Input placeholder="target (optional)" value={newTarget} onChange={(e) => setNewTarget(e.target.value)} />
          <Input placeholder="cron (e.g. 0 2 * * *)" value={newCron} onChange={(e) => setNewCron(e.target.value)} />
          <Input placeholder="timezone (e.g. Asia/Tokyo)" value={newTimezone} onChange={(e) => setNewTimezone(e.target.value)} />
        </div>
        <DialogFooter>
          <Button disabled={busy === "__create__"} onClick={doCreate}>
            {busy === "__create__" ? "Creating…" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );

  return (
    <section>
      <PageHeader title="Schedules" action={createButton} />
      {!items ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : items.length === 0 ? (
        <EmptyState message="No schedules yet" hint='Click "+ New schedule" to create one.' />
      ) : (
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Scenario</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Cron</TableHead>
                <TableHead>Last Fired</TableHead>
                <TableHead>Active</TableHead>
                <TableHead>Status</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className="font-mono text-xs">{s.id}</TableCell>
                  <TableCell>{s.scenario}</TableCell>
                  <TableCell>{s.target ?? "local"}</TableCell>
                  <TableCell className="font-mono text-xs">{s.cron}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {s.lastScheduledAt ? new Date(s.lastScheduledAt).toLocaleString() : "—"}
                  </TableCell>
                  <TableCell>{s.activeCount ?? 0}</TableCell>
                  <TableCell>
                    {s.suspended ? (
                      <Badge variant="outline" className="bg-status-pending/15 text-status-pending">paused</Badge>
                    ) : (
                      <Badge variant="outline" className="bg-status-success/15 text-status-success">active</Badge>
                    )}
                  </TableCell>
                  <TableCell className="space-x-1 whitespace-nowrap">
                    {s.suspended ? (
                      <Button variant="outline" size="sm" disabled={busy === s.id} onClick={() => doResume(s.id)}>
                        resume
                      </Button>
                    ) : (
                      <Button variant="outline" size="sm" disabled={busy === s.id} onClick={() => doPause(s.id)}>
                        pause
                      </Button>
                    )}
                    <AlertDialog>
                      <AlertDialogTrigger asChild>
                        <Button variant="outline" size="sm" disabled={busy === s.id} className="text-destructive">
                          delete
                        </Button>
                      </AlertDialogTrigger>
                      <AlertDialogContent>
                        <AlertDialogHeader>
                          <AlertDialogTitle>Delete schedule "{s.id}"?</AlertDialogTitle>
                          <AlertDialogDescription>
                            This removes the CronWorkflow. In-flight runs are not affected.
                          </AlertDialogDescription>
                        </AlertDialogHeader>
                        <AlertDialogFooter>
                          <AlertDialogCancel>Cancel</AlertDialogCancel>
                          <AlertDialogAction onClick={() => doDelete(s.id)}>Delete</AlertDialogAction>
                        </AlertDialogFooter>
                      </AlertDialogContent>
                    </AlertDialog>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Verify build**

Run: `pnpm build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add controlplane/web/src/pages/SchedulesPage.tsx
git commit -m "feat(web): Schedules page with Dialog create + AlertDialog confirm + toasts"
```

---

## Task 13: `parseVerdict` pure-logic module (TDD)

**Files:**
- Create: `controlplane/web/src/lib/verdict.ts`
- Test: `controlplane/web/src/lib/verdict.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, it, expect } from "vitest";
import { parseVerdict } from "@/lib/verdict";

describe("parseVerdict", () => {
  it("returns null for null/undefined", () => {
    expect(parseVerdict(null)).toBeNull();
    expect(parseVerdict(undefined)).toBeNull();
  });

  it("returns null when overall is not a boolean", () => {
    expect(parseVerdict({ thresholds: [] })).toBeNull();
  });

  it("parses overall and thresholds with lt bound", () => {
    const v = parseVerdict({
      overall: true,
      thresholds: [
        { metric: "p95-latency-chaos", value: 0.0000025, lt: 2.5, passed: true },
        { metric: "error-rate-recovery", value: 0.3, lt: 0.05, passed: false },
      ],
    });
    expect(v).not.toBeNull();
    expect(v!.overall).toBe(true);
    expect(v!.thresholds).toHaveLength(2);
    expect(v!.thresholds[0]).toEqual({
      metric: "p95-latency-chaos", value: 0.0000025, bound: "< 2.5", passed: true,
    });
  });

  it("formats a gt bound", () => {
    const v = parseVerdict({ overall: false, thresholds: [{ metric: "throughput", value: 50, gt: 100, passed: false }] });
    expect(v!.thresholds[0].bound).toBe("> 100");
  });

  it("uses '—' bound when neither lt nor gt present", () => {
    const v = parseVerdict({ overall: true, thresholds: [{ metric: "x", value: 1, passed: true }] });
    expect(v!.thresholds[0].bound).toBe("—");
  });

  it("extracts raw_promql when present", () => {
    const v = parseVerdict({
      overall: true, thresholds: [],
      raw_promql: "up == 1", raw_promql_value: 1, raw_promql_pass: true,
    });
    expect(v!.rawPromQL).toEqual({ query: "up == 1", value: 1, passed: true });
  });

  it("omits rawPromQL when raw_promql is empty", () => {
    const v = parseVerdict({ overall: true, thresholds: [] });
    expect(v!.rawPromQL).toBeUndefined();
  });

  it("tolerates a missing thresholds array", () => {
    const v = parseVerdict({ overall: true });
    expect(v!.thresholds).toEqual([]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm test`
Expected: FAIL — `parseVerdict` does not exist.

- [ ] **Step 3: Implement `src/lib/verdict.ts`**

```ts
export interface ParsedThreshold {
  metric: string;
  value: number;
  bound: string;
  passed: boolean;
}

export interface ParsedVerdict {
  overall: boolean;
  thresholds: ParsedThreshold[];
  rawPromQL?: { query: string; value: number; passed: boolean };
}

function num(v: unknown): number {
  return typeof v === "number" ? v : Number(v);
}

export function parseVerdict(raw: Record<string, unknown> | null | undefined): ParsedVerdict | null {
  if (!raw || typeof raw.overall !== "boolean") return null;

  const rawThresholds = Array.isArray(raw.thresholds) ? (raw.thresholds as Record<string, unknown>[]) : [];
  const thresholds: ParsedThreshold[] = rawThresholds.map((t) => {
    let bound = "—";
    if (typeof t.lt === "number") bound = `< ${t.lt}`;
    else if (typeof t.gt === "number") bound = `> ${t.gt}`;
    return {
      metric: String(t.metric ?? ""),
      value: num(t.value),
      bound,
      passed: Boolean(t.passed),
    };
  });

  const result: ParsedVerdict = { overall: raw.overall, thresholds };

  if (typeof raw.raw_promql === "string" && raw.raw_promql !== "") {
    result.rawPromQL = {
      query: raw.raw_promql,
      value: num(raw.raw_promql_value),
      passed: Boolean(raw.raw_promql_pass),
    };
  }

  return result;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm test`
Expected: PASS (all `stats.test.ts` + `verdict.test.ts` tests green).

- [ ] **Step 5: Commit**

```bash
git add controlplane/web/src/lib/verdict.ts controlplane/web/src/lib/verdict.test.ts
git commit -m "feat(web): parseVerdict module with Vitest coverage"
```

---

## Task 14: Migrate Run detail page + VerdictView

**Files:**
- Create: `controlplane/web/src/components/VerdictView.tsx`
- Modify: `controlplane/web/src/pages/RunDetailPage.tsx`

- [ ] **Step 1: Create `src/components/VerdictView.tsx`**

```tsx
import { CheckCircle2, XCircle } from "lucide-react";
import { parseVerdict } from "@/lib/verdict";
import { cn } from "@/lib/utils";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function VerdictView({ verdict }: { verdict: Record<string, unknown> | null | undefined }) {
  const parsed = parseVerdict(verdict);
  if (!parsed) {
    return <p className="text-sm text-muted-foreground">No verdict report yet.</p>;
  }
  return (
    <div className="space-y-4">
      <div
        className={cn(
          "flex items-center gap-2 rounded-lg border p-3 font-medium",
          parsed.overall
            ? "border-status-success/40 bg-status-success/10 text-status-success"
            : "border-status-failed/40 bg-status-failed/10 text-status-failed"
        )}
      >
        {parsed.overall ? <CheckCircle2 className="h-5 w-5" /> : <XCircle className="h-5 w-5" />}
        {parsed.overall ? "PASS" : "FAIL"}
      </div>

      {parsed.thresholds.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Metric</TableHead>
              <TableHead>Value</TableHead>
              <TableHead>Bound</TableHead>
              <TableHead>Result</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {parsed.thresholds.map((t) => (
              <TableRow key={t.metric}>
                <TableCell className="font-medium">{t.metric}</TableCell>
                <TableCell className="font-mono text-xs">{t.value}</TableCell>
                <TableCell className="font-mono text-xs">{t.bound}</TableCell>
                <TableCell className={t.passed ? "text-status-success" : "text-status-failed"}>
                  {t.passed ? "pass" : "fail"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {parsed.rawPromQL && (
        <div className="text-sm">
          <span className="text-muted-foreground">Raw PromQL: </span>
          <code className="font-mono text-xs">{parsed.rawPromQL.query}</code>{" "}
          <span className={parsed.rawPromQL.passed ? "text-status-success" : "text-status-failed"}>
            ({parsed.rawPromQL.passed ? "pass" : "fail"})
          </span>
        </div>
      )}

      <details className="text-sm text-muted-foreground">
        <summary className="cursor-pointer">View raw JSON</summary>
        <pre className="mt-2 overflow-auto rounded border bg-muted/40 p-3 text-xs">
          {JSON.stringify(verdict, null, 2)}
        </pre>
      </details>
    </div>
  );
}
```

- [ ] **Step 2: Replace `src/pages/RunDetailPage.tsx`**

```tsx
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { components } from "../api/gen";
import { StatusBadge } from "@/components/StatusBadge";
import { ErrorState } from "@/components/ErrorState";
import { VerdictView } from "@/components/VerdictView";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

type RunDetail = components["schemas"]["RunDetail"];

export function RunDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [run, setRun] = useState<RunDetail | null>(null);
  const [liveStatus, setLiveStatus] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    if (!id) return;
    api.GET("/api/runs/{id}", { params: { path: { id } } }).then(({ data, error }) => {
      if (error) setError(error);
      else setRun(data as RunDetail);
    });

    const es = new EventSource(`/api/runs/${id}/events`);
    const onEvent = (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        if (data.phase) setLiveStatus(data.phase);
      } catch {
        /* ignore */
      }
    };
    es.addEventListener("snapshot", onEvent);
    es.addEventListener("MODIFIED", onEvent);
    es.addEventListener("ADDED", onEvent);
    es.addEventListener("DELETED", onEvent);
    return () => es.close();
  }, [id]);

  if (error) return <ErrorState message="Failed to load run" details={error} />;
  if (!run) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
      </div>
    );
  }

  const status = liveStatus ?? String(run.status ?? "Unknown");

  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-center gap-3">
        <h1 className="text-xl font-semibold">{run.id}</h1>
        <StatusBadge status={status} />
        {run.target && <span className="text-xs text-muted-foreground">target: {run.target}</span>}
        {run.triggeredBy?.id && (
          <Link to="/schedules" className="text-xs text-primary hover:underline">
            Triggered by schedule: {run.triggeredBy.id}
          </Link>
        )}
      </header>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Scenario</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">{run.scenario}</p>
        </CardContent>
      </Card>

      {run.steps && run.steps.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Steps</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Step</TableHead>
                  <TableHead>Phase</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {run.steps.map((s, i) => (
                  <TableRow key={i}>
                    <TableCell>{s.name}</TableCell>
                    <TableCell className="text-muted-foreground">{s.phase}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Verdict</CardTitle>
        </CardHeader>
        <CardContent>
          <VerdictView verdict={run.verdict} />
        </CardContent>
      </Card>
    </section>
  );
}
```

- [ ] **Step 3: Verify build + full test run**

```bash
pnpm build
pnpm test
```

Expected: both PASS.

- [ ] **Step 4: Commit**

```bash
git add controlplane/web/src/components/VerdictView.tsx controlplane/web/src/pages/RunDetailPage.tsx
git commit -m "feat(web): Run detail with readable VerdictView + client-side schedule link"
```

---

## Task 15: Full-stack verification + docs

**Files:**
- Modify: `CLAUDE.md` (repo root) — add a short "controlplane UI refresh" note under the Phase F section.

- [ ] **Step 1: Full build of the Go binary (embeds the new UI)**

Run (from `controlplane/`):
```bash
make ui-build
make build
```
Expected: `make ui-build` runs `pnpm install --frozen-lockfile` (lockfile must already be committed — it is, from Task 1), `pnpm build`, and copies `web/dist` → `internal/api/dist`. `make build` then compiles `bin/dlh-controlplane` with the embedded UI. Both succeed.

- [ ] **Step 2: Run the Go test suite (must remain green)**

Run (from `controlplane/`): `go test ./...`
Expected: PASS — no Go code changed; this confirms the embed still compiles.

- [ ] **Step 3: Run the web test suite**

Run (from `controlplane/web`): `pnpm test`
Expected: PASS (`stats.test.ts` + `verdict.test.ts`).

- [ ] **Step 4: Manual end-to-end smoke (recommended)**

Reload the controlplane image into minikube and exercise the UI:
```bash
cd controlplane && make reload-minikube
kubectl -n dlh-test-fw rollout restart deployment/dlh-controlplane
kubectl -n dlh-test-fw rollout status deployment/dlh-controlplane --timeout=60s
kubectl -n dlh-test-fw port-forward svc/dlh-controlplane 18080:80
```
Open http://localhost:18080 and verify: dark theme by default; toggle to light persists on reload; Runs shows stat cards + live indicator; Scenarios run → toast + navigate; Schedules create dialog + delete confirm; Run detail shows the verdict table.

- [ ] **Step 5: Update root `CLAUDE.md`**

Add this bullet under the "Phase F additions (Plan 19)" section (or a new short "controlplane UI" subsection):

```markdown
### controlplane UI refresh

- `controlplane/web` uses **shadcn/ui** primitives (vendored in `src/components/ui/`)
  + a dark-default indigo theme with a light toggle (`src/lib/theme.tsx`,
  persisted under `localStorage["dlh-theme"]`).
- Runs is a dashboard landing: stat cards computed client-side
  (`src/lib/stats.ts`) + a polling (5s) runs table.
- Verdict rendering is `src/components/VerdictView.tsx` driven by
  `src/lib/verdict.ts` (parses the verdict-job `report.json` `overall` +
  `thresholds`). Pure logic in `src/lib/` is unit-tested with **Vitest**
  (`pnpm test`); everything else is gated by `pnpm build`.
- shadcn deps are pnpm-managed — adding components updates `pnpm-lock.yaml`,
  which MUST be committed (CI's `make ui-build` uses `--frozen-lockfile`).
```

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: note controlplane UI refresh conventions in CLAUDE.md"
```

- [ ] **Step 7: Merge to main (--no-ff per repo convention)**

```bash
cd /Users/allen/repo/dlh-test-fw
git checkout main
git merge --no-ff feat/controlplane-ui-refresh -m "Merge feat/controlplane-ui-refresh: shadcn UI refresh

Dashboard-forward Runs landing, dark-default theme + light toggle, indigo
accent, shadcn/ui primitives, toasts/dialogs replacing alert/confirm,
readable VerdictView, polling auto-refresh, client-side navigation.
Pure logic (computeStats, parseVerdict) covered by Vitest."
```
Then remove the worktree if one was used (`git worktree remove ../dlh-test-fw-ui-refresh`).

---

## Notes & deviations from the spec

- **Primitive set trimmed (YAGNI):** the spec listed Tooltip and DropdownMenu, but no described feature uses them — the theme toggle is a plain icon Button. They are omitted. Add later if a feature needs them.
- **No React Testing Library / jsdom:** the spec mentioned RTL, but the only unit-tested code is pure (`computeStats`, `parseVerdict`), so Vitest runs in the `node` environment with no DOM deps. Presentational components are covered by the build gate + manual smoke, as the spec's testing strategy intends.
- **Pure-logic phasing:** the spec put "extract pure logic + tests" in phase 8 (last). This plan builds each module **test-first immediately before the page that consumes it** (`computeStats` before Runs, `parseVerdict` before Run detail) — same outcome, proper TDD ordering.
- **Primitives are hand-vendored, not generated by the shadcn CLI.** This keeps execution deterministic and offline (no CLI/Tailwind-v3-vs-v4 drift). The files are the standard shadcn source.
- **No `components.json`.** The spec mentioned it, but it exists only to drive the shadcn CLI's `add` command. Since primitives are hand-vendored, the file serves no purpose and is omitted. Add it later if you want to pull additional components via the CLI.

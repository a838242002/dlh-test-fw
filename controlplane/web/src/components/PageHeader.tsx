import { type ReactNode } from "react";

export function PageHeader({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <div className="mb-6 flex items-center justify-between">
      <h1 className="text-xl font-semibold">{title}</h1>
      {action}
    </div>
  );
}

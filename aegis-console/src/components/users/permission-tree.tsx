"use client";

import { useState } from "react";
import { Check, ChevronDown, ChevronRight, X } from "lucide-react";
import type { PermissionGroup } from "@/lib/api/types";

type Props = {
  groups: PermissionGroup[];
  compact?: boolean;
};

export function PermissionTree({ groups, compact }: Props) {
  return (
    <div className="space-y-1">
      {groups.map((group) => (
        <PermissionGroupNode key={group.key} group={group} compact={compact} />
      ))}
    </div>
  );
}

function PermissionGroupNode({ group, compact }: { group: PermissionGroup; compact?: boolean }) {
  const [open, setOpen] = useState(!compact);
  const grantedCount = group.permissions.filter((p) => p.description === "granted").length;
  const totalCount = group.permissions.length;
  const allGranted = grantedCount === totalCount;

  return (
    <div>
      <button
        type="button"
        className="flex w-full items-center gap-1.5 rounded-md px-2 py-1 text-left text-xs transition-colors hover:bg-muted/50"
        onClick={() => setOpen(!open)}
      >
        {open ? <ChevronDown className="size-3 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3 shrink-0 text-muted-foreground" />}
        <span className="font-medium">{group.name}</span>
        <span className={`ml-auto text-[10px] tabular-nums ${allGranted ? "text-emerald-600 dark:text-emerald-400" : "text-muted-foreground"}`}>
          {grantedCount}/{totalCount}
        </span>
      </button>
      {open && (
        <div className="ml-5 space-y-0.5 py-0.5">
          {group.permissions.map((perm) => {
            const granted = perm.description === "granted";
            return (
              <div key={perm.code} className="flex items-center gap-1.5 px-2 py-0.5 text-[11px]">
                {granted ? (
                  <Check className="size-3 shrink-0 text-emerald-500" />
                ) : (
                  <X className="size-3 shrink-0 text-zinc-300 dark:text-zinc-600" />
                )}
                <span className={granted ? "text-foreground" : "text-muted-foreground"}>{perm.name}</span>
                <span className="ml-auto font-mono text-[9px] text-muted-foreground/50">{perm.code}</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

import { useId, type SVGProps } from "react";

type P = SVGProps<SVGSVGElement>;
const base = (p: P) => ({
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.6,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
  ...p,
});

export const IconChat = (p: P) => (
  <svg {...base(p)}>
    <path d="M20 11.5c0 4.14-3.8 7.5-8.5 7.5-1.2 0-2.35-.22-3.39-.62L4 20l1.3-3.47C4.48 15.36 4 13.98 4 12.5 4 8.36 7.8 5 12.5 5S20 7.36 20 11.5Z" />
    <path d="M9 12h.01M12.5 12h.01M16 12h.01" strokeWidth={2.2} />
  </svg>
);
export const IconBolt = (p: P) => (
  <svg {...base(p)}>
    <path d="M13 3 5 13.5h6L10 21l8-10.5h-6L13 3Z" />
  </svg>
);
export const IconPulse = (p: P) => (
  <svg {...base(p)}>
    <path d="M3 12h4l2.5-6 4 12 2.5-6H21" />
  </svg>
);
export const IconServer = (p: P) => (
  <svg {...base(p)}>
    <rect x="4" y="4" width="16" height="7" rx="2" />
    <rect x="4" y="13" width="16" height="7" rx="2" />
    <path d="M8 7.5h.01M8 16.5h.01" strokeWidth={2.4} />
  </svg>
);
export const IconMonitor = (p: P) => (
  <svg {...base(p)}>
    <rect x="3" y="4" width="18" height="12" rx="2" />
    <path d="M8 20h8M12 16v4" />
  </svg>
);
export const IconSend = (p: P) => (
  <svg {...base(p)} strokeWidth={2}>
    <path d="M12 19V5M5.5 11.5 12 5l6.5 6.5" />
  </svg>
);
export const IconStop = (p: P) => (
  <svg {...base(p)}>
    <rect x="7" y="7" width="10" height="10" rx="2" fill="currentColor" stroke="none" />
  </svg>
);
export const IconPlus = (p: P) => (
  <svg {...base(p)}>
    <path d="M12 5v14M5 12h14" />
  </svg>
);
export const IconBack = (p: P) => (
  <svg {...base(p)}>
    <path d="M15 5 8 12l7 7" />
  </svg>
);
export const IconLogout = (p: P) => (
  <svg {...base(p)}>
    <path d="M14 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4M9 16l-4-4 4-4M5 12h11" />
  </svg>
);
export const IconAlert = (p: P) => (
  <svg {...base(p)}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 8v4.5M12 16h.01" strokeWidth={2} />
  </svg>
);
export const IconSpark = (p: P) => (
  <svg {...base(p)}>
    <path d="M12 3c.6 4.2 2.8 6.4 7 7-4.2.6-6.4 2.8-7 7-.6-4.2-2.8-6.4-7-7 4.2-.6 6.4-2.8 7-7Z" />
  </svg>
);
export const IconCode = (p: P) => (
  <svg {...base(p)}>
    <path d="m8 8-4 4 4 4M16 8l4 4-4 4M13.5 5l-3 14" />
  </svg>
);
export const IconEye = (p: P) => (
  <svg {...base(p)}>
    <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
    <circle cx="12" cy="12" r="2.8" />
  </svg>
);
export const IconPen = (p: P) => (
  <svg {...base(p)}>
    <path d="M4 20h4L19 9a2.83 2.83 0 0 0-4-4L4 16v4Z" />
  </svg>
);
export const IconRefresh = (p: P) => (
  <svg {...base(p)}>
    <path d="M20 11a8 8 0 1 0-2.34 5.66M20 5v6h-6" />
  </svg>
);

// Brand mark: the Avora "A", lit from above.
// Each instance gets its own gradient id: the sidebar copy is display:none on
// phones, and a shared id would resolve to that hidden gradient and paint nothing.
export function BrandMark(p: P) {
  const id = `bm${useId().replace(/:/g, "")}`;
  return (
    <svg viewBox="0 0 64 64" aria-hidden {...p}>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#fff" />
          <stop offset=".6" stopColor="#cfcfd6" />
          <stop offset="1" stopColor="#7a7a84" />
        </linearGradient>
      </defs>
      <path fill={`url(#${id})`} d="M32 6 50 56h-8.6l-4-11.4H26.6L22.6 56H14L32 6Zm-3.1 31.2h6.2L32 27.6l-3.1 9.6Z" />
    </svg>
  );
}

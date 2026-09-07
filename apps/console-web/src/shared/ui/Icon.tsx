type IconName = "refresh" | "menu" | "close" | "chevron" | "external" | "check" | "circle";

const paths: Record<IconName, React.ReactNode> = {
  refresh: (
    <>
      <path d="M20 11a8.1 8.1 0 0 0-14.9-4L3 10" />
      <path d="M3 4v6h6" />
      <path d="M4 13a8.1 8.1 0 0 0 14.9 4L21 14" />
      <path d="M15 14h6v6" />
    </>
  ),
  menu: (
    <>
      <path d="M4 7h16" />
      <path d="M4 12h16" />
      <path d="M4 17h16" />
    </>
  ),
  close: (
    <>
      <path d="m6 6 12 12" />
      <path d="m18 6-12 12" />
    </>
  ),
  chevron: <path d="m9 18 6-6-6-6" />,
  external: (
    <>
      <path d="M15 3h6v6" />
      <path d="m10 14 11-11" />
      <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
    </>
  ),
  check: <path d="m5 12 4 4L19 6" />,
  circle: <circle cx="12" cy="12" r="8" />,
};

export function Icon({ name, size = 20 }: { name: IconName; size?: number }) {
  return (
    <svg
      className="icon-svg"
      aria-hidden="true"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths[name]}
    </svg>
  );
}

import { Check, ChevronRight, Circle, ExternalLink, type LucideIcon, Menu, RefreshCw, X } from "lucide-react";

type IconName = "refresh" | "menu" | "close" | "chevron" | "external" | "check" | "circle";

const icons: Record<IconName, LucideIcon> = {
  refresh: RefreshCw,
  menu: Menu,
  close: X,
  chevron: ChevronRight,
  external: ExternalLink,
  check: Check,
  circle: Circle,
};

export function Icon({ name, size = 20 }: { name: IconName; size?: number }) {
  const Component = icons[name];
  return <Component aria-hidden="true" className="icon-svg" size={size} strokeWidth={2} />;
}

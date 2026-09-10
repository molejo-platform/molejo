type BrandLogoProps = {
  compact?: boolean;
  surface: "light" | "dark";
};

export function BrandLogo({ compact = false, surface }: BrandLogoProps) {
  const asset = compact ? `molejo-symbol-on-${surface}.webp` : `molejo-horizontal-on-${surface}.webp`;
  const dimensions = compact ? { width: 180, height: 99 } : { width: 420, height: 90 };
  return (
    <img
      className={`brand-logo ${compact ? "brand-logo-compact" : "brand-logo-full"}`}
      src={`/brand/${asset}`}
      alt="Molejo"
      width={dimensions.width}
      height={dimensions.height}
    />
  );
}

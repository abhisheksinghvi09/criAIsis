/** The criAIsis mark: four specialist arcs converging on a single commander point.
 *
 * Drawn for this product rather than borrowed from the reference design, whose
 * logo belongs to another brand.
 */
export function Mark({ size = 26 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" aria-hidden="true">
      <circle cx="16" cy="16" r="14" stroke="currentColor" strokeOpacity="0.28" strokeWidth="1.4" />
      {/* Four domains, each entering from its own quadrant */}
      <path d="M16 4.5 L16 12.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M27.5 16 L19.5 16" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M16 27.5 L16 19.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M4.5 16 L12.5 16" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      {/* The commander's verdict */}
      <circle cx="16" cy="16" r="3.2" fill="currentColor" />
    </svg>
  );
}

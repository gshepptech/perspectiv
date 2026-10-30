/** "Canon", or "Canon: <lore name>" with the name in italics once its members have named it. */
export function CanonName({ name }: { name: string }) {
  return name ? (
    <span className="wrap-anywhere">
      Canon: <i className="font-serif">{name}</i>
    </span>
  ) : (
    <>Canon</>
  );
}

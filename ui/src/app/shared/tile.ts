/**
 * Tiles that link as a whole but hold links of their own. Links must not
 * nest, so the tile's own link is stretched over it (an ::after that covers
 * the tile) and the links inside sit above that. Hover and keyboard focus
 * show on the tile, as the brand's panels take them.
 */

/** The tile: a panel, highlighted while hovered or while its own link has focus. */
export const TILE =
  'mp-panel mp-panel-flush relative text-fg hover:border-line-strong hover:bg-surface-2 has-[.tile-main:focus-visible]:outline-2 has-[.tile-main:focus-visible]:outline-offset-2 has-[.tile-main:focus-visible]:outline-signal';

/** The tile's own link, stretched over the whole tile. */
export const TILE_MAIN =
  'tile-main text-inherit no-underline outline-none after:absolute after:inset-0 after:content-[""]';

/**
 * A link that looks like the text it is, until it is hovered or focused: for
 * counts and names. Above a tile's own link, so it can be clicked inside one.
 */
export const QUIET_LINK =
  'relative z-10 rounded-sm text-inherit no-underline decoration-1 underline-offset-2 hover:underline focus-visible:underline focus-visible:outline-2 focus-visible:outline-signal';

/** A tile that is one link and holds no others: a contributor chip. */
export const TILE_LINK =
  'mp-panel mp-panel-flush text-fg no-underline hover:border-line-strong hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-signal';

/** Every link on a page, decoded, so that a test reads them as written. */
export function hrefs(el: HTMLElement): string[] {
  return Array.from(el.querySelectorAll('a[href]')).map((a) =>
    decodeURIComponent(a.getAttribute('href') ?? ''),
  );
}

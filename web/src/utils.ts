/**
 * Safely escapes characters for inclusion in HTML text nodes and quoted attributes.
 * Escapes &, <, >, ", and ' to prevent HTML and attribute injection XSS.
 */
export function escapeHtml(str: unknown): string {
  if (str === null || str === undefined) {
    return '';
  }
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

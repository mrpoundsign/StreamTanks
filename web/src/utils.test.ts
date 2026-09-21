import { describe, it, expect } from 'vitest';
import { escapeHtml } from './utils';

describe('escapeHtml', () => {
  it('escapes standard HTML special characters', () => {
    expect(escapeHtml('<script>alert("xss")</script>')).toBe(
      '&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt;'
    );
  });

  it('escapes single and double quotes to prevent attribute breakout', () => {
    const input = `x" onmouseover="alert(1)" ' onclick='alert(2)'`;
    const escaped = escapeHtml(input);
    expect(escaped).toBe(
      'x&quot; onmouseover=&quot;alert(1)&quot; &#39; onclick=&#39;alert(2)&#39;'
    );
    expect(escaped.includes('"')).toBe(false);
    expect(escaped.includes("'")).toBe(false);
  });

  it('escapes ampersands without double-escaping', () => {
    expect(escapeHtml('Salt & Pepper')).toBe('Salt &amp; Pepper');
    expect(escapeHtml('&amp;')).toBe('&amp;amp;');
  });

  it('handles null, undefined, and non-string values gracefully', () => {
    expect(escapeHtml(null)).toBe('');
    expect(escapeHtml(undefined)).toBe('');
    expect(escapeHtml(0)).toBe('0');
    expect(escapeHtml(123)).toBe('123');
    expect(escapeHtml(false)).toBe('false');
  });
});

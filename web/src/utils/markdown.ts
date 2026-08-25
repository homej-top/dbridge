import MarkdownIt from 'markdown-it';

export const md = new MarkdownIt({ breaks: true, html: false });

/** Pre-process LLM-generated markdown to fix common formatting issues. */
export function sanitizeMarkdown(text: string): string {
  if (!text) return '';
  let t = text;
  // 1. Fix headers: ##text → ## text, and add newline before inline headers
  t = t.replace(/^(#{1,6})([^\s#])/gm, '$1 $2');
  t = t.replace(/([^\n])(#{1,6}\s)/g, '$1\n$2');
  // 2. Fix concatenated table rows: |cellA|cellB||nextRowCell| → separate rows
  t = t.replace(/\|([^|]+)\|\|([^|]+)\|/g, '|$1|\n|$2|');
  // 3. Ensure blank line before table blocks
  t = t.replace(/([^\n|])\n(\|[^\n]+\|)/g, '$1\n\n$2');
  // 4. Normalize table cell spacing
  t = t.replace(/^\|(.+)\|$/gm, (line) => {
    const cells = line.slice(1, -1).split('|');
    return '| ' + cells.map(c => c.trim()).join(' | ') + ' |';
  });
  return t;
}

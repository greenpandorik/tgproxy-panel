// Helpers for the TemplateEditorPage live preview iframe. Mirrors the
// inlining logic in internal/api/sites.go handleSitePreview - the srcdoc is
// built client-side so the preview updates without a round trip, while the
// deployed-on-a-node preview (NodeSiteTab) still uses the real endpoint.

/** Decodes a base64 asset value (as stored in the `assets` map) to a UTF-8 string. */
export function base64ToText(b64: string): string {
  try {
    const binary = atob(b64);
    const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return '';
  }
}

/** Encodes a UTF-8 string to base64 (used when reading an uploaded text file back for the assets map). */
export function textToBase64(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary);
}

/** Reads a File as a base64 string (no `data:...;base64,` prefix). */
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error('read failed'));
    reader.onload = () => {
      const result = String(reader.result ?? '');
      const comma = result.indexOf(',');
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.readAsDataURL(file);
  });
}

/** Approximate decoded byte size of a base64 string, for the assets list. */
export function base64ByteSize(b64: string): number {
  const len = b64.length;
  if (len === 0) return 0;
  const padding = b64.endsWith('==') ? 2 : b64.endsWith('=') ? 1 : 0;
  return Math.max(0, Math.floor((len * 3) / 4) - padding);
}

/** Builds the iframe `srcdoc` by inlining every `.css` asset referenced as a stylesheet `<link>`, same as the server-side preview endpoint. */
export function buildPreviewSrcdoc(html: string, assets: Record<string, string>): string {
  let page = html;
  for (const [path, b64] of Object.entries(assets)) {
    if (!path.endsWith('.css')) continue;
    const css = base64ToText(b64);
    const style = `<style>${css}</style>`;
    page = page.replace(`<link rel="stylesheet" href="/${path}"/>`, style).replace(`<link rel="stylesheet" href="/${path}">`, style);
  }
  return page;
}

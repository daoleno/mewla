import { describe, expect, test } from "bun:test";
import { MAX_SVG_BYTES, svgPreviewHtml, validateSvgPreview } from "./svgPreview";

const wrap = (body: string) => `<svg viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg">${body}</svg>`;

describe("SVG preview", () => {
  test("bounded vectors keep their geometry and their markup", () => {
    const xml = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 4181 1472"><path fill="white" d="M0 0h4181v1472H0z"/></svg>`;
    const preview = validateSvgPreview(xml);
    expect(preview.ratio).toBeCloseTo(4181 / 1472);
    expect(preview.xml).toBe(xml);
    expect(validateSvgPreview(`<?xml version="1.0"?>\n<!-- logo --><svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><g><text>a &amp; b</text></g></svg>`).ratio).toBe(2);
    expect(validateSvgPreview(wrap(`<defs><linearGradient id="fade"><stop offset="0" stop-color="white"/></linearGradient></defs><path fill="url(#fade)" d="M0 0"/>`)).ratio).toBe(1);
    expect(validateSvgPreview(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 2 1"><defs><path id="p" d="M0 0"/></defs><use xlink:href="#p"/></svg>`).ratio).toBe(2);
  });

  test("malformed markup, scripts, styles and anything outside the file are refused with the old words", () => {
    expect(() => validateSvgPreview(wrap(`<path d="M0 0"`))).toThrow();
    expect(() => validateSvgPreview(wrap(`<g><path d="M0 0"/>`))).toThrow();
    expect(() => validateSvgPreview(`<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///secret">]>${wrap("<path d='M0 0'/>")}`)).toThrow("SVG document declarations are unavailable.");
    expect(() => validateSvgPreview(wrap(`<text>&x;</text>`))).toThrow();
    expect(() => validateSvgPreview(wrap(`<image href="https://example.test/tracker"/>`))).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(wrap(`<use href="https://example.test/sprite.svg#a"/>`))).toThrow("External SVG references are unavailable.");
    expect(() => validateSvgPreview(wrap(`<foreignObject><script>bad()</script></foreignObject>`))).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(wrap(`<path onclick="bad()" d="M0 0"/>`))).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(wrap(`<path style="fill: red" d="M0 0"/>`))).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(wrap(`<path fill="url(https://example.test/track)" d="M0 0"/>`))).toThrow("External SVG references are unavailable.");
    expect(() => validateSvgPreview(`${wrap(`<path d="M0 0"/>`)}${wrap(`<path d="M0 0"/>`)}`)).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(`${wrap(`<path d="M0 0"/>`)}<?xml-stylesheet href="https://example.test/track"?>`)).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(`<svg viewBox="0 0 1 1"><path d="M0 0"/></svg>`)).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(wrap(`<![CDATA[x]]>`))).toThrow("Unsupported SVG content.");
  });

  test("size, nodes, depth and area are bounded", () => {
    expect(() => validateSvgPreview(wrap(`<!--${"x".repeat(MAX_SVG_BYTES)}-->`))).toThrow("SVG exceeds the preview limit.");
    expect(() => validateSvgPreview(wrap(`<path d="M0 0"/>`.repeat(10_001)))).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(wrap(`${"<g>".repeat(64)}${"</g>".repeat(64)}`))).toThrow("Unsupported SVG content.");
    expect(() => validateSvgPreview(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10000 10000"><path d="M0 0"/></svg>`)).toThrow("SVG dimensions are unavailable or exceed the preview limit.");
    expect(() => validateSvgPreview(`<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0"/></svg>`)).toThrow("SVG dimensions are unavailable or exceed the preview limit.");
  });

  test("the page that draws it runs nothing and loads nothing", () => {
    const html = svgPreviewHtml(validateSvgPreview(`<?xml version="1.0"?>${wrap(`<path d="M0 0"/>`)}`), "#fff");
    expect(html).toContain(`content="default-src 'none'; style-src 'unsafe-inline'"`);
    expect(html).not.toContain("<?xml");
    expect(html).toContain(wrap(`<path d="M0 0"/>`));
  });
});

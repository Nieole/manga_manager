import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { downloadBookFile } from './download';

// The vitest env is "node" (no DOM). downloadBookFile only builds a URL and drives a
// throwaway <a> element, so we stub a tiny document that records what the helper does.
interface FakeAnchor {
  tag: string;
  href: string;
  style: { display?: string };
  clicked: number;
  removed: boolean;
  click: () => void;
  remove: () => void;
}

let created: FakeAnchor | null;
let appended: FakeAnchor[];

beforeEach(() => {
  created = null;
  appended = [];
  (globalThis as unknown as { document: unknown }).document = {
    createElement: (tag: string): FakeAnchor => {
      const anchor: FakeAnchor = {
        tag,
        href: '',
        style: {},
        clicked: 0,
        removed: false,
        click() {
          this.clicked += 1;
        },
        remove() {
          this.removed = true;
        },
      };
      created = anchor;
      return anchor;
    },
    body: {
      appendChild: (el: FakeAnchor) => {
        appended.push(el);
      },
    },
  };
});

afterEach(() => {
  delete (globalThis as unknown as { document?: unknown }).document;
});

describe('downloadBookFile', () => {
  it('builds the /api/books/{id}/file URL and triggers a hidden-anchor download', () => {
    downloadBookFile(123);
    expect(created).not.toBeNull();
    expect(created!.tag).toBe('a');
    expect(created!.href).toBe('/api/books/123/file');
    expect(created!.style.display).toBe('none');
    // appended to body, clicked exactly once, then removed
    expect(appended).toContain(created);
    expect(created!.clicked).toBe(1);
    expect(created!.removed).toBe(true);
  });

  it('interpolates the given book id into the path', () => {
    downloadBookFile(0);
    expect(created!.href).toBe('/api/books/0/file');
    downloadBookFile(98765);
    expect(created!.href).toBe('/api/books/98765/file');
  });
});

import type { Config } from './types';
import { acceptSectionSave, applySectionSnapshot, pickSectionSnapshot, SECTION_FIELD_PATHS, type EditorSectionKey } from './sections';

const equalSection = (a: Config, b: Config, section: EditorSectionKey) =>
  JSON.stringify(pickSectionSnapshot(a, section)) === JSON.stringify(pickSectionSnapshot(b, section));

// A section retains the server generation on which its unsaved intent was based.
export class EditSession {
  draft: Config | null = null;
  server: Config | null = null;
  private bases: Partial<Record<EditorSectionKey, Config>> = {};
  private versions: Partial<Record<EditorSectionKey, string>> = {};
  private conflicts = new Set<EditorSectionKey>();
  private serverVersion = '';

  edit(next: Config | null) { this.draft = next; }

  dirty(section: EditorSectionKey): boolean {
    const base = this.bases[section];
    return !!(this.draft && base && !equalSection(this.draft, base, section));
  }

  conflicted(section: EditorSectionKey): boolean { return this.conflicts.has(section); }

  prepare(section: EditorSectionKey) {
    if (!this.draft || !this.server) return null;
    return {
      section,
      sent: this.draft,
      payload: applySectionSnapshot(this.server, this.draft, section),
      version: this.versions[section] || '',
    };
  }

  receive(config: Config, version: string) {
    let next = this.draft || config;
    for (const section of Object.keys(SECTION_FIELD_PATHS) as EditorSectionKey[]) {
      const base = this.bases[section];
      if (!this.dirty(section)) {
        next = applySectionSnapshot(next, config, section);
        this.bases[section] = config;
        this.versions[section] = version;
        this.conflicts.delete(section);
      } else if (base && !equalSection(base, config, section)) {
        this.conflicts.add(section);
      }
    }
    this.server = config;
    this.serverVersion = version;
    this.draft = next;
  }

  acknowledge(section: EditorSectionKey, sent: Config, config: Config, version: string) {
    this.draft = acceptSectionSave(this.draft || config, sent, config, section);
    this.bases[section] = config;
    this.versions[section] = version;
    this.conflicts.delete(section);
    this.server = config;
    this.serverVersion = version;
  }

  reject(section: EditorSectionKey) { this.conflicts.add(section); }

  discard(section: EditorSectionKey) {
    if (!this.server || !this.draft) return;
    this.draft = applySectionSnapshot(this.draft, this.server, section);
    this.bases[section] = this.server;
    this.versions[section] = this.serverVersion;
    this.conflicts.delete(section);
  }
}

import type { ReactNode } from 'react';
import { SettingsEditorProvider } from './SettingsEditorProvider';
import { KOReaderProvider } from '../koreader-settings/KOReaderProvider';
import { ConnectionTestingProvider } from '../settings-actions/ConnectionTestingProvider';

export function SettingsProvider({ children }: { children: ReactNode }) {
  return <SettingsEditorProvider><KOReaderProvider><ConnectionTestingProvider>{children}</ConnectionTestingProvider></KOReaderProvider></SettingsEditorProvider>;
}

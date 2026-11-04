// @vitest-environment jsdom
import type { ComponentProps } from 'react';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as chinese } from '../../i18n/locales/zh-CN';
import { messages as english } from '../../i18n/locales/en-US';
import { LibraryFormModal } from './LibraryFormModal';

afterEach(cleanup);

function setup(locale: 'zh-CN' | 'en-US', overrides: Partial<ComponentProps<typeof LibraryFormModal>> = {}) {
  const messages = locale === 'zh-CN' ? chinese : english;
  const props: ComponentProps<typeof LibraryFormModal> = {
    title: 'Library settings', submitLabel: 'Save library', submittingLabel: 'Saving library',
    open: true, name: 'Science fiction', path: '/books/ScienceFictionAndFantasyCompleteCollection',
    scanMode: 'none', koreaderSyncEnabled: false, scanInterval: 15, scanFormats: 'cbz',
    submitting: false, browsing: false, browseCurrent: '/books', browseParent: '/',
    browseDirs: [], browseDrives: [], recentPaths: [], supportedScanFormats: 'cbz, zip',
    onClose: vi.fn(), onSubmit: vi.fn((event) => event.preventDefault()),
    onNameChange: vi.fn(), onPathChange: vi.fn(), onScanModeChange: vi.fn(),
    onKOReaderSyncEnabledChange: vi.fn(), onScanIntervalChange: vi.fn(), onScanFormatsChange: vi.fn(),
    onOpenDirectoryBrowser: vi.fn(), onCloseDirectoryBrowser: vi.fn(),
    onChooseCurrentDirectory: vi.fn(), onNavigateDirectory: vi.fn(),
    ...overrides,
  };
  render(
    <LocaleProvider initialLocale={locale} initialMessages={messages} fallbackMessages={chinese}>
      <LibraryFormModal {...props} />
    </LocaleProvider>,
  );
  return { props, messages };
}

it.each(['zh-CN', 'en-US'] as const)('allows the actual directory input to shrink without shrinking Browse (%s)', (locale) => {
  const { props, messages } = setup(locale);
  const dialog = screen.getByRole('dialog', { name: props.title });
  const path = within(dialog).getByPlaceholderText(messages['directoryPicker.placeholder']);
  const browse = within(dialog).getByRole('button', { name: messages['directoryPicker.browse'] });
  expect(path.getAttribute('required')).not.toBeNull();
  expect(path.classList.contains('flex-1')).toBe(true);
  expect(path.parentElement?.classList.contains('flex')).toBe(true);
  expect(path.nextElementSibling).toBe(browse);
  // CSS contracts only: jsdom does not lay out the actual modal and picker.
  expect(path.classList.contains('min-w-0')).toBe(true);
  expect(browse.classList.contains('shrink-0')).toBe(true);
});

it.each(['zh-CN', 'en-US'] as const)('keeps path editing, browsing and the external form submit wired (%s)', (locale) => {
  const { props, messages } = setup(locale);
  fireEvent.change(screen.getByPlaceholderText(messages['directoryPicker.placeholder']), { target: { value: '/books/new' } });
  expect(props.onPathChange).toHaveBeenCalledWith('/books/new');
  const browse = screen.getByRole('button', { name: messages['directoryPicker.browse'] });
  expect(browse.getAttribute('type')).toBe('button');
  fireEvent.click(browse);
  expect(props.onOpenDirectoryBrowser).toHaveBeenCalledTimes(1);
  expect(props.onSubmit).not.toHaveBeenCalled();
  const submit = screen.getByRole('button', { name: props.submitLabel });
  expect(submit.getAttribute('type')).toBe('submit');
  expect(submit.getAttribute('form')).toBe('library-form-modal');
  fireEvent.click(submit);
  expect(props.onSubmit).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole('button', { name: messages['modal.cancel'] }));
  expect(props.onClose).toHaveBeenCalledTimes(1);
});

it('keeps submission disabled while saving', () => {
  const { props } = setup('en-US', { submitting: true });
  expect((screen.getByRole('button', { name: props.submittingLabel }) as HTMLButtonElement).disabled).toBe(true);
});

it('does not mount a closed modal', () => {
  setup('en-US', { open: false });
  expect(screen.queryByRole('dialog')).toBeNull();
});

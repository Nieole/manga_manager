// @vitest-environment jsdom
import type { ComponentProps } from 'react';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider } from '../../i18n/LocaleProvider';
import { messages as chinese } from '../../i18n/locales/zh-CN';
import { messages as english } from '../../i18n/locales/en-US';
import { SearchModal } from './SearchModal';

afterEach(cleanup);

function setup(locale: 'zh-CN' | 'en-US', overrides: Partial<ComponentProps<typeof SearchModal>> = {}) {
  const messages = locale === 'zh-CN' ? chinese : english;
  const props: ComponentProps<typeof SearchModal> = {
    open: true, searchQuery: 'ScienceFictionAndFantasyCompleteCollection',
    searchResults: [{ id: 's_7', fields: { type: 'series', title: 'Science fiction' }, score: 0.8 }],
    selectedIndex: 0, searchTarget: 'all',
    onClose: vi.fn(), onSearchQueryChange: vi.fn(), onSearchKeyDown: vi.fn(),
    onResetSearch: vi.fn(), onSearchTargetChange: vi.fn(), onSelectResult: vi.fn(), onHighlightIndex: vi.fn(),
    ...overrides,
  };
  render(
    <LocaleProvider initialLocale={locale} initialMessages={messages} fallbackMessages={chinese}>
      <SearchModal {...props} />
    </LocaleProvider>,
  );
  return { props, messages };
}

it.each(['zh-CN', 'en-US'] as const)('lets the search input shrink while keeping the icon and clear action fixed (%s)', (locale) => {
  const { messages } = setup(locale);
  const dialog = screen.getByRole('dialog', { name: messages['searchModal.title'] });
  const input = within(dialog).getByRole('textbox');
  const row = input.parentElement!;
  expect(input.classList.contains('flex-1')).toBe(true);
  expect(input.classList.contains('min-w-0')).toBe(true);
  expect(input.previousElementSibling?.classList.contains('shrink-0')).toBe(true);
  expect(within(row).getByRole('button').classList.contains('shrink-0')).toBe(true);
});

it.each(['zh-CN', 'en-US'] as const)('preserves typing, keyboard navigation, reset, scope and result callbacks (%s)', (locale) => {
  const { props, messages } = setup(locale);
  const input = screen.getByRole('textbox');
  fireEvent.change(input, { target: { value: 'new query' } });
  expect(props.onSearchQueryChange).toHaveBeenCalledWith('new query');
  fireEvent.keyDown(input, { key: 'ArrowDown' });
  expect(props.onSearchKeyDown).toHaveBeenCalledTimes(1);
  fireEvent.click(within(input.parentElement!).getByRole('button'));
  expect(props.onResetSearch).toHaveBeenCalledTimes(1);
  fireEvent.click(screen.getByRole('button', { name: messages['searchModal.scope.series'] }));
  expect(props.onSearchTargetChange).toHaveBeenCalledWith('series');
  fireEvent.mouseEnter(screen.getByText('Science fiction'));
  expect(props.onHighlightIndex).toHaveBeenCalledWith(0);
  fireEvent.click(screen.getByText('Science fiction'));
  expect(props.onSelectResult).toHaveBeenCalledWith(props.searchResults[0]);
  expect(props.onClose).not.toHaveBeenCalled();
  fireEvent.keyDown(input, { key: 'Escape' });
  expect(props.onClose).toHaveBeenCalledTimes(1);
});

it('omits clear for an empty query without removing search input', () => {
  setup('en-US', { searchQuery: '' });
  const input = screen.getByRole('textbox');
  expect(within(input.parentElement!).queryByRole('button')).toBeNull();
});

it('does not mount a closed modal', () => {
  setup('en-US', { open: false });
  expect(screen.queryByRole('dialog')).toBeNull();
});

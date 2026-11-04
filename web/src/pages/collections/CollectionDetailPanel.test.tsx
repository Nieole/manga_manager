// @vitest-environment jsdom
import type { ComponentProps } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { LocaleProvider, useI18n } from '../../i18n/LocaleProvider';
import { messages as chinese } from '../../i18n/locales/zh-CN';
import { messages as english } from '../../i18n/locales/en-US';
import { CollectionDetailPanel } from './CollectionDetailPanel';

type Props = Omit<ComponentProps<typeof CollectionDetailPanel>, 't'>;

function LocalizedPanel(props: Props) {
  const { t } = useI18n();
  return <CollectionDetailPanel {...props} t={t} />;
}

afterEach(cleanup);

function setup(locale: 'zh-CN' | 'en-US', name: string, overrides: Partial<Props> = {}) {
  const messages = locale === 'zh-CN' ? chinese : english;
  const props: Props = {
    selected: { view_id: 'smart:7', kind: 'smart', id: 7, numeric_id: 7, name,
      description: '', series_count: 1, source_type: 'smart_filter', created_at: '2026-01-01T00:00:00Z' },
    seriesItems: [{ series_id: 201, series_name: 'Member series', cover_path: { String: '', Valid: false }, book_count: 1 }],
    seriesTotal: 1, hasMore: false, loading: false, canManage: true,
    onLoadMore: vi.fn(), onEditCollection: vi.fn(), onOpenSmartEdit: vi.fn(), onOpenSnapshot: vi.fn(),
    onRemoveSeries: vi.fn(), onOpenSeries: vi.fn(), ...overrides,
  };
  const view = render(
    <LocaleProvider initialLocale={locale} initialMessages={messages} fallbackMessages={chinese}>
      <LocalizedPanel {...props} />
    </LocaleProvider>,
  );
  return { ...view, props, messages };
}

const names = [
  'ScienceFictionAndFantasyCompleteCollection',
  'Science Fiction and Fantasy: The Complete Illustrated Collection',
  '科幻与奇幻经典作品完整珍藏合集以及精选特别篇',
];

it.each((['zh-CN', 'en-US'] as const).flatMap(locale => names.map(name => ({ locale, name }))))(
  'constrains the complete title shrink chain while preserving badges and actions ($locale: $name)',
  ({ locale, name }) => {
    const { container, messages } = setup(locale, name);
    const heading = screen.getByRole('heading', { level: 2, name });
    expect(heading.getAttribute('title')).toBe(name);
    expect(heading.classList.contains('truncate')).toBe(true);
    expect(heading.classList.contains('min-w-0')).toBe(true);
    const titleRow = heading.parentElement!;
    const titleGroup = titleRow.parentElement!;
    expect(titleRow.classList.contains('min-w-0')).toBe(true);
    expect(titleGroup.classList.contains('min-w-0')).toBe(true);
    expect(titleGroup.classList.contains('flex-1')).toBe(true);
    expect(titleGroup.classList.contains('basis-48')).toBe(true);
    expect(titleGroup.parentElement?.classList.contains('flex-wrap')).toBe(true);
    expect(container.firstElementChild?.classList.contains('min-w-0')).toBe(true);
    const source = screen.getByText(messages['collections.source.smart_filter']);
    const count = screen.getByText(messages['common.seriesCount'].replace('{{count}}', '1'));
    for (const badge of [source, count]) {
      expect(badge.classList.contains('shrink-0')).toBe(true);
      expect(badge.classList.contains('whitespace-nowrap')).toBe(true);
    }
    for (const action of [messages['common.edit'], messages['collections.snapshot']]) {
      expect(screen.getByRole('button', { name: action }).classList.contains('shrink-0')).toBe(true);
    }
  },
);

it('retains smart edit, snapshot and series navigation callbacks', () => {
  const { props, messages } = setup('en-US', names[0]);
  fireEvent.click(screen.getByRole('button', { name: messages['common.edit'] }));
  fireEvent.click(screen.getByRole('button', { name: messages['collections.snapshot'] }));
  fireEvent.click(screen.getByText('Member series'));
  expect(props.onOpenSmartEdit).toHaveBeenCalledTimes(1);
  expect(props.onOpenSnapshot).toHaveBeenCalledTimes(1);
  expect(props.onOpenSeries).toHaveBeenCalledWith(201);
  expect(props.onEditCollection).not.toHaveBeenCalled();
});

it('retains manual edit and member removal without opening the removed series', () => {
  const selected: Props['selected'] = { view_id: 'collection:3', kind: 'collection', id: 3, numeric_id: 3,
    name: names[2], description: '', series_count: 1, source_type: 'manual', created_at: '' };
  const { props, messages } = setup('zh-CN', names[2], { selected });
  const edit = screen.getByRole('button', { name: messages['common.edit'] });
  expect(edit.classList.contains('shrink-0')).toBe(true);
  fireEvent.click(edit);
  fireEvent.click(screen.getByRole('button', { name: messages['common.remove'] }));
  expect(props.onEditCollection).toHaveBeenCalledTimes(1);
  expect(props.onRemoveSeries).toHaveBeenCalledWith(201);
  expect(props.onOpenSeries).not.toHaveBeenCalled();
  expect(screen.queryByRole('button', { name: messages['collections.snapshot'] })).toBeNull();
});

it.each(['smart', 'collection'] as const)('keeps regular-user %s collections read-only', (kind) => {
  const selected: Props['selected'] = { view_id: `${kind}:3`, kind, id: 3, numeric_id: 3,
    name: names[0], description: '', series_count: 1, source_type: kind === 'smart' ? 'smart_filter' : 'manual', created_at: '' };
  const { props, messages } = setup('en-US', names[0], { canManage: false, selected });
  for (const name of [messages['common.edit'], messages['collections.snapshot'], messages['common.remove']]) {
    expect(screen.queryByRole('button', { name })).toBeNull();
  }
  fireEvent.click(screen.getByText('Member series'));
  expect(props.onOpenSeries).toHaveBeenCalledWith(201);
});

it('keeps franchise badges intact and derived collections read-only even for administrators', () => {
  const selected: Props['selected'] = { view_id: 'collection:3', kind: 'collection', id: 3, numeric_id: 3,
    name: names[0], description: '', series_count: 1, source_type: 'system_franchise', created_at: '' };
  const { messages } = setup('en-US', names[0], { selected });
  const readOnly = screen.getByText(messages['collections.readOnly']);
  expect(readOnly.getAttribute('title')).toBe(messages['collections.readOnlyHint']);
  expect(readOnly.classList.contains('shrink-0')).toBe(true);
  expect(readOnly.classList.contains('whitespace-nowrap')).toBe(true);
  for (const name of [messages['common.edit'], messages['collections.snapshot'], messages['common.remove']]) {
    expect(screen.queryByRole('button', { name })).toBeNull();
  }
});

it('preserves the loaded/total badge and pagination callback', () => {
  const { props, messages } = setup('en-US', names[0], { seriesTotal: 123, hasMore: true });
  const count = screen.getByText(messages['collections.loadedOfTotal'].replace('{{loaded}}', '1').replace('{{total}}', '123'));
  expect(count.classList.contains('shrink-0')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: messages['collections.loadMore'] }));
  expect(props.onLoadMore).toHaveBeenCalledTimes(1);
});

// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState, type ComponentProps } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ReaderHelpPanel } from './ReaderHelpPanel';
import { ReaderImmersiveShell } from './ReaderImmersiveShell';
import { ReaderProgressTray } from './ReaderProgressTray';
import { ReaderSettingsDrawer } from './ReaderSettingsDrawer';
import { ReaderTopBar } from './ReaderTopBar';

vi.mock('../../auth/AuthProvider', () => ({ useAuth: () => ({ isAdmin: false }) }));
vi.mock('../../components/ToastProvider', () => ({ useToast: () => ({ showToast: vi.fn() }) }));

const t = (key: string) => key;
function settingsProps(): ComponentProps<typeof ReaderSettingsDrawer> {
  return {
    t, readerTheme: 'base', setReaderTheme: vi.fn(),
    readMode: 'paged', setReadMode: vi.fn(), readDirection: 'ltr', setReadDirection: vi.fn(),
    doublePage: false, setDoublePage: vi.fn(), scaleMode: 'fit-screen', setScaleMode: vi.fn(),
    imageFilter: 'none', setImageFilter: vi.fn(), autoCrop: false, setAutoCrop: vi.fn(),
    preloadCount: 3, setPreloadCount: vi.fn(), readerImageFormat: 'original', setReaderImageFormat: vi.fn(),
    readerImageQuality: 80, setReaderImageQuality: vi.fn(), eyeProtection: false, setEyeProtection: vi.fn(),
    w2xScale: 2, setW2xScale: vi.fn(), w2xNoise: 0, setW2xNoise: vi.fn(), w2xFormat: 'webp', setW2xFormat: vi.fn(),
    isOnline: true, offlineSupported: false, offlineStatus: null, offlineCaching: false, offlineDeleting: false,
    offlineCachedPages: 0, activePageCount: 10, offlineQueuedPage: null, offlineCacheError: null,
    readerLoading: false, onCacheBook: vi.fn(), onDeleteOfflineBook: vi.fn(),
    bookmarks: [], bookmarkNote: '', savingBookmark: false, loading: false, currentBookmark: null,
    currentPageNumber: 1, onBookmarkNoteChange: vi.fn(), onSaveBookmark: vi.fn(),
    onDeleteBookmark: vi.fn(), onJumpToPage: vi.fn(),
  };
}

function setup(initialPanel: 'settings' | 'help' = 'settings', visible = true) {
  const settings = settingsProps();
  const onEdgeReveal = vi.fn();
  const onCommitPage = vi.fn();
  const onSliderChange = vi.fn();
  function Harness() {
    const [panel, setPanel] = useState<'settings' | 'help' | null>(initialPanel);
    const [sliderValue, setSliderValue] = useState(1);
    return (
      <div className="absolute inset-0 overflow-hidden">
        <ReaderImmersiveShell visible={visible} onEdgeReveal={onEdgeReveal}
          topBar={<>
            <ReaderTopBar
              t={t} bookTitle="Manga" isBookmarked={false} savingBookmark={false} loading={false}
              showHelp={panel === 'help'} showSettings={panel === 'settings'}
              onBack={vi.fn()} onSaveBookmark={vi.fn()} allInVolume={[]} currentBookId={1}
              currentPageNumber={1} onOpenBook={vi.fn()}
              onToggleSettings={() => setPanel(panel === 'settings' ? null : 'settings')}
              onToggleHelp={() => setPanel(panel === 'help' ? null : 'help')}
            />
            {panel === 'settings' && <ReaderSettingsDrawer {...settings} />}
            {panel === 'help' && <ReaderHelpPanel t={t} />}
          </>}
          tray={<ReaderProgressTray
            t={t} readDirection="ltr" currentPageIndex={0} pageCount={10} sliderValue={sliderValue}
            hoverPage={null} hoverX={0} onSliderChange={value => { setSliderValue(value); onSliderChange(value); }}
            onHoverPageChange={vi.fn()} onHoverXChange={vi.fn()} onCommitPage={onCommitPage}
            prev={null} next={null} onOpenBook={vi.fn()}
          />}
        />
      </div>
    );
  }
  const view = render(<Harness />);
  const top = view.container.querySelector('div.absolute.top-0.px-6') as HTMLElement;
  const tray = view.container.querySelector('div.absolute.bottom-0.z-20') as HTMLElement;
  return { ...view, settings, top, tray, onEdgeReveal, onCommitPage, onSliderChange };
}

beforeEach(() => localStorage.clear());
afterEach(cleanup);

// jsdom guards the real composition's CSS contract and callbacks, not geometry.
// Chromium must separately verify viewport bounds and elementFromPoint hits.
describe('reader panel viewport and occlusion constraints', () => {
  it('limits the whole settings panel and scrolls its body with fixed headers', () => {
    const { top, settings } = setup();
    const header = screen.getByRole('button', { name: 'reader.settingsMode.global' }).parentElement!;
    const drawer = header.parentElement!;
    const tabs = header.nextElementSibling!;
    const body = tabs.nextElementSibling!;
    expect(top.classList.contains('flex-col')).toBe(true);
    expect(top.classList.contains('max-h-dvh')).toBe(true);
    expect(top.classList.contains('[&>:first-child]:shrink-0')).toBe(true);
    expect(drawer.classList.contains('max-h-[calc(100dvh-6rem)]')).toBe(true);
    expect(drawer.classList.contains('max-w-full')).toBe(true);
    expect(drawer.classList.contains('min-h-0')).toBe(true);
    expect(header.classList.contains('shrink-0')).toBe(true);
    expect(tabs.classList.contains('shrink-0')).toBe(true);
    expect(body.classList.contains('min-h-0')).toBe(true);
    expect(body.classList.contains('flex-1')).toBe(true);
    expect(body.classList.contains('overflow-y-auto')).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: /reader.eyeProtectionWarm/ }));
    expect(settings.setEyeProtection).toHaveBeenCalledWith(true);
    fireEvent.click(screen.getByRole('button', { name: 'reader.settingsTab.image' }));
    fireEvent.click(screen.getByRole('button', { name: 'reader.autoCropOff' }));
    expect(settings.setAutoCrop).toHaveBeenCalledWith(true);
  });

  it('stacks settings above the tray without transparent shells intercepting hits', () => {
    const { top, tray, onSliderChange, onCommitPage } = setup();
    expect(top.classList.contains('z-40')).toBe(true);
    expect(tray.classList.contains('z-20')).toBe(true);
    expect(top.classList.contains('pointer-events-none')).toBe(true);
    expect(top.classList.contains('[&>*]:pointer-events-auto')).toBe(true);
    expect(tray.classList.contains('pointer-events-none')).toBe(true);
    const slider = screen.getAllByRole('slider').find(input => input.getAttribute('min') === '1')!;
    expect(slider.closest('.pointer-events-auto')).not.toBeNull();
    fireEvent.change(slider, { target: { value: '4' } });
    fireEvent.mouseUp(slider);
    expect(onSliderChange).toHaveBeenCalledWith(4);
    expect(onCommitPage).toHaveBeenCalledWith(4);
    fireEvent.click(screen.getByTitle('reader.settings'));
    expect(screen.queryByRole('button', { name: /reader.eyeProtectionWarm/ })).toBeNull();
  });

  it('also constrains help and lets its long content scroll', () => {
    const { top } = setup('help');
    const help = screen.getByText('reader.helpShortcuts').closest('.bg-komgaSurface')!;
    expect(help.parentElement).toBe(top);
    expect(help.classList.contains('max-h-[calc(100dvh-6rem)]')).toBe(true);
    expect(help.classList.contains('max-w-full')).toBe(true);
    expect(help.classList.contains('min-h-0')).toBe(true);
    expect(help.classList.contains('overflow-y-auto')).toBe(true);
    expect(screen.getByText('reader.helpTroubleshootingDescription')).toBeTruthy();
  });

  it('leaves reveal edges active but hidden controls inert', () => {
    const { container, top, tray, onEdgeReveal } = setup('settings', false);
    expect(top.hasAttribute('inert')).toBe(true);
    expect(tray.hasAttribute('inert')).toBe(true);
    expect(top.classList.contains('[&>*]:pointer-events-auto')).toBe(false);
    const edges = container.querySelectorAll('div[aria-hidden]');
    expect(edges.length).toBe(2);
    edges.forEach(edge => expect(edge.classList.contains('pointer-events-auto')).toBe(true));
    fireEvent.mouseEnter(edges[0]);
    fireEvent.touchStart(edges[1]);
    expect(onEdgeReveal).toHaveBeenCalledTimes(2);
  });
});

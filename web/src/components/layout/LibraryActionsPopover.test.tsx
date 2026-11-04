// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LibraryActionsPopover } from './LibraryActionsPopover';

function rect(left: number, top: number, width: number, height: number): DOMRect {
    return { x: left, y: top, left, top, width, height, right: left + width, bottom: top + height, toJSON: () => ({}) };
}

let anchorRect: DOMRect;
let contentHeight: number;
let anchor: HTMLButtonElement;
let notifyResize: () => void;
const disconnect = vi.fn();
const observe = vi.fn();

beforeEach(() => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1024 });
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 600 });
    anchorRect = rect(220, 551, 28, 28);
    contentHeight = 324;
    anchor = document.createElement('button');
    anchor.textContent = 'Library details';
    document.body.append(anchor);
    anchor.focus();
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
        if (this === anchor) return anchorRect;
        if (this.getAttribute('role') === 'group') {
            return rect(Number.parseFloat(this.style.left) || 0, Number.parseFloat(this.style.top) || 0,
                Math.min(192, window.innerWidth - 16), Math.min(contentHeight, Number.parseFloat(this.style.maxHeight) || contentHeight));
        }
        if (this.parentElement?.getAttribute('role') === 'group') return rect(0, 0, 192, contentHeight);
        return rect(0, 0, 0, 0);
    });
    vi.stubGlobal('ResizeObserver', class {
        constructor(callback: () => void) { notifyResize = callback; }
        observe = observe;
        disconnect = disconnect;
        unobserve = vi.fn();
    });
});

afterEach(() => {
    cleanup();
    anchor.remove();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    vi.clearAllMocks();
});

function openPopover() {
    const onClose = vi.fn();
    const rendered = render(
        <LibraryActionsPopover anchor={anchor} label="Library actions" onClose={onClose}>
            <button>Edit library</button>
            <button>Delete library</button>
        </LibraryActionsPopover>
    );
    return { ...rendered, onClose, popup: screen.getByRole('group', { name: 'Library actions' }) };
}

function expectBounds(popup: HTMLElement, top: number, left: number, height: number, width = 192) {
    const bounds = popup.getBoundingClientRect();
    expect(bounds.top).toBe(top);
    expect(bounds.left).toBe(left);
    expect(bounds.height).toBe(height);
    expect(bounds.width).toBe(width);
    expect(bounds.top).toBeGreaterThanOrEqual(8);
    expect(bounds.bottom).toBeLessThanOrEqual(window.innerHeight - 8);
    expect(bounds.left).toBeGreaterThanOrEqual(8);
    expect(bounds.right).toBeLessThanOrEqual(window.innerWidth - 8);
}

describe('library actions viewport placement', () => {
    it('opens above the bottom library at 1024x600, keeping Delete reachable', () => {
        const { popup } = openPopover();
        expectBounds(popup, 223, 56, 324);
        expect(popup.contains(screen.getByRole('button', { name: 'Delete library' }))).toBe(true);
    });

    it('prefers below when the actual menu fits', () => {
        anchorRect = rect(220, 40, 28, 28);
        const { popup } = openPopover();
        expectBounds(popup, 72, 56, 324);
    });

    it.each([
        [rect(0, 40, 28, 28), 1024, 8, 192],
        [rect(1010, 40, 14, 28), 1024, 824, 192],
        [rect(100, 40, 28, 28), 150, 8, 134],
    ])('clamps horizontal bounds for anchor %j in a %i px viewport', (bounds, viewportWidth, left, width) => {
        anchorRect = bounds;
        Object.defineProperty(window, 'innerWidth', { configurable: true, value: viewportWidth });
        const { popup } = openPopover();
        expectBounds(popup, 72, left, 324, width);
        expect(popup.style.maxWidth).toBe('calc(100vw - 16px)');
    });

    it.each([
        [400, 8, 388],
        [140, 172, 420],
    ])('uses the larger side and internal scrolling for tall content at y=%i', (anchorTop, top, availableHeight) => {
        anchorRect = rect(220, anchorTop, 28, 28);
        contentHeight = 900;
        const { popup, onClose } = openPopover();
        expectBounds(popup, top, 56, availableHeight);
        expect(popup.style.maxHeight).toBe(`${availableHeight}px`);
        expect(popup.classList.contains('overflow-y-auto')).toBe(true);
        expect(popup.classList.contains('overscroll-contain')).toBe(true);
        fireEvent.scroll(popup);
        fireEvent.scroll(screen.getByRole('button', { name: 'Delete library' }));
        expect(onClose).not.toHaveBeenCalled();
    });

    it('remeasures when protection warning/actions change the actual content height', () => {
        const { popup, unmount } = openPopover();
        expect(observe).toHaveBeenCalledWith(popup.firstElementChild);
        contentHeight = 420;
        act(() => notifyResize());
        expectBounds(popup, 127, 56, 420);
        contentHeight = 900;
        act(() => notifyResize());
        expectBounds(popup, 8, 56, 539);
        unmount();
        expect(disconnect).toHaveBeenCalledOnce();
    });

    it('remeasures the anchor and viewport on window resize', () => {
        const { popup, onClose } = openPopover();
        anchorRect = rect(120, 220, 28, 28);
        Object.defineProperty(window, 'innerHeight', { configurable: true, value: 400 });
        Object.defineProperty(window, 'innerWidth', { configurable: true, value: 180 });
        fireEvent(window, new Event('resize'));
        expectBounds(popup, 8, 8, 208, 164);
        expect(onClose).not.toHaveBeenCalled();
    });
});

describe('library actions dismissal', () => {
    it('focuses the first action without scrolling the anchor', () => {
        openPopover();
        expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Edit library' }));
    });

    it.each(['sidebar', 'window'])('reanchors on external %s scrolling, ignoring internal scrolling', (target) => {
        const { popup, onClose } = openPopover();
        anchorRect = rect(120, 40, 28, 28);
        fireEvent.scroll(popup);
        fireEvent.scroll(screen.getByRole('button', { name: 'Delete library' }));
        expectBounds(popup, 223, 56, 324);
        const sidebar = document.createElement('nav');
        document.body.append(sidebar);
        fireEvent.scroll(target === 'window' ? window : sidebar);
        sidebar.remove();
        expectBounds(popup, 72, 8, 324);
        expect(onClose).not.toHaveBeenCalled();
    });

    it('closes if external scrolling takes the anchor outside the viewport', () => {
        const { onClose } = openPopover();
        anchorRect = rect(220, -40, 28, 28);
        fireEvent.scroll(window);
        expect(onClose).toHaveBeenCalledOnce();
    });

    it('closes on Escape and restores focus to the anchor', () => {
        const { onClose } = openPopover();
        screen.getByRole('button', { name: 'Delete library' }).focus();
        fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
        expect(onClose).toHaveBeenCalledOnce();
        expect(document.activeElement).toBe(anchor);
    });

    it('closes on the backdrop, but not on popup clicks', () => {
        const { popup, onClose } = openPopover();
        fireEvent.click(screen.getByRole('button', { name: 'Edit library' }));
        expect(onClose).not.toHaveBeenCalled();
        fireEvent.click(popup.previousElementSibling!);
        expect(onClose).toHaveBeenCalledOnce();
    });

    it('closes on outside clicks beyond the backdrop', () => {
        const { onClose } = openPopover();
        fireEvent.click(document.body);
        expect(onClose).toHaveBeenCalledOnce();
    });

    it('does not bubble popup clicks to the surrounding library link', () => {
        const onNavigate = vi.fn();
        render(
            <a href="/library/10" onClick={onNavigate}>
                <LibraryActionsPopover anchor={anchor} label="Library actions" onClose={vi.fn()}>
                    <button>Edit library</button>
                </LibraryActionsPopover>
            </a>
        );
        fireEvent.click(screen.getByRole('group', { name: 'Library actions' }));
        expect(onNavigate).not.toHaveBeenCalled();
    });

    it('removes global listeners on unmount', () => {
        const { onClose, unmount } = openPopover();
        unmount();
        anchor.remove();
        fireEvent.scroll(window);
        fireEvent(window, new Event('resize'));
        fireEvent.keyDown(document, { key: 'Escape' });
        expect(onClose).not.toHaveBeenCalled();
    });
});

import { useEffect, useLayoutEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

interface LibraryActionsPopoverProps {
    anchor: HTMLButtonElement;
    label: string;
    onClose: () => void;
    children: ReactNode;
}

const VIEWPORT_MARGIN = 8;
const ANCHOR_GAP = 4;

export function LibraryActionsPopover({ anchor, label, onClose, children }: LibraryActionsPopoverProps) {
    const popupRef = useRef<HTMLDivElement>(null);
    const contentRef = useRef<HTMLDivElement>(null);

    useLayoutEffect(() => {
        const popup = popupRef.current;
        const content = contentRef.current;
        if (!popup || !content) return;

        const reposition = () => {
            const rect = anchor.getBoundingClientRect();
            if (!anchor.isConnected || rect.width === 0 || rect.height === 0) {
                onClose();
                return;
            }
            const viewportWidth = window.innerWidth;
            const viewportHeight = window.innerHeight;
            if (rect.bottom <= 0 || rect.top >= viewportHeight || rect.right <= 0 || rect.left >= viewportWidth) {
                onClose();
                return;
            }
            const width = popup.getBoundingClientRect().width;
            const styles = getComputedStyle(popup);
            // Measure unconstrained content, not the popup's possibly scroll-limited height.
            const height = content.getBoundingClientRect().height
                + (Number.parseFloat(styles.borderTopWidth) || 0)
                + (Number.parseFloat(styles.borderBottomWidth) || 0);
            const below = Math.max(0, viewportHeight - VIEWPORT_MARGIN - rect.bottom - ANCHOR_GAP);
            const above = Math.max(0, rect.top - ANCHOR_GAP - VIEWPORT_MARGIN);
            const openBelow = height <= below || (height > above && below >= above);
            const availableHeight = openBelow ? below : above;
            const visibleHeight = Math.min(height, availableHeight);
            const preferredTop = openBelow ? rect.bottom + ANCHOR_GAP : rect.top - ANCHOR_GAP - visibleHeight;

            popup.style.maxHeight = `${availableHeight}px`;
            popup.style.top = `${Math.max(VIEWPORT_MARGIN, Math.min(preferredTop, viewportHeight - VIEWPORT_MARGIN - visibleHeight))}px`;
            popup.style.left = `${Math.max(VIEWPORT_MARGIN, Math.min(rect.right - width, viewportWidth - VIEWPORT_MARGIN - width))}px`;
        };

        const handleScroll = (event: Event) => {
            if (event.target instanceof Node && popup.contains(event.target)) return;
            reposition();
        };
        reposition();
        const observer = new ResizeObserver(reposition);
        observer.observe(content);
        window.addEventListener('resize', reposition);
        window.addEventListener('scroll', handleScroll, true);
        return () => {
            observer.disconnect();
            window.removeEventListener('resize', reposition);
            window.removeEventListener('scroll', handleScroll, true);
        };
    }, [anchor, onClose]);

    useLayoutEffect(() => {
        popupRef.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus({ preventScroll: true });
    }, [anchor]);

    useEffect(() => {
        const handleClick = (event: MouseEvent) => {
            if (event.target instanceof Node && (popupRef.current?.contains(event.target) || anchor.contains(event.target))) return;
            onClose();
        };
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            event.preventDefault();
            event.stopPropagation();
            onClose();
            anchor.focus({ preventScroll: true });
        };
        document.addEventListener('click', handleClick);
        document.addEventListener('keydown', handleKeyDown);
        return () => {
            document.removeEventListener('click', handleClick);
            document.removeEventListener('keydown', handleKeyDown);
        };
    }, [anchor, onClose]);

    return createPortal(
        <>
            <div
                className="fixed inset-0 z-40"
                onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    onClose();
                }}
            />
            <div
                ref={popupRef}
                role="group"
                aria-label={label}
                className="fixed w-48 bg-gray-800 border border-gray-700 rounded-lg shadow-xl z-50 overflow-y-auto overscroll-contain"
                style={{ maxWidth: 'calc(100vw - 16px)' }}
                onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                }}
            >
                <div ref={contentRef}>{children}</div>
            </div>
        </>,
        document.body
    );
}

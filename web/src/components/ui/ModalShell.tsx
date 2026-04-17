import { useEffect, useId, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';

type ModalSize = 'compact' | 'standard' | 'wide';
type ModalPlacement = 'center' | 'top';

interface ModalShellProps {
  open: boolean;
  onClose: () => void;
  title?: string;
  description?: string;
  icon?: ReactNode;
  headerActions?: ReactNode;
  headerContent?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
  size?: ModalSize;
  placement?: ModalPlacement;
  closeOnBackdrop?: boolean;
  closeOnEsc?: boolean;
  showCloseButton?: boolean;
  zIndexClassName?: string;
  panelClassName?: string;
  bodyClassName?: string;
  headerClassName?: string;
  footerClassName?: string;
}

const sizeClassMap: Record<ModalSize, string> = {
  compact: 'max-w-lg',
  standard: 'max-w-3xl',
  wide: 'max-w-6xl',
};

export function ModalShell({
  open,
  onClose,
  title,
  description,
  icon,
  headerActions,
  headerContent,
  footer,
  children,
  size = 'standard',
  placement = 'center',
  closeOnBackdrop = true,
  closeOnEsc = true,
  showCloseButton = true,
  zIndexClassName = 'z-50',
  panelClassName = '',
  bodyClassName = '',
  headerClassName = '',
  footerClassName = '',
}: ModalShellProps) {
  const titleId = useId();
  const descriptionId = useId();

  useEffect(() => {
    if (!open) return;

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const handleKeyDown = (event: KeyboardEvent) => {
      if (closeOnEsc && event.key === 'Escape') {
        onClose();
      }
    };

    document.addEventListener('keydown', handleKeyDown);

    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [closeOnEsc, onClose, open]);

  if (!open) return null;

  return createPortal(
    <div className={`fixed inset-0 ${zIndexClassName}`}>
      <div
        className="absolute inset-0 backdrop-blur-sm"
        style={{
          background:
            'radial-gradient(circle at top, rgb(var(--theme-glow) / 0.16), transparent 35%), linear-gradient(to bottom, rgb(var(--theme-overlay-top) / 0.78), rgb(var(--theme-overlay-bottom) / 0.88))',
        }}
        onClick={closeOnBackdrop ? onClose : undefined}
      />
      <div
        className={`relative flex min-h-full justify-center px-4 ${placement === 'top' ? 'items-start pt-[8vh] pb-6' : 'items-center py-4'}`}
      >
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby={title ? titleId : undefined}
          aria-describedby={description ? descriptionId : undefined}
          onClick={(event) => event.stopPropagation()}
          className={`relative flex w-full ${sizeClassMap[size]} max-h-[92vh] flex-col overflow-hidden rounded-[28px] border border-gray-800/90 shadow-[0_32px_110px_-34px_rgb(var(--theme-shadow)/0.92)] ${panelClassName}`}
          style={{
            background: 'linear-gradient(180deg, rgb(var(--theme-modal-top) / 0.98), rgb(var(--theme-modal-bottom) / 0.98))',
          }}
        >
          <div
            className="pointer-events-none absolute inset-x-0 top-0 h-28 opacity-80"
            style={{ background: 'radial-gradient(circle at top, rgb(var(--color-white) / 0.08), transparent 65%)' }}
          />

          {(title || description || headerActions || headerContent || showCloseButton) && (
            <div className={`relative border-b border-gray-800/90 bg-gray-950/35 px-5 py-5 sm:px-6 ${headerClassName}`}>
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1">
                  {(title || description || icon) && (
                    <div className="min-w-0">
                      {title && (
                        <div className="flex items-center gap-3">
                          {icon ? (
                            <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-white/10 bg-white/[0.03] text-komgaPrimary shadow-[inset_0_1px_0_rgb(255_255_255/0.08)]">
                              {icon}
                            </div>
                          ) : null}
                          <div className="min-w-0">
                            <h3 id={titleId} className="truncate text-xl font-semibold tracking-tight text-white">
                              {title}
                            </h3>
                            {description ? (
                              <p id={descriptionId} className="mt-1 max-w-2xl text-sm leading-6 text-gray-400">
                                {description}
                              </p>
                            ) : null}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                  {headerContent ? <div className="mt-4">{headerContent}</div> : null}
                </div>

                <div className="flex shrink-0 items-center gap-2">
                  {headerActions}
                  {showCloseButton ? (
                    <button
                      type="button"
                      onClick={onClose}
                      className="inline-flex h-10 w-10 items-center justify-center rounded-2xl border border-gray-700/80 bg-gray-900/70 text-gray-400 transition-all hover:border-gray-600 hover:bg-gray-800 hover:text-white"
                      aria-label="关闭"
                    >
                      <X className="h-5 w-5" />
                    </button>
                  ) : null}
                </div>
              </div>
            </div>
          )}

          <div className={`relative flex-1 overflow-y-auto px-5 py-5 sm:px-6 sm:py-6 ${bodyClassName}`}>{children}</div>

          {footer ? (
            <div className={`relative border-t border-gray-800/90 bg-gray-950/45 px-5 py-4 sm:px-6 ${footerClassName}`}>{footer}</div>
          ) : null}
        </div>
      </div>
    </div>,
    document.body,
  );
}

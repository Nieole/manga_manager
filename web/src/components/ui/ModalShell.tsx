import { useEffect, useId, useRef, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { useI18n } from '../../i18n/LocaleProvider';

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

// FOCUSABLE_SELECTOR 覆盖浏览器默认可聚焦的元素，外加显式 tabindex 的自定义控件。
// 排除 tabindex="-1"：那是「可以用脚本聚焦、但不进 Tab 顺序」的约定。
const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

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
  const { t } = useI18n();
  const titleId = useId();
  const descriptionId = useId();
  const panelRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;

    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    // 记住是谁打开的这个弹窗，关闭时把焦点还回去。
    // 不还的话，键盘用户关掉弹窗后焦点回到 <body>，下一次 Tab 要从整页开头重新走一遍。
    const previouslyFocused = document.activeElement as HTMLElement | null;

    // 打开时必须把焦点移进弹窗，否则读屏软件读的还是背景那一页，
    // 键盘用户按 Tab 也是在背景内容之间走——弹窗形同不存在。
    const focusFirst = () => {
      const panel = panelRef.current;
      if (!panel) return;
      const target = panel.querySelector<HTMLElement>(FOCUSABLE_SELECTOR) ?? panel;
      target.focus();
    };
    // 等一帧：内容可能是异步渲染的（懒加载的表单、Suspense 边界）。
    const raf = requestAnimationFrame(focusFirst);

    const handleKeyDown = (event: KeyboardEvent) => {
      if (closeOnEsc && event.key === 'Escape') {
        onClose();
        return;
      }
      if (event.key !== 'Tab') return;

      // 焦点陷阱：aria-modal 只是告诉读屏软件「背景不可用」，浏览器的 Tab 顺序并不受它约束。
      // 没有这段，Tab 会走出弹窗、落到背后那一页的链接上——视觉上被遮住，却仍然可聚焦、可点击。
      const panel = panelRef.current;
      if (!panel) return;
      // 不用 offsetParent 过滤「不可见元素」：弹窗面板本身挂在 position: fixed 的容器里，
      // 里面所有元素的 offsetParent 都是 null，那样过滤会把整个列表清空、陷阱直接失效。
      // 隐藏元素靠 hidden 属性排除即可；disabled 已经在选择器里挡掉了。
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
        (el) => !el.hasAttribute('hidden'),
      );
      if (focusable.length === 0) {
        event.preventDefault();
        panel.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const active = document.activeElement as HTMLElement | null;
      if (event.shiftKey && (active === first || !panel.contains(active))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && (active === last || !panel.contains(active))) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener('keydown', handleKeyDown);

    return () => {
      cancelAnimationFrame(raf);
      document.body.style.overflow = previousOverflow;
      document.removeEventListener('keydown', handleKeyDown);
      // 元素可能已随页面变化被卸载，isConnected 兜一下。
      if (previouslyFocused && previouslyFocused.isConnected) {
        previouslyFocused.focus();
      }
    };
  }, [closeOnEsc, onClose, open]);

  if (!open) return null;

  return createPortal(
    <div className={`fixed inset-0 ${zIndexClassName}`}>
      <div
        className="absolute inset-0 backdrop-blur-xs"
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
          ref={panelRef}
          role="dialog"
          aria-modal="true"
          tabIndex={-1}
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
            style={{ background: 'radial-gradient(circle at top, rgb(var(--rgb-white) / 0.08), transparent 65%)' }}
          />

          {(title || headerActions || headerContent || showCloseButton) && (
            <div className={`relative border-b border-gray-800/90 bg-gray-950/35 px-5 py-5 sm:px-6 ${headerClassName}`}>
              <div className="flex items-start justify-between gap-4">
                <div className="min-w-0 flex-1">
                  {(title || icon) && (
                    <div className="min-w-0">
                      {title && (
                        <div className="flex items-center gap-3">
                          {icon ? (
                            <div className="flex h-10 w-10 items-center justify-center rounded-2xl border border-white/10 bg-white/3 text-komgaPrimary shadow-[inset_0_1px_0_rgb(255_255_255/0.08)]">
                              {icon}
                            </div>
                          ) : null}
                          <div className="min-w-0">
                            <h3 id={titleId} className="truncate text-xl font-semibold tracking-tight text-white">
                              {title}
                            </h3>
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
                      aria-label={t('common.close')}
                    >
                      <X className="h-5 w-5" />
                    </button>
                  ) : null}
                </div>
              </div>
            </div>
          )}

          {(description || children) ? (
            <div className={`relative flex-1 overflow-y-auto px-5 py-5 sm:px-6 sm:py-6 ${bodyClassName}`}>
              {description ? (
                <div id={descriptionId} className={`text-sm leading-6 text-gray-400 ${children ? 'mb-4' : ''}`}>
                  {description}
                </div>
              ) : null}
              {children}
            </div>
          ) : null}

          {footer ? (
            <div className={`relative border-t border-gray-800/90 bg-gray-950/45 px-5 py-4 sm:px-6 ${footerClassName}`}>{footer}</div>
          ) : null}
        </div>
      </div>
    </div>,
    document.body,
  );
}

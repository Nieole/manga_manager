import type { ReactNode } from 'react';

interface ReaderImmersiveShellProps {
  visible: boolean;
  topBar: ReactNode;
  tray: ReactNode;
  onEdgeReveal: () => void;
}

export function ReaderImmersiveShell({ visible, topBar, tray, onEdgeReveal }: ReaderImmersiveShellProps) {
  return (
    <>
      <div
        aria-hidden
        onMouseEnter={onEdgeReveal}
        onTouchStart={onEdgeReveal}
        className={`absolute top-0 inset-x-0 h-10 z-30 ${visible ? 'pointer-events-none' : 'pointer-events-auto'}`}
      />
      {/* Keep the toolbar fixed while panels shrink into the remaining viewport.
          Only the visible controls, not the gradient shell, receive pointer hits. */}
      <div
        inert={!visible}
        className={`absolute top-0 inset-x-0 px-6 pt-4 pb-3 flex flex-col max-h-dvh [&>:first-child]:shrink-0 pointer-events-none bg-linear-to-b from-komgaDark/90 via-komgaDark/55 to-transparent z-40 transition-all duration-300 ${
          visible ? 'opacity-100 translate-y-0 [&>*]:pointer-events-auto' : 'opacity-0 -translate-y-4'
        }`}
      >
        {topBar}
      </div>
      <div
        aria-hidden
        onMouseEnter={onEdgeReveal}
        onTouchStart={onEdgeReveal}
        className={`absolute bottom-0 inset-x-0 h-12 z-30 ${visible ? 'pointer-events-none' : 'pointer-events-auto'}`}
      />
      <div
        inert={!visible}
        className={`absolute bottom-0 inset-x-0 z-20 pointer-events-none transition-all duration-300 ${
          visible ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4'
        }`}
      >
        {tray}
      </div>
    </>
  );
}

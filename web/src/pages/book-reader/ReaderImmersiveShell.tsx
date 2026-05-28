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
      <div
        className={`absolute top-0 inset-x-0 px-6 pt-4 pb-3 bg-gradient-to-b from-komgaDark/90 via-komgaDark/55 to-transparent z-20 transition-all duration-300 ${
          visible ? 'opacity-100 translate-y-0' : 'opacity-0 -translate-y-4 pointer-events-none'
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
        className={`absolute bottom-0 inset-x-0 z-20 transition-all duration-300 ${
          visible ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4 pointer-events-none'
        }`}
      >
        {tray}
      </div>
    </>
  );
}

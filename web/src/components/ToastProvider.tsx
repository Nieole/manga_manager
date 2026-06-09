/**
 * 业务说明：本文件是业务实现，属于前端共享组件层，负责沉淀按钮、面板、列表、封面、进度和反馈等可复用 UI 片段。
 * 它让资料库、阅读器、设置和系列详情在视觉和交互上保持一致。
 * 维护时应关注组件职责边界、可访问性、主题变量、加载态和不同页面的复用语义。
 */

import { createContext, useCallback, useContext, useState, type ReactNode } from 'react';

interface ToastItem {
  id: number;
  text: string;
  type: 'success' | 'error';
}

interface ToastContextValue {
  showToast: (text: string, type?: 'success' | 'error') => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  const showToast = useCallback((text: string, type: 'success' | 'error' = 'success') => {
    const id = ++nextId;
    setToasts((prev) => [...prev, { id, text, type }]);
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((item) => item.id !== id));
    }, 3200);
  }, []);

  const dismiss = useCallback((id: number) => {
    setToasts((prev) => prev.filter((item) => item.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ showToast }}>
      {children}
      {toasts.length > 0 && (
        <div className="fixed bottom-6 right-6 z-[9999] flex flex-col gap-2">
          {toasts.map((toast) => (
            <div
              key={toast.id}
              className="animate-in slide-in-from-bottom-5 fade-in duration-300"
            >
              <div
                className={`flex items-center gap-3 rounded-lg border px-4 py-3 shadow-lg ${
                  toast.type === 'success'
                    ? 'border-green-700 bg-green-900 text-green-100'
                    : 'border-red-700 bg-red-900 text-red-100'
                }`}
              >
                <span className="text-sm font-medium">{toast.text}</span>
                <button
                  onClick={() => dismiss(toast.id)}
                  className="ml-2 text-white/50 hover:text-white"
                >
                  ✕
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </ToastContext.Provider>
  );
}

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    // Fallback for components not wrapped in ToastProvider
    return {
      showToast: (text, type) => {
        console.warn('[Toast fallback]', type, text);
      },
    };
  }
  return ctx;
}

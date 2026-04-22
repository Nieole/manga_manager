import React from 'react';
import { getClientLocale, translateInLocale } from '../i18n/LocaleProvider';

interface ErrorBoundaryState {
    hasError: boolean;
    error: Error | null;
}

/**
 * 全局错误边界组件，阻止子组件崩溃导致整页白屏。
 * 提供友好的错误提示和"返回主页"按钮。
 */
export default class ErrorBoundary extends React.Component<
    { children: React.ReactNode },
    ErrorBoundaryState
> {
    constructor(props: { children: React.ReactNode }) {
        super(props);
        this.state = { hasError: false, error: null };
    }

    static getDerivedStateFromError(error: Error): ErrorBoundaryState {
        return { hasError: true, error };
    }

    componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
        console.error('ErrorBoundary caught an error:', error, errorInfo);
    }

    handleReset = () => {
        this.setState({ hasError: false, error: null });
        window.location.href = '/';
    };

    render() {
        if (this.state.hasError) {
            const locale = getClientLocale();
            return (
                <div className="flex-1 flex items-center justify-center p-10 h-full">
                    <div className="text-center max-w-md">
                        <div className="text-6xl mb-6">💥</div>
                        <h2 className="text-2xl font-bold text-white mb-3">{translateInLocale(locale, 'errorBoundary.title')}</h2>
                        <p className="text-gray-400 text-sm mb-2">
                            {translateInLocale(locale, 'errorBoundary.description')}
                        </p>
                        {this.state.error && (
                            <pre className="text-xs text-red-400/70 bg-red-500/10 border border-red-500/20 rounded-lg p-3 mt-4 mb-6 text-left overflow-auto max-h-32">
                                {this.state.error.message}
                            </pre>
                        )}
                        <button
                            onClick={this.handleReset}
                            className="px-6 py-3 bg-komgaPrimary hover:bg-komgaPrimaryHover text-white font-medium rounded-xl shadow-lg transition-all duration-300 hover:scale-105"
                        >
                            {translateInLocale(locale, 'errorBoundary.backHome')}
                        </button>
                    </div>
                </div>
            );
        }
        return this.props.children;
    }
}

import React, { useMemo, useCallback } from 'react';
import { MangaViewer } from '@yui540/comimi-react';
import type { ReaderThemeProps } from '../ReaderThemeProps';
import { ReaderSettingsDrawer } from '../ReaderSettingsDrawer';
import { Settings, ArrowLeft } from 'lucide-react';

export function ComimiTheme(props: ReaderThemeProps) {
    const {
        bookId,
        bookTitle,
        pages,
        ensurePageImageLoaded,
        handleBackToSeries,
        readerLoading,
        t,
        loadError,
        readDirection,
        showSettings,
        setShowSettings,
    } = props;

    // Convert pages to Comimi's format
    const manga = useMemo(() => {
        if (!pages || pages.length === 0) return { id: String(bookId), title: bookTitle, author: '', pages: [] };
        return {
            id: String(bookId),
            title: bookTitle,
            author: "",
            pages: pages.map((page) => ({
                id: String(page.number),
                type: 'image' as const,
                src: 'data:image/gif;base64,R0lGODlhAQABAAD/ACwAAAAAAQABAAACADs=',
            }))
        };
    }, [pages, bookTitle, bookId]);

    // Comimi will call this to resolve the actual image URL
    const resolvePageSrc = useCallback(async (context: any) => {
        try {
            const pageObj = context.page || context;
            const pageNum = parseInt(pageObj.id, 10);
            if (!pageNum || !bookId) return pageObj.src || "";
            // This triggers our cache, preloading, blob fetching, etc.
            const url = await ensurePageImageLoaded(bookId, pageNum);
            return url;
        } catch (e) {
            console.error("Failed to resolve page src:", context, e);
            return context?.page?.src || context?.src || ""; 
        }
    }, [ensurePageImageLoaded, bookId]);

    if (readerLoading) {
        return (
            <div className="absolute inset-0 bg-komgaDark flex items-center justify-center text-gray-400">
                <div className="flex flex-col items-center">
                    <svg className="animate-spin -ml-1 mr-3 h-8 w-8 text-komgaPrimary mb-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                    </svg>
                    <span>Loading...</span>
                </div>
            </div>
        );
    }

    if (loadError) {
        return (
            <div className="absolute inset-0 bg-komgaDark flex items-center justify-center text-red-500">
                <div className="flex flex-col items-center">
                    <span className="mb-4 text-xl">Error: {loadError}</span>
                    <button onClick={handleBackToSeries} className="bg-gray-800 px-4 py-2 rounded hover:bg-gray-700 text-white">
                        {t('reader.backToSeries')}
                    </button>
                </div>
            </div>
        );
    }

    return (
        <div className="absolute inset-0 z-50 bg-black comimi-custom-wrapper" style={{ '--cmm-dir': readDirection === 'rtl' ? 'rtl' : 'ltr' } as React.CSSProperties}>
            {/* Global style to hide the native comimi menu panel to prevent confusion since we disabled its thumbnails */}
            <style>{`
                .comimi-menu-panel { display: none !important; }
                .comimi-custom-wrapper:has(.comimi-controls-dock[data-overlay="false"]) .custom-overlay-ui {
                    opacity: 0;
                    pointer-events: none;
                }
                .custom-overlay-ui {
                    transition: opacity 0.3s linear;
                }
            `}</style>
            
            <MangaViewer 
                manga={manga}
                resolvePageSrc={resolvePageSrc}
                settings={{ 
                    readingDirection: readDirection === 'rtl' ? 'rtl' : 'ltr',
                    layoutMode: 'browserFullscreen'
                }}
                lockLayoutMode
            />
            
            
            {/* Custom Back Button overlaid on top-left */}
            <div className="absolute top-6 left-6 z-[9999] custom-overlay-ui">
                <button 
                    onClick={handleBackToSeries}
                    className="backdrop-blur-sm p-3 rounded-2xl transition-all flex items-center justify-center hover:opacity-90"
                    style={{ background: 'rgba(255, 255, 255, 0.8)', color: '#333', boxShadow: '0 0 8px rgba(0,0,0,0.1)' }}
                    title={t('reader.backToSeries')}
                >
                    <ArrowLeft className="w-6 h-6" />
                </button>
            </div>

            {/* Custom Settings Button overlaid on top-right */}
            <div className="absolute top-6 right-6 z-[9999] custom-overlay-ui">
                <button 
                    onClick={() => setShowSettings(!showSettings)}
                    className="backdrop-blur-sm p-3 rounded-2xl transition-all flex items-center justify-center hover:opacity-90"
                    style={{ background: 'rgba(255, 255, 255, 0.8)', color: '#333', boxShadow: '0 0 8px rgba(0,0,0,0.1)' }}
                    title={t('reader.settings')}
                >
                    <Settings className="w-6 h-6" />
                </button>
            </div>

            {showSettings && (
                <div className="absolute top-20 right-6 z-[9999] custom-overlay-ui">
                    <ReaderSettingsDrawer
                        t={t}
                        readerTheme={props.readerTheme}
                        setReaderTheme={props.setReaderTheme}
                        readMode={props.readMode}
                        setReadMode={props.setReadMode}
                        readDirection={props.readDirection}
                        setReadDirection={props.setReadDirection}
                        doublePage={props.doublePage}
                        setDoublePage={props.setDoublePage}
                        scaleMode={props.scaleMode}
                        setScaleMode={props.setScaleMode}
                        imageFilter={props.imageFilter}
                        setImageFilter={props.setImageFilter}
                        autoCrop={props.autoCrop}
                        setAutoCrop={props.setAutoCrop}
                        preloadCount={props.preloadCount}
                        setPreloadCount={props.setPreloadCount}
                        readerImageFormat={props.readerImageFormat}
                        setReaderImageFormat={props.setReaderImageFormat}
                        readerImageQuality={props.readerImageQuality}
                        setReaderImageQuality={props.setReaderImageQuality}
                        eyeProtection={props.eyeProtection}
                        setEyeProtection={props.setEyeProtection}
                        w2xScale={props.w2xScale}
                        setW2xScale={props.setW2xScale}
                        w2xNoise={props.w2xNoise}
                        setW2xNoise={props.setW2xNoise}
                        w2xFormat={props.w2xFormat}
                        setW2xFormat={props.setW2xFormat}
                        isOnline={props.isOnline}
                        offlineSupported={props.offlineSupported}
                        offlineStatus={props.offlineStatus}
                        offlineCaching={props.offlineCaching}
                        offlineDeleting={props.offlineDeleting}
                        offlineCachedPages={props.offlineCachedPages}
                        activePageCount={props.pages.length}
                        offlineQueuedPage={props.offlineQueuedPage}
                        offlineCacheError={props.offlineCacheError}
                        readerLoading={props.readerLoading}
                        onCacheBook={props.handleCacheBookOffline}
                        onDeleteOfflineBook={props.handleDeleteOfflineBook}
                        bookmarks={props.bookmarks}
                        bookmarkNote={props.bookmarkNote}
                        savingBookmark={props.savingBookmark}
                        loading={props.loading}
                        currentBookmark={props.currentBookmark}
                        currentPageNumber={props.currentPageNumber}
                        onBookmarkNoteChange={props.setBookmarkNote}
                        onSaveBookmark={props.handleSaveBookmark}
                        onDeleteBookmark={props.handleDeleteBookmark}
                        onJumpToPage={props.jumpToPage}
                    />
                </div>
            )}
        </div>
    );
}

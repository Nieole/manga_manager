import { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { PagedReader } from './book-reader/PagedReader';
import { ReaderHelpPanel } from './book-reader/ReaderHelpPanel';
import { ReaderProgressTray } from './book-reader/ReaderProgressTray';
import { ReaderSettingsDrawer } from './book-reader/ReaderSettingsDrawer';
import { ReaderErrorState, ReaderEyeProtectionOverlay, ReaderLoadingState } from './book-reader/ReaderStateViews';
import { ReaderTopBar } from './book-reader/ReaderTopBar';
import { WebtoonReader, type WebtoonReaderHandle } from './book-reader/WebtoonReader';
import { usePageImageCache } from './book-reader/usePageImageCache';
import { useReaderBookData } from './book-reader/useReaderBookData';
import { useReaderBookmarks } from './book-reader/useReaderBookmarks';
import { useReaderKeyboardShortcuts } from './book-reader/useReaderKeyboardShortcuts';
import { useReaderOffline } from './book-reader/useReaderOffline';
import { useReaderPageNavigation } from './book-reader/useReaderPageNavigation';
import { useReaderPointerDrag } from './book-reader/useReaderPointerDrag';
import { useReaderPreferences } from './book-reader/useReaderPreferences';
import { useReaderProgressPipeline } from './book-reader/useReaderProgressPipeline';
import { useI18n } from '../i18n/LocaleProvider';

export default function BookReader() {
    const { t } = useI18n();
    const { bookId } = useParams();
    const navigate = useNavigate();

    const {
        readMode,
        setReadMode,
        readDirection,
        setReadDirection,
        doublePage,
        setDoublePage,
        scaleMode,
        setScaleMode,
        imageFilter,
        setImageFilter,
        autoCrop,
        setAutoCrop,
        preloadCount,
        setPreloadCount,
        readerImageFormat,
        setReaderImageFormat,
        readerImageQuality,
        setReaderImageQuality,
        eyeProtection,
        setEyeProtection,
        w2xScale,
        setW2xScale,
        w2xNoise,
        setW2xNoise,
        w2xFormat,
        setW2xFormat,
    } = useReaderPreferences();
    const readModeRef = useRef(readMode);
    const tRef = useRef(t);
    const webtoonReaderRef = useRef<WebtoonReaderHandle>(null);

    useEffect(() => {
        readModeRef.current = readMode;
    }, [readMode]);

    useEffect(() => {
        tRef.current = t;
    }, [t]);

    useEffect(() => {
        currentBookIdRef.current = bookId ?? null;
    }, [bookId]);

    // UI State
    const [showSettings, setShowSettings] = useState(false);
    const [showHelp, setShowHelp] = useState(false);
    // Paged mode state
    const [currentPageIndex, setCurrentPageIndex] = useState(0);
    // 底部进度条本地状态，用于解耦拖拽 UI 与核心渲染
    const [sliderValue, setSliderValue] = useState(1);
    const [hoverPage, setHoverPage] = useState<number | null>(null);
    const [hoverX, setHoverX] = useState(0);
    const currentBookIdRef = useRef<string | null>(bookId ?? null);
    const {
        cachedPageImageUrls,
        setCachedPageImageUrls,
        imageOptionsKey,
        getBookCache,
        getImageUrlForBook,
        getImageUrl,
        clearAllPageImageCaches,
        cachedImageUrlsForBook,
        retainBookCaches,
        releasePageImagesOutsideWindow,
        ensurePageImageLoaded,
        isPagedImageReady,
    } = usePageImageCache({
        imageFilter,
        w2xScale,
        w2xNoise,
        w2xFormat,
        autoCrop,
        readerImageFormat,
        readerImageQuality,
        currentBookIdRef,
    });
    const {
        pages,
        activePages,
        loading,
        loadError,
        bookTitle,
        bookVolume,
        nextBookId,
        pagesBelongToCurrentBook,
        readerLoading,
        pagesBookIdRef,
        seriesIdRef,
        nextBookIdRef,
        fetchPagesForBook,
        fetchBookInfoForBook,
    } = useReaderBookData({
        bookId,
        currentBookIdRef,
        tRef,
        getBookCache,
        setCachedPageImageUrls,
        cachedImageUrlsForBook,
        retainBookCaches,
        setCurrentPageIndex,
        setSliderValue,
    });
    const currentPageNumber = activePages[currentPageIndex]?.number ?? currentPageIndex + 1;
    const {
        bookmarks,
        bookmarkNote,
        setBookmarkNote,
        savingBookmark,
        currentBookmark,
        saveBookmark: handleSaveBookmark,
        deleteBookmark: handleDeleteBookmark,
    } = useReaderBookmarks({
        bookId,
        currentBookIdRef,
        activePageCount: activePages.length,
        currentPageNumber,
    });
    const {
        isOnline,
        offlineSupported,
        offlineStatus,
        offlineCaching,
        offlineDeleting,
        offlineCachedPages,
        offlineQueuedPage,
        offlineCacheError,
        queueProgress: queueOfflineReaderProgress,
        cacheBookOffline: handleCacheBookOffline,
        deleteBookOffline: handleDeleteOfflineBook,
    } = useReaderOffline({
        bookId,
        bookTitle,
        pages: activePages,
        imageFilter,
        autoCrop,
        readerImageFormat,
        readerImageQuality,
        getImageUrlForBook,
        t,
    });
    const {
        containerRef,
        isDragging,
        handleMouseDown,
        handleMouseLeave,
        handleMouseUp,
        handleMouseMove,
    } = useReaderPointerDrag();

    const handleBackToSeries = useCallback(() => {
        if (seriesIdRef.current) {
            if (bookVolume) {
                navigate(`/series/${seriesIdRef.current}?volume=${encodeURIComponent(bookVolume)}`);
            } else {
                navigate(`/series/${seriesIdRef.current}`);
            }
            return;
        }
        navigate('/');
    }, [bookVolume, navigate]);
    const handleOpenBook = useCallback((targetBookId: number) => {
        navigate(`/reader/${targetBookId}`, { replace: true });
    }, [navigate]);
    const toggleHelp = useCallback(() => {
        setShowHelp((value) => !value);
    }, []);
    const handleWebtoonRenderRangeChange = useCallback((startIndex: number, endIndex: number) => {
        if (!bookId || readModeRef.current !== 'webtoon') return;
        const keepStart = Math.max(0, startIndex - Math.max(2, preloadCount));
        const keepEnd = Math.min(activePages.length - 1, endIndex + Math.max(2, preloadCount));
        const keepPages: number[] = [];
        for (let index = keepStart; index <= keepEnd; index += 1) {
            const page = activePages[index];
            if (page) {
                keepPages.push(page.number);
            }
        }
        releasePageImagesOutsideWindow(bookId, keepPages);
    }, [activePages, bookId, preloadCount, releasePageImagesOutsideWindow]);
    const handleWebtoonRenderedImageCountChange = useCallback((count: number) => {
        if (typeof window === 'undefined') return;
        window.dispatchEvent(new CustomEvent('manga-reader:webtoon-dom-images', {
            detail: {
                bookId,
                count,
                pageCount: activePages.length,
            },
        }));
    }, [activePages.length, bookId]);

    useReaderProgressPipeline({
        bookId,
        loading,
        pages,
        pagesBelongToCurrentBook,
        currentPageIndex,
        readMode,
        doublePage,
        preloadCount,
        nextBookId,
        pagesBookIdRef,
        getBookCache,
        getImageUrlForBook,
        ensurePageImageLoaded,
        fetchPagesForBook,
        fetchBookInfoForBook,
        retainBookCaches,
        queueOfflineReaderProgress,
    });

    const {
        jumpToPage,
        handleNext,
        handlePrev,
        firstPage,
        lastPage,
    } = useReaderPageNavigation({
        activePages,
        doublePage,
        readModeRef,
        nextBookIdRef,
        setCurrentPageIndex,
        setSliderValue,
        onScrollToWebtoonPage: (pageNumber) => webtoonReaderRef.current?.scrollToPage(pageNumber),
        onOpenBook: handleOpenBook,
    });

    // 当书籍或图像处理参数变化时，预加载去重缓存需要重新开始计算。
    useEffect(() => {
        clearAllPageImageCaches();
    }, [imageOptionsKey, clearAllPageImageCaches]);

    useEffect(() => {
        return () => clearAllPageImageCaches();
    }, [clearAllPageImageCaches]);

    // 同步 sliderValue 与全局状态（当通过按钮翻页时）
    useEffect(() => {
        setSliderValue(currentPageIndex + 1);
    }, [currentPageIndex]);

    useReaderKeyboardShortcuts({
        readMode,
        readDirection,
        activePageCount: activePages.length,
        onNext: handleNext,
        onPrev: handlePrev,
        onFirstPage: firstPage,
        onLastPage: lastPage,
        onToggleHelp: toggleHelp,
        onSaveBookmark: handleSaveBookmark,
    });

    return (
        <div className="absolute inset-0 bg-komgaDark flex flex-col z-50 overflow-hidden">
            {/* 顶栏控制面板区悬浮感应 */}
            <div className={`absolute top-0 inset-x-0 h-20 bg-gradient-to-b from-komgaDark/90 to-transparent flex flex-col justify-start pt-4 px-6 transition-all duration-300 z-20 ${showSettings ? 'opacity-100 translate-y-0' : 'opacity-0 -translate-y-4 hover:translate-y-0 hover:opacity-100'}`}>
                <ReaderTopBar
                    t={t}
                    bookTitle={bookTitle}
                    isBookmarked={Boolean(currentBookmark)}
                    savingBookmark={savingBookmark}
                    loading={loading}
                    showHelp={showHelp}
                    showSettings={showSettings}
                    onBack={handleBackToSeries}
                    onSaveBookmark={handleSaveBookmark}
                    onToggleHelp={toggleHelp}
                    onToggleSettings={() => setShowSettings((value) => !value)}
                />

                {showHelp && <ReaderHelpPanel t={t} />}

                {showSettings && (
                    <ReaderSettingsDrawer
                        t={t}
                        readMode={readMode}
                        setReadMode={setReadMode}
                        readDirection={readDirection}
                        setReadDirection={setReadDirection}
                        doublePage={doublePage}
                        setDoublePage={setDoublePage}
                        scaleMode={scaleMode}
                        setScaleMode={setScaleMode}
                        imageFilter={imageFilter}
                        setImageFilter={setImageFilter}
                        autoCrop={autoCrop}
                        setAutoCrop={setAutoCrop}
                        preloadCount={preloadCount}
                        setPreloadCount={setPreloadCount}
                        readerImageFormat={readerImageFormat}
                        setReaderImageFormat={setReaderImageFormat}
                        readerImageQuality={readerImageQuality}
                        setReaderImageQuality={setReaderImageQuality}
                        eyeProtection={eyeProtection}
                        setEyeProtection={setEyeProtection}
                        w2xScale={w2xScale}
                        setW2xScale={setW2xScale}
                        w2xNoise={w2xNoise}
                        setW2xNoise={setW2xNoise}
                        w2xFormat={w2xFormat}
                        setW2xFormat={setW2xFormat}
                        isOnline={isOnline}
                        offlineSupported={offlineSupported}
                        offlineStatus={offlineStatus}
                        offlineCaching={offlineCaching}
                        offlineDeleting={offlineDeleting}
                        offlineCachedPages={offlineCachedPages}
                        activePageCount={activePages.length}
                        offlineQueuedPage={offlineQueuedPage}
                        offlineCacheError={offlineCacheError}
                        readerLoading={readerLoading}
                        onCacheBook={handleCacheBookOffline}
                        onDeleteOfflineBook={handleDeleteOfflineBook}
                        bookmarks={bookmarks}
                        bookmarkNote={bookmarkNote}
                        savingBookmark={savingBookmark}
                        loading={loading}
                        currentBookmark={currentBookmark}
                        currentPageNumber={currentPageNumber}
                        onBookmarkNoteChange={setBookmarkNote}
                        onSaveBookmark={handleSaveBookmark}
                        onDeleteBookmark={handleDeleteBookmark}
                        onJumpToPage={jumpToPage}
                    />
                )}
            </div>

            <div className="flex-1 w-full relative overflow-hidden ReaderScrollContainer">
                {eyeProtection && <ReaderEyeProtectionOverlay />}
                {readerLoading ? (
                    <ReaderLoadingState />
                ) : loadError ? (
                    <ReaderErrorState
                        t={t}
                        message={loadError}
                        onRetry={() => window.location.reload()}
                        onBackToSeries={handleBackToSeries}
                    />
                ) : readMode === 'webtoon' ? (
                    <WebtoonReader
                        ref={webtoonReaderRef}
                        t={t}
                        bookId={bookId}
                        pages={activePages}
                        currentPageIndex={currentPageIndex}
                        cachedPageImageUrls={cachedPageImageUrls}
                        imageFilter={imageFilter}
                        scaleMode={scaleMode}
                        doublePage={doublePage}
                        nextBookId={nextBookId}
                        getImageUrl={getImageUrl}
                        onVisiblePageChange={setCurrentPageIndex}
                        onRenderRangeChange={handleWebtoonRenderRangeChange}
                        onRenderedImageCountChange={handleWebtoonRenderedImageCountChange}
                        onOpenNextBook={(targetBookId) => navigate(`/reader/${targetBookId}`, { replace: true })}
                    />
                ) : (
                    <PagedReader
                        pages={activePages}
                        currentPageIndex={currentPageIndex}
                        doublePage={doublePage}
                        readDirection={readDirection}
                        scaleMode={scaleMode}
                        imageFilter={imageFilter}
                        isDragging={isDragging}
                        containerRef={containerRef}
                        cachedPageImageUrls={cachedPageImageUrls}
                        isPagedImageReady={isPagedImageReady}
                        onPrev={handlePrev}
                        onNext={handleNext}
                        onMouseDown={handleMouseDown}
                        onMouseLeave={handleMouseLeave}
                        onMouseUp={handleMouseUp}
                        onMouseMove={handleMouseMove}
                    />
                )}

                <ReaderProgressTray
                    t={t}
                    showSettings={showSettings}
                    currentPageIndex={currentPageIndex}
                    pageCount={activePages.length}
                    sliderValue={sliderValue}
                    hoverPage={hoverPage}
                    hoverX={hoverX}
                    onSliderChange={setSliderValue}
                    onHoverPageChange={setHoverPage}
                    onHoverXChange={setHoverX}
                    onCommitPage={jumpToPage}
                />
            </div>
        </div>
    );
}

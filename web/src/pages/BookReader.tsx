import React, { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { PagedReader } from './book-reader/PagedReader';
import { ReaderHelpPanel } from './book-reader/ReaderHelpPanel';
import { ReaderProgressTray } from './book-reader/ReaderProgressTray';
import { ReaderSettingsDrawer } from './book-reader/ReaderSettingsDrawer';
import { ReaderErrorState, ReaderEyeProtectionOverlay, ReaderLoadingState } from './book-reader/ReaderStateViews';
import { ReaderTopBar } from './book-reader/ReaderTopBar';
import { WebtoonReader } from './book-reader/WebtoonReader';
import { usePageImageCache } from './book-reader/usePageImageCache';
import { useReaderBookData } from './book-reader/useReaderBookData';
import { useReaderBookmarks } from './book-reader/useReaderBookmarks';
import { useReaderOffline } from './book-reader/useReaderOffline';
import { useReaderPreferences } from './book-reader/useReaderPreferences';
import { useReaderProgressPipeline } from './book-reader/useReaderProgressPipeline';
import { useI18n } from '../i18n/LocaleProvider';

function isReaderShortcutInput(target: EventTarget | null) {
    if (!(target instanceof HTMLElement)) return false;
    const tagName = target.tagName.toLowerCase();
    return tagName === 'input' || tagName === 'textarea' || tagName === 'select' || target.isContentEditable;
}

export default function BookReader() {
    const { t } = useI18n();
    const { bookId } = useParams();
    const navigate = useNavigate();

    // Reading Settings
    // --- 拖拉平移操控状态 ---
    const containerRef = useRef<HTMLDivElement>(null);
    const [isDragging, setIsDragging] = useState(false);
    const [dragStart, setDragStart] = useState({ x: 0, y: 0 });
    const [scrollStart, setScrollStart] = useState({ left: 0, top: 0 });
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
        readModeRef,
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
        setCurrentPageIndex,
        queueOfflineReaderProgress,
    });

    const jumpToPage = useCallback((pageNumber: number) => {
        const targetIndex = Math.max(0, Math.min(activePages.length - 1, pageNumber - 1));
        setSliderValue(targetIndex + 1);
        if (readModeRef.current === 'paged') {
            setCurrentPageIndex(targetIndex);
            return;
        }
        const targetImg = document.querySelector(`img[data-page-number="${targetIndex + 1}"]`);
        if (targetImg) {
            targetImg.scrollIntoView({ behavior: 'auto', block: 'center' });
        }
    }, [activePages.length]);

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

    // ==== 渲染相关计算 ====

    // 页码控制
    const handleNext = useCallback(() => {
        const step = doublePage ? 2 : 1;
        setCurrentPageIndex(prev => {
            if (prev + step >= activePages.length) {
                // 已到最后一页，尝试跳转下一本
                if (nextBookIdRef.current) {
                    setTimeout(() => navigate(`/reader/${nextBookIdRef.current}`, { replace: true }), 0);
                }
                return prev; // 保持当前页不变
            }
            return Math.min(prev + step, activePages.length - 1);
        });
    }, [activePages.length, doublePage, navigate]);

    const handlePrev = useCallback(() => {
        const step = doublePage ? 2 : 1;
        setCurrentPageIndex(prev => Math.max(prev - step, 0));
    }, [doublePage]);

    // 键盘支持
    useEffect(() => {
        if (readMode !== 'paged') return;
        const handleKeyDown = (e: KeyboardEvent) => {
            if (isReaderShortcutInput(e.target)) return;
            if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') {
                e.preventDefault();
                if (readDirection === 'ltr') {
                    handleNext();
                } else {
                    handlePrev();
                }
            } else if (e.key === 'ArrowLeft' || e.key === 'PageUp') {
                e.preventDefault();
                if (readDirection === 'ltr') {
                    handlePrev();
                } else {
                    handleNext();
                }
            } else if (e.key === 'Home') {
                e.preventDefault();
                setCurrentPageIndex(0);
            } else if (e.key === 'End') {
                e.preventDefault();
                setCurrentPageIndex(Math.max(0, activePages.length - 1));
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => window.removeEventListener('keydown', handleKeyDown);
    }, [readMode, readDirection, handleNext, handlePrev, activePages.length]);

    useEffect(() => {
        const handleGlobalHelp = (e: KeyboardEvent) => {
            if (isReaderShortcutInput(e.target)) return;
            if (e.key.toLowerCase() === 'h' || e.key === '?') {
                e.preventDefault();
                setShowHelp(prev => !prev);
            } else if (e.key.toLowerCase() === 'b') {
                e.preventDefault();
                handleSaveBookmark();
            }
        };
        window.addEventListener('keydown', handleGlobalHelp);
        return () => window.removeEventListener('keydown', handleGlobalHelp);
    }, [handleSaveBookmark]);

    // --- 图层鼠标物理拖拽交互方法群 ---
    const handleMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
        if (!containerRef.current) return;
        setIsDragging(true);
        // 记录按下时的原始光标位置及容器目前混动余量
        setDragStart({ x: e.pageX, y: e.pageY });
        setScrollStart({
            left: containerRef.current.scrollLeft,
            top: containerRef.current.scrollTop
        });
    };

    const handleMouseLeave = () => {
        setIsDragging(false);
    };

    const handleMouseUp = () => {
        setIsDragging(false);
    };

    const handleMouseMove = (e: React.MouseEvent<HTMLDivElement>) => {
        if (!isDragging || !containerRef.current) return;
        e.preventDefault();

        // 计算鼠标位移差
        const dx = e.pageX - dragStart.x;
        const dy = e.pageY - dragStart.y;

        // 按照物理相反方向拨动纸张滚动条（向左推光标，纸往右走；向上滑光标，纸往下掉）
        containerRef.current.scrollLeft = scrollStart.left - dx;
        containerRef.current.scrollTop = scrollStart.top - dy;
    };

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
                    onToggleHelp={() => setShowHelp((value) => !value)}
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
                        t={t}
                        bookId={bookId}
                        pages={activePages}
                        cachedPageImageUrls={cachedPageImageUrls}
                        imageFilter={imageFilter}
                        scaleMode={scaleMode}
                        doublePage={doublePage}
                        nextBookId={nextBookId}
                        getImageUrl={getImageUrl}
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

import { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import type { WebtoonReaderHandle } from './book-reader/WebtoonReader';
import { BaseTheme } from './book-reader/themes/BaseTheme';
import { ComimiTheme } from './book-reader/themes/ComimiTheme';
import type { ReaderThemeProps } from './book-reader/ReaderThemeProps';
import { usePageImageCache } from './book-reader/usePageImageCache';
import { useReaderBookData } from './book-reader/useReaderBookData';
import { useReaderBookmarks } from './book-reader/useReaderBookmarks';
import { useReaderImmersive } from './book-reader/useReaderImmersive';
import { useReaderKeyboardShortcuts } from './book-reader/useReaderKeyboardShortcuts';
import { useReaderOffline } from './book-reader/useReaderOffline';
import { useReaderPageNavigation } from './book-reader/useReaderPageNavigation';
import { useReaderPointerDrag } from './book-reader/useReaderPointerDrag';
import { useReaderPreferences } from './book-reader/useReaderPreferences';
import { useReaderProgressPipeline } from './book-reader/useReaderProgressPipeline';
import { useReaderProgressIndicator } from './book-reader/useReaderProgressIndicator';
import { useReaderSiblings } from './book-reader/useReaderSiblings';
import { useI18n } from '../i18n/LocaleProvider';

export default function BookReader() {
    const { t } = useI18n();
    const { bookId } = useParams();
    const navigate = useNavigate();

    const {
        readerTheme,
        setReaderTheme,
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
    const progressIndicator = useReaderProgressIndicator({
        bookId,
        pagesBookIdRef,
        loading,
        isOnline,
        offlineQueuedPage,
        queueOfflineReaderProgress,
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
    // seriesIdRef.current 在加载书本信息时被命令式赋值，无需进入依赖数组
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
        updateProgress: progressIndicator.updateProgress,
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

    const siblings = useReaderSiblings({
        bookId,
        seriesIdRef,
        bookVolume,
        loading,
    });
    const immersive = useReaderImmersive({
        forcedVisible: showSettings || showHelp,
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

    const themeProps: ReaderThemeProps = {
        t, bookId, pages: activePages, currentPageIndex, cachedPageImageUrls, isPagedImageReady, getImageUrl, ensurePageImageLoaded,
        readerTheme, setReaderTheme, readMode, setReadMode, readDirection, setReadDirection, doublePage, setDoublePage,
        scaleMode, setScaleMode, imageFilter, setImageFilter, autoCrop, setAutoCrop,
        preloadCount, setPreloadCount, readerImageFormat, setReaderImageFormat,
        readerImageQuality, setReaderImageQuality, eyeProtection, setEyeProtection,
        w2xScale, setW2xScale, w2xNoise, setW2xNoise, w2xFormat, setW2xFormat,
        showSettings, setShowSettings, showHelp, setShowHelp, sliderValue, setSliderValue,
        hoverPage, setHoverPage, hoverX, setHoverX, loading, loadError, readerLoading,
        bookTitle, bookVolume, nextBookId, handleBackToSeries, handleOpenBook, toggleHelp,
        setCurrentPageIndex, handleNext, handlePrev, jumpToPage, containerRef, isDragging,
        handleMouseDown, handleMouseLeave, handleMouseUp, handleMouseMove, immersive, siblings,
        webtoonReaderRef, handleWebtoonRenderRangeChange, handleWebtoonRenderedImageCountChange,
        progressIndicator, bookmarks, bookmarkNote, setBookmarkNote, savingBookmark, currentBookmark,
        currentPageNumber, handleSaveBookmark, handleDeleteBookmark, isOnline, offlineSupported,
        offlineStatus, offlineCaching, offlineDeleting, offlineCachedPages, offlineQueuedPage,
        offlineCacheError, handleCacheBookOffline, handleDeleteOfflineBook
    };

    if (readerTheme === 'comimi') {
        return <ComimiTheme {...themeProps} />;
    }

    return <BaseTheme {...themeProps} />;
}

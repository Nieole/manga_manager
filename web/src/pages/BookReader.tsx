/**
 * 业务说明：本文件是业务实现，属于前端阅读器总装页面，负责把书籍数据、图片缓存、阅读偏好、离线缓存和进度同步串成完整阅读体验。
 * 它是分页阅读、条漫阅读、下一卷跳转、书签、快捷键和主题外观的编排层，本身尽量不直接承载底层请求细节。
 * 维护时应关注当前书籍 ID、页面索引、本地缓存 URL 和后端进度写回之间的一致性，避免切书或切模式时串页。
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
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
import { useReaderReadingTime } from './book-reader/useReaderReadingTime';
import { computeReaderBack } from './book-reader/readerNavigation';
import { useReaderSiblings } from './book-reader/useReaderSiblings';
import { useI18n } from '../i18n/LocaleProvider';

export default function BookReader() {
    const { t } = useI18n();
    const { bookId } = useParams();
    const navigate = useNavigate();
    const location = useLocation();
    // 进入阅读器时历史栈里是否还有站内来源页。location.key === 'default' 表示这是会话首个路由（深链/新标签直达）。
    // 用 ref 在首次挂载时定格——阅读器内「自动翻到下一本」用的是 replace（会换 location.key），不应改变此判断。
    const hadInAppHistoryRef = useRef(location.key !== 'default');

    // 阅读器偏好是跨书籍保留的用户配置，后续 hook 都会把这些配置转换为图片 URL 参数、布局模式或交互行为。
    // 新增偏好时需要同时检查设置面板、缓存 key、离线缓存和进度恢复，避免同一页在不同参数下复用旧图。
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

    // 页面级 UI 状态只描述当前阅读会话，不直接写入后端；真正需要持久化的偏好和阅读进度分别交给专用 hook 处理。
    // 这样切换书籍时可以精确重置页码和浮层，同时保留用户长期偏好的阅读模式。
    const [showSettings, setShowSettings] = useState(false);
    const [showHelp, setShowHelp] = useState(false);
    // 分页模式的页码以数组索引为准，写回后端时再转换为实际页号，避免归档页号不连续时出现进度偏移。
    const [currentPageIndex, setCurrentPageIndex] = useState(0);
    // 底部进度条本地状态，用于解耦拖拽 UI 与核心渲染
    const [sliderValue, setSliderValue] = useState(1);
    const [hoverPage, setHoverPage] = useState<number | null>(null);
    const [hoverX, setHoverX] = useState(0);
    const currentBookIdRef = useRef<string | null>(bookId ?? null);
    // 图片缓存 hook 负责把同一本书、同一套图像参数下的 Object URL 生命周期管住。
    // 阅读器只声明“当前窗口需要哪些页”，释放策略由 hook 统一处理，防止长时间阅读后浏览器内存持续增长。
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
    // 书籍数据 hook 是后端阅读接口的入口：先拿页清单和书籍信息，再根据当前书籍身份更新系列跳转、下一卷和标题。
    // currentBookIdRef 用于丢弃过期请求结果，解决快速切书时旧请求覆盖新页面的问题。
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
    // 统计每本书的活跃阅读时长（第 6 项）。仅登录用户会真正落库（后端按 currentUserID 判定）。
    useReaderReadingTime({ bookId });
    const {
        containerRef,
        isDragging,
        handleMouseDown,
        handleMouseLeave,
        handleMouseUp,
        handleMouseMove,
    } = useReaderPointerDrag();

    const handleBackToSeries = useCallback(() => {
        const action = computeReaderBack({
            seriesId: seriesIdRef.current,
            bookVolume,
            hasInAppHistory: hadInAppHistoryRef.current,
        });
        if (action.kind === 'history') {
            navigate(-1); // 浏览器回退，弹出阅读器这一条历史（修复「再返回又回到阅读器」）
        } else if (action.kind === 'series') {
            // 无站内历史（深链/刷新页）：用 replace 覆盖阅读器这一条，确保系列页后面不再残留阅读器，
            // 否则刷新后再返回又会退回阅读器。
            navigate(action.url, { replace: true });
        } else {
            navigate('/', { replace: true });
        }
    // seriesIdRef.current 在加载书本信息时被命令式赋值，无需进入依赖数组
    // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [bookVolume, navigate]);
    const handleOpenBook = useCallback((targetBookId: number) => {
        navigate(`/reader/${targetBookId}`, { replace: true });
    }, [navigate]);
    const toggleHelp = useCallback(() => {
        setShowHelp((value) => !value);
    }, []);
    // 条漫模式按可见区间动态保留前后缓冲页，业务目标是在滚动阅读时保持连续，同时及时释放已经远离视口的图片。
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

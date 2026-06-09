/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import type { ReaderThemeProps } from '../ReaderThemeProps';
import { PagedReader } from '../PagedReader';
import { ReaderHelpPanel } from '../ReaderHelpPanel';
import { ReaderImmersiveShell } from '../ReaderImmersiveShell';
import { ReaderProgressTray } from '../ReaderProgressTray';
import { ReaderSettingsDrawer } from '../ReaderSettingsDrawer';
import { ReaderErrorState, ReaderEyeProtectionOverlay, ReaderLoadingState } from '../ReaderStateViews';
import { ReaderTopBar } from '../ReaderTopBar';
import { WebtoonReader } from '../WebtoonReader';
import { useNavigate } from 'react-router-dom';

export function BaseTheme(props: ReaderThemeProps) {
    const {
        t, bookId, pages, currentPageIndex, cachedPageImageUrls, isPagedImageReady, getImageUrl,
        readMode, setReadMode, readDirection, setReadDirection, doublePage, setDoublePage,
        readerTheme, setReaderTheme,
        scaleMode, setScaleMode, imageFilter, setImageFilter, autoCrop, setAutoCrop,
        preloadCount, setPreloadCount, readerImageFormat, setReaderImageFormat,
        readerImageQuality, setReaderImageQuality, eyeProtection, setEyeProtection,
        w2xScale, setW2xScale, w2xNoise, setW2xNoise, w2xFormat, setW2xFormat,
        showSettings, setShowSettings, showHelp, sliderValue, setSliderValue,
        hoverPage, setHoverPage, hoverX, setHoverX, loading, loadError, readerLoading,
        bookTitle, bookVolume, nextBookId, handleBackToSeries, handleOpenBook, toggleHelp,
        setCurrentPageIndex, handleNext, handlePrev, jumpToPage, containerRef, isDragging,
        handleMouseDown, handleMouseLeave, handleMouseUp, handleMouseMove, immersive, siblings,
        webtoonReaderRef, handleWebtoonRenderRangeChange, handleWebtoonRenderedImageCountChange,
        progressIndicator, bookmarks, bookmarkNote, setBookmarkNote, savingBookmark, currentBookmark,
        currentPageNumber, handleSaveBookmark, handleDeleteBookmark, isOnline, offlineSupported,
        offlineStatus, offlineCaching, offlineDeleting, offlineCachedPages, offlineQueuedPage,
        offlineCacheError, handleCacheBookOffline, handleDeleteOfflineBook
    } = props;
    
    const navigate = useNavigate();

    return (
        <div className="absolute inset-0 bg-komgaDark flex flex-col z-50 overflow-hidden">
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
                        pages={pages}
                        currentPageIndex={currentPageIndex}
                        cachedPageImageUrls={cachedPageImageUrls}
                        imageFilter={imageFilter}
                        scaleMode={scaleMode}
                        doublePage={doublePage}
                        nextBookId={nextBookId}
                        nextBookLabel={siblings.next?.title ?? null}
                        getImageUrl={getImageUrl}
                        onVisiblePageChange={setCurrentPageIndex}
                        onRenderRangeChange={handleWebtoonRenderRangeChange}
                        onRenderedImageCountChange={handleWebtoonRenderedImageCountChange}
                        onOpenNextBook={(targetBookId) => navigate(`/reader/${targetBookId}`, { replace: true })}
                        onCenterTap={immersive.toggle}
                    />
                ) : (
                    <PagedReader
                        pages={pages}
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
                        onCenterTap={immersive.toggle}
                        onMouseDown={handleMouseDown}
                        onMouseLeave={handleMouseLeave}
                        onMouseUp={handleMouseUp}
                        onMouseMove={handleMouseMove}
                    />
                )}
                <ReaderImmersiveShell
                    visible={immersive.visible}
                    onEdgeReveal={immersive.show}
                    topBar={
                        <>
                            <ReaderTopBar
                                t={t}
                                bookTitle={bookTitle}
                                bookVolume={bookVolume}
                                isBookmarked={Boolean(currentBookmark)}
                                savingBookmark={savingBookmark}
                                loading={loading}
                                showHelp={showHelp}
                                showSettings={showSettings}
                                progressStatus={progressIndicator.status}
                                allInVolume={siblings.allInVolume}
                                currentBookId={bookId ? Number(bookId) : null}
                                onOpenBook={handleOpenBook}
                                onBack={handleBackToSeries}
                                onSaveBookmark={handleSaveBookmark}
                                onToggleHelp={toggleHelp}
                                onToggleSettings={() => setShowSettings((value) => !value)}
                            />

                            {showHelp && <ReaderHelpPanel t={t} />}

                            {showSettings && (
                                <ReaderSettingsDrawer
                                    t={t}
                                    readerTheme={readerTheme}
                                    setReaderTheme={setReaderTheme}
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
                                    activePageCount={pages.length}
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
                        </>
                    }
                    tray={
                        <ReaderProgressTray
                            t={t}
                            readDirection={readDirection}
                            currentPageIndex={currentPageIndex}
                            pageCount={pages.length}
                            sliderValue={sliderValue}
                            hoverPage={hoverPage}
                            hoverX={hoverX}
                            prev={siblings.prev}
                            next={siblings.next}
                            onSliderChange={setSliderValue}
                            onHoverPageChange={setHoverPage}
                            onHoverXChange={setHoverX}
                            onCommitPage={jumpToPage}
                            onOpenBook={handleOpenBook}
                        />
                    }
                />
            </div>
        </div>
    );
}

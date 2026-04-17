import React, { useState, useEffect, useCallback, useRef } from 'react';
import axios from 'axios';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, CircleHelp, Loader2, RefreshCw, Settings, ChevronLeft, ChevronRight } from 'lucide-react';
import { getFilterStyle, getPagedImages, getScaleClasses } from './book-reader/helpers';
import { useReaderPreferences } from './book-reader/useReaderPreferences';
import type { ImageFilter, Page, ScaleMode } from './book-reader/types';

export default function BookReader() {
    const { bookId } = useParams();
    const navigate = useNavigate();
    const [pages, setPages] = useState<Page[]>([]);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState<string | null>(null);
    const observer = useRef<IntersectionObserver | null>(null);

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
        eyeProtection,
        setEyeProtection,
        w2xScale,
        setW2xScale,
        w2xNoise,
        setW2xNoise,
        w2xFormat,
        setW2xFormat,
    } = useReaderPreferences();

    // UI State
    const [showSettings, setShowSettings] = useState(false);
    const [showHelp, setShowHelp] = useState(false);
    // Paged mode state
    const [currentPageIndex, setCurrentPageIndex] = useState(0);
    // 底部进度条本地状态，用于解耦拖拽 UI 与核心渲染
    const [sliderValue, setSliderValue] = useState(1);
    const [hoverPage, setHoverPage] = useState<number | null>(null);
    const [hoverX, setHoverX] = useState(0);
    // Book context for navigation
    const seriesIdRef = useRef<number | null>(null);
    const [nextBookId, setNextBookId] = useState<number | null>(null);
    const nextBookIdRef = useRef<number | null>(null);
    const [bookTitle, setBookTitle] = useState<string>('');
    const [bookVolume, setBookVolume] = useState<string>('');
    const pagesBookIdRef = useRef<string | null>(null);

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

    // 回传阅读进度
    const updateProgress = useCallback((pageNumber: number) => {
        if (!bookId || loading) return;
        // 核心保护：如果当前路由的 bookId 与内存中 pages 所属的 ID 不一致，禁止上报
        if (bookId !== pagesBookIdRef.current) return;
        if (pageNumber <= 0) return;
        axios.post(`/api/books/${bookId}/progress`, { page: pageNumber })
            .catch(err => console.error("Failed to update read progress", err));
    }, [bookId, loading]);

    // 获取图像资源 URL（纯净无防抖，以保证跟前方预加载 Preloader 抓取下的缓存完全字面一致击穿 304）
    const getImageUrl = useCallback((pageNum: number) => {
        let url = `/api/pages/${bookId}/${pageNum}`;
        if (imageFilter && imageFilter !== 'none') {
            url += `?filter=${imageFilter}`;
            if (imageFilter === 'waifu2x' || imageFilter === 'realcugan') {
                url += `&w2x_scale=${w2xScale}&w2x_noise=${w2xNoise}&w2x_format=${w2xFormat}`;
            }
        }
        if (autoCrop) {
            url += (url.includes('?') ? '&' : '?') + 'auto_crop=true';
        }
        return url;
    }, [bookId, imageFilter, w2xScale, w2xNoise, w2xFormat]);

    useEffect(() => {
        if (!bookId) return;

        // 切换书籍时重置所有运行时状态
        setPages([]);
        setLoading(true);
        setLoadError(null);
        setCurrentPageIndex(0);
        setNextBookId(null);
        setBookTitle('');

        Promise.all([
            axios.get(`/api/pages/${bookId}`),
            axios.get(`/api/book-info/${bookId}`)
        ]).then(([pagesRes, infoRes]) => {
            const sorted = pagesRes.data.sort((a: Page, b: Page) => a.number - b.number);
            if (sorted.length === 0) {
                setLoadError('当前书籍没有可读取的页面，可能是归档损坏或格式不完整。');
                setLoading(false);
                return;
            }
            setPages(sorted);
            pagesBookIdRef.current = bookId; // 记录这批 pages 属于哪个 bookId

            // 恢复上次阅读进度
            const lastPage = infoRes.data.last_read_page?.Valid ? infoRes.data.last_read_page.Int64 : 1;
            seriesIdRef.current = infoRes.data.series_id || null;
            setBookTitle(infoRes.data.title?.Valid ? infoRes.data.title.String : infoRes.data.name);
            setBookVolume(infoRes.data.volume || '');
            // 获取下一本
            axios.get(`/api/book-next/${bookId}`)
                .then(res => { setNextBookId(res.data.id); nextBookIdRef.current = res.data.id; })
                .catch(() => { setNextBookId(null); nextBookIdRef.current = null; });

            if (lastPage > 0) {
                const safePage = Math.min(lastPage, sorted.length > 0 ? sorted.length : lastPage);
                const targetIdx = safePage - 1;
                setSliderValue(safePage);

                if (readMode === 'paged') {
                    setCurrentPageIndex(Math.max(0, targetIdx));
                } else {
                    setTimeout(() => {
                        const targetImg = document.querySelector(`img[data-page-number="${safePage}"]`);
                        if (targetImg) {
                            targetImg.scrollIntoView({ behavior: 'auto', block: 'start' });
                        }
                    }, 500);
                }
            }

            setLoading(false);
        }).catch(err => {
            console.error("Failed to load book data", err);
            setLoadError(err.response?.data?.error || '阅读器无法加载当前书籍。请检查归档内容、页面解析和文件权限。');
            setLoading(false);
        });
    }, [bookId]);

    // 预加载队列系统 (向后驱取指定页数放入浏览器缓存池) - 增加防抖延迟以防连续翻页/拖拽触发洪峰
    useEffect(() => {
        if (!pages.length || preloadCount <= 0 || loading) return;

        const timer = setTimeout(() => {
            const startIndex = currentPageIndex + (readMode === 'paged' && doublePage ? 2 : 1);
            const endIndex = Math.min(startIndex + preloadCount, pages.length);
            for (let i = startIndex; i < endIndex; i++) {
                const img = new window.Image();
                img.src = getImageUrl(pages[i].number);
            }
        }, 300);

        return () => clearTimeout(timer);
    }, [currentPageIndex, pages, preloadCount, readMode, doublePage, loading, getImageUrl]);

    // 独立视口追踪 (仅 webtoon 瀑布流下生效，Paged 模式通过翻页按钮触发计算)
    useEffect(() => {
        if (loading || pages.length === 0 || readMode !== 'webtoon') return;

        let debounceTimeout: number;
        const options = {
            root: null,
            rootMargin: '0px',
            threshold: 0.5
        };

        observer.current = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const pageNumStr = entry.target.getAttribute('data-page-number');
                    if (pageNumStr) {
                        const pageNum = parseInt(pageNumStr, 10);
                        setCurrentPageIndex(pageNum - 1);
                        clearTimeout(debounceTimeout);
                        debounceTimeout = window.setTimeout(() => {
                            updateProgress(pageNum);
                        }, 1000);
                    }
                }
            });
        }, options);

        // 绑定现存图片
        const imgs = document.querySelectorAll('.ReaderScrollContainer img');
        imgs.forEach(img => observer.current?.observe(img));

        return () => {
            if (observer.current) {
                observer.current.disconnect();
            }
            clearTimeout(debounceTimeout);
        };
    }, [loading, pages, readMode, updateProgress]);

    // Paged 模式翻页延迟上报
    useEffect(() => {
        if (!loading && readMode === 'paged' && pages.length > 0 && bookId === pagesBookIdRef.current) {
            const timer = setTimeout(() => {
                updateProgress(pages[currentPageIndex].number);
            }, 1000); // 停止翻页/拖拽 1s 后上报
            return () => clearTimeout(timer);
        }
    }, [currentPageIndex, readMode, pages, updateProgress, loading, bookId]);

    // 同步 sliderValue 与全局状态（当通过按钮翻页时）
    useEffect(() => {
        setSliderValue(currentPageIndex + 1);
    }, [currentPageIndex]);

    // ==== 渲染相关计算 ====

    // 页码控制
    const handleNext = () => {
        let step = doublePage ? 2 : 1;
        setCurrentPageIndex(prev => {
            if (prev + step >= pages.length) {
                // 已到最后一页，尝试跳转下一本
                if (nextBookIdRef.current) {
                    setTimeout(() => navigate(`/reader/${nextBookIdRef.current}`, { replace: true }), 0);
                }
                return prev; // 保持当前页不变
            }
            return Math.min(prev + step, pages.length - 1);
        });
    };

    const handlePrev = () => {
        let step = doublePage ? 2 : 1;
        setCurrentPageIndex(prev => Math.max(prev - step, 0));
    };

    // 键盘支持
    useEffect(() => {
        if (readMode !== 'paged') return;
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'ArrowRight') {
                readDirection === 'ltr' ? handleNext() : handlePrev();
            } else if (e.key === 'ArrowLeft') {
                readDirection === 'ltr' ? handlePrev() : handleNext();
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        return () => window.removeEventListener('keydown', handleKeyDown);
    }, [readMode, readDirection, doublePage, pages.length]);

    useEffect(() => {
        const handleGlobalHelp = (e: KeyboardEvent) => {
            if (e.key.toLowerCase() === 'h' || e.key === '?') {
                setShowHelp(prev => !prev);
            }
        };
        window.addEventListener('keydown', handleGlobalHelp);
        return () => window.removeEventListener('keydown', handleGlobalHelp);
    }, []);

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
                <div className="flex items-center justify-between w-full relative">
                    <button
                        onClick={handleBackToSeries}
                        className="text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full px-4 py-2 backdrop-blur border border-white/10 shadow-lg shrink-0 z-10"
                    >
                        <ArrowLeft className="w-5 h-5 mr-2" />
                        返回
                    </button>

                    {/* 绝对居中书名 */}
                    <div className="absolute inset-0 flex items-center justify-center pointer-events-none px-32">
                        <span className="text-white font-medium truncate drop-shadow-md text-center">{bookTitle}</span>
                    </div>

                    <div className="flex items-center gap-2 shrink-0 z-10">
                        <button
                            onClick={() => setShowHelp(!showHelp)}
                            className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg ${showHelp ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
                            title="阅读帮助"
                        >
                            <CircleHelp className="w-5 h-5" />
                        </button>
                        <button
                            onClick={() => setShowSettings(!showSettings)}
                            className={`text-white hover:text-komgaPrimary transition flex items-center bg-komgaDark/70 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg ${showSettings ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
                        >
                            <Settings className="w-5 h-5" />
                        </button>
                    </div>
                </div>

                {showHelp && (
                    <div className="self-end mt-3 bg-komgaSurface border border-gray-800 rounded-xl p-4 shadow-2xl w-[90vw] sm:w-80 max-w-sm text-sm text-gray-300 animate-in fade-in slide-in-from-top-4">
                        <div className="space-y-3">
                            <div>
                                <p className="text-xs uppercase tracking-wider text-gray-500 mb-1">快捷操作</p>
                                <p>左右方向键：翻页</p>
                                <p><span className="font-mono">H</span> / <span className="font-mono">?</span>：显示帮助</p>
                            </div>
                            <div>
                                <p className="text-xs uppercase tracking-wider text-gray-500 mb-1">移动端</p>
                                <p>瀑布流直接滚动；翻页模式点击左右两侧区域翻页。</p>
                            </div>
                            <div>
                                <p className="text-xs uppercase tracking-wider text-gray-500 mb-1">排错</p>
                                <p>读取失败时优先检查归档完整性、扫描结果和页面解析。</p>
                            </div>
                        </div>
                    </div>
                )}

                {/* 设置体 */}
                {showSettings && (
                    <div className="self-end mt-4 bg-komgaSurface border border-gray-800 rounded-xl p-4 sm:p-5 shadow-2xl w-[90vw] sm:w-80 max-w-sm text-sm text-gray-300 flex flex-col gap-4 animate-in fade-in slide-in-from-top-4 origin-top-right">
                        <div>
                            <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block border-b border-gray-800 pb-1">阅读模式与排版</span>
                            <div className="flex bg-gray-900 rounded p-1 mb-3">
                                <button className={`flex-1 py-1.5 rounded transition ${readMode === 'webtoon' ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadMode('webtoon')}>瀑布流</button>
                                <button className={`flex-1 py-1.5 rounded transition ${readMode === 'paged' ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadMode('paged')}>翻页</button>
                            </div>

                            {readMode === 'paged' && (
                                <div className="space-y-3">
                                    <div>
                                        <span className="text-[10px] text-gray-500 mb-1 block">单双页排版</span>
                                        <div className="flex bg-gray-900 rounded p-0.5">
                                            <button className={`flex-1 py-1 rounded text-xs transition ${!doublePage ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setDoublePage(false)}>单页居中</button>
                                            <button className={`flex-1 py-1 rounded text-xs transition ${doublePage ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setDoublePage(true)}>支持跨页</button>
                                        </div>
                                    </div>
                                    <div>
                                        <span className="text-[10px] text-gray-500 mb-1 block">阅读方向</span>
                                        <div className="flex bg-gray-900 rounded p-0.5">
                                            <button className={`flex-1 py-1 rounded text-xs transition ${readDirection === 'ltr' ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadDirection('ltr')}>左到右 (漫威)</button>
                                            <button className={`flex-1 py-1 rounded text-xs transition ${readDirection === 'rtl' ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadDirection('rtl')}>右到左 (日漫)</button>
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>

                        <div className="h-px bg-gray-800 my-1"></div>

                        <div>
                            <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">缩放与图像处理</span>
                            <div className="flex bg-gray-900 rounded p-1 mb-3">
                                {['original', 'fit-height', 'fit-width', 'fit-screen'].map(sm => (
                                    <button
                                        key={sm}
                                        className={`flex-1 py-1 rounded transition text-[10px] ${scaleMode === sm ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`}
                                        onClick={() => setScaleMode(sm as ScaleMode)}
                                        title={sm === 'original' ? '原始尺寸' : sm === 'fit-height' ? '符合高度' : sm === 'fit-width' ? '符合宽度' : '符合屏幕'}
                                    >
                                        {sm === 'original' ? '原始' : sm === 'fit-height' ? '等高' : sm === 'fit-width' ? '等宽' : '适屏'}
                                    </button>
                                ))}
                            </div>

                            <select
                                value={imageFilter}
                                onChange={(e) => setImageFilter(e.target.value as ImageFilter)}
                                className="w-full bg-gray-900 border border-gray-700 text-gray-300 text-xs rounded p-2 outline-none cursor-pointer mb-2"
                            >
                                <option value="none">原始图像 (Raw / 无处理)</option>
                                <option value="nearest">相邻像素法 (Nearest / Pixelated)</option>
                                <option value="average">平均像素法 (Average)</option>
                                <option value="bilinear">双线性差值 (Bilinear / Auto)</option>
                                <option value="bicubic">Bicubic (高画质三次插值)</option>
                                <option value="lanczos2">Lanczos2 (分两级 Lanczos)</option>
                                <option value="lanczos3">Lanczos3 (锐利重采样)</option>
                                <option value="mitchell">Mitchell-Netravali (平滑平衡)</option>
                                <option value="bspline">B-Spline (极度平滑/防锯齿)</option>
                                <option value="catmullrom">Catmull-Rom (保留边缘锐度)</option>
                                <option value="waifu2x">Waifu2x 初代二次元重绘 (需本地引擎)</option>
                                <option value="realcugan">Real-CUGAN 次世代超分 (需本地引擎)</option>
                            </select>

                            <button
                                className={`w-full py-2 rounded text-xs transition font-medium border ${autoCrop ? 'bg-komgaPrimary/20 border-komgaPrimary text-komgaPrimary shadow-[0_0_15px_rgba(168,85,247,0.2)]' : 'bg-gray-900 border-gray-700 text-gray-400 hover:border-gray-500'}`}
                                onClick={() => setAutoCrop(!autoCrop)}
                            >
                                {autoCrop ? '✨ 自动裁切已开启' : '开启自动裁切白边 (实验性)'}
                            </button>
                        </div>

                        {(imageFilter === 'waifu2x' || imageFilter === 'realcugan') && (
                            <div className="bg-gray-900/50 p-3 rounded border border-komgaPrimary/30 animate-in fade-in slide-in-from-top-2">
                                <div className="mb-3">
                                    <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider mb-2 flex justify-between">
                                        <span>引擎缩放倍数</span>
                                        <span className="text-komgaPrimary">{w2xScale}x</span>
                                    </span>
                                    <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                                        {[1, 2, 4, 8].map(s => (
                                            <button key={s} className={`flex-1 py-1 rounded transition text-xs font-semibold ${w2xScale === s ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`} onClick={() => setW2xScale(s)}>{s}x</button>
                                        ))}
                                    </div>
                                </div>
                                <div className="mb-3">
                                    <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider mb-2 flex justify-between">
                                        <span>降噪等级 (Noise)</span>
                                        <span className="text-komgaPrimary">{w2xNoise === -1 ? '关闭' : w2xNoise}</span>
                                    </span>
                                    <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                                        {[-1, 0, 1, 2, 3].map(n => (
                                            <button key={n} className={`flex-1 py-1 rounded transition text-xs font-semibold ${w2xNoise === n ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`} onClick={() => setW2xNoise(n)}>{n === -1 ? '关' : n}</button>
                                        ))}
                                    </div>
                                </div>
                                <div>
                                    <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider mb-2 flex justify-between">
                                        <span>输出编码格式</span>
                                        <span className="text-komgaPrimary uppercase text-[10px]">{w2xFormat}</span>
                                    </span>
                                    <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                                        {['webp', 'png', 'jpg'].map(f => (
                                            <button key={f} className={`flex-1 py-1 rounded transition text-xs font-semibold uppercase ${w2xFormat === f ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`} onClick={() => setW2xFormat(f)}>{f}</button>
                                        ))}
                                    </div>
                                </div>
                            </div>
                        )}

                        <div>
                            <div className="flex items-center justify-between mb-1">
                                <span className="text-gray-500 font-semibold uppercase text-[10px] tracking-wider">预缓存页数</span>
                                <span className="text-[10px] text-gray-400">{preloadCount} 页</span>
                            </div>
                            <input
                                type="range"
                                min={0}
                                max={10}
                                step={1}
                                value={preloadCount}
                                onChange={(e) => setPreloadCount(parseInt(e.target.value, 10))}
                                className="w-full accent-komgaPrimary h-1"
                            />
                        </div>

                        <div className="h-px bg-gray-800 my-1"></div>

                        <div>
                            <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">护眼模式</span>
                            <button
                                onClick={() => setEyeProtection(!eyeProtection)}
                                className={`w-full flex items-center justify-between px-3 py-2.5 rounded-lg transition-all ${eyeProtection ? 'bg-amber-900/40 border border-amber-600/40 text-amber-200' : 'bg-gray-900 border border-gray-800 text-gray-400 hover:bg-gray-800'
                                    }`}
                            >
                                <span className="text-xs flex items-center gap-2">
                                    <span className="text-base">{eyeProtection ? '🌙' : '☀️'}</span>
                                    暖色护眼滤镜
                                </span>
                                <span className={`text-[10px] font-medium ${eyeProtection ? 'text-amber-400' : 'text-gray-600'
                                    }`}>{eyeProtection ? '已开启' : '关闭'}</span>
                            </button>
                        </div>
                    </div>
                )}
            </div>

            <div className="flex-1 w-full relative overflow-hidden ReaderScrollContainer">
                {/* 护眼模式滤镜覆盖层 */}
                {eyeProtection && (
                    <div className="absolute inset-0 z-30 pointer-events-none" style={{
                        background: 'rgba(255, 180, 50, 0.12)',
                        mixBlendMode: 'multiply'
                    }} />
                )}
                {loading ? (
                    <div className="flex items-center justify-center h-full">
                        <Loader2 className="w-10 h-10 animate-spin text-komgaPrimary" />
                    </div>
                ) : loadError ? (
                    <div className="flex h-full items-center justify-center px-6">
                        <div className="max-w-xl rounded-2xl border border-red-500/20 bg-red-500/10 p-6 text-center">
                            <p className="text-lg font-semibold text-white">当前书籍无法读取</p>
                            <p className="mt-3 text-sm leading-7 text-red-100/90">{loadError}</p>
                            <div className="mt-6 flex flex-col sm:flex-row items-center justify-center gap-3">
                                <button
                                    onClick={() => window.location.reload()}
                                    className="inline-flex items-center gap-2 rounded-xl bg-white/10 px-4 py-2 text-sm text-white hover:bg-white/15"
                                >
                                    <RefreshCw className="w-4 h-4" />
                                    重试加载
                                </button>
                                <button
                                    onClick={handleBackToSeries}
                                    className="inline-flex items-center gap-2 rounded-xl bg-komgaPrimary px-4 py-2 text-sm text-white hover:bg-komgaPrimaryHover"
                                >
                                    <ArrowLeft className="w-4 h-4" />
                                    返回系列
                                </button>
                            </div>
                        </div>
                    </div>
                ) : readMode === 'webtoon' ? (
                    <div className="flex flex-col items-center w-full bg-komgaDark relative h-full overflow-y-auto overflow-x-hidden">
                        {pages.map(page => (
                            <img
                                key={page.number}
                                data-page-number={page.number}
                                src={getImageUrl(page.number)}
                                loading="lazy"
                                decoding="async"
                                style={getFilterStyle(imageFilter)}
                                className={getScaleClasses(scaleMode, doublePage, "bg-gray-900 min-h-[50vh] drop-shadow-lg max-w-[100vw]")}
                                alt={`Page ${page.number}`}
                            />
                        ))}
                        {nextBookId && (
                            <button
                                onClick={() => navigate(`/reader/${nextBookId}`, { replace: true })}
                                className="my-10 px-8 py-4 bg-komgaPrimary hover:bg-komgaPrimaryHover text-white font-bold rounded-xl shadow-2xl text-lg transition-all duration-300 hover:scale-105"
                            >
                                ▶ 继续阅读下一本
                            </button>
                        )}
                    </div>
                ) : (
                    <div className="flex items-center justify-center w-full h-full bg-komgaDark relative">
                        {/* 左触控区/按钮 */}
                        <div
                            className="absolute left-0 inset-y-0 w-[20vw] sm:w-1/3 z-10 flex items-center justify-start sm:px-8 cursor-pointer md:hover:bg-white/5 transition opacity-0 md:hover:opacity-100 group"
                            onClick={() => readDirection === 'ltr' ? handlePrev() : handleNext()}
                        >
                            <ChevronLeft className="w-12 h-12 text-white/40 group-hover:text-white/80 drop-shadow-lg transition-colors hidden md:block" />
                        </div>

                        {/* 图像容器 - 根据数量排列并赋予拖拽监听和原生弹性滚动 */}
                        <div
                            ref={containerRef}
                            className={`flex flex-col sm:flex-row items-center justify-center h-full max-w-full overflow-auto ${isDragging ? 'cursor-grabbing' : 'cursor-grab'} ${(scaleMode === 'fit-width' || scaleMode === 'fit-screen') ? 'px-0 w-full' : 'px-8 sm:px-20'} select-none gap-0 ${doublePage ? 'drop-shadow-[0_20px_50px_rgba(0,0,0,0.9)]' : ''}`}
                            onMouseDown={handleMouseDown}
                            onMouseLeave={handleMouseLeave}
                            onMouseUp={handleMouseUp}
                            onMouseMove={handleMouseMove}
                        >
                            {getPagedImages(pages, currentPageIndex, doublePage, readDirection).map((p, idx, arr) => {
                                // 深度修复方案：两张图片各自向中心偏移 -0.5px (逻辑像素) 实现物理重叠
                                // 这能从底层封死亚像素计算导致的黑色缝隙泄露
                                const isSpread = doublePage && arr.length > 1;
                                const overlapStyle: React.CSSProperties = isSpread ? {
                                    marginLeft: idx === 1 ? '-0.5px' : '0',
                                    marginRight: idx === 0 ? '-0.5px' : '0',
                                    zIndex: idx === 0 ? 1 : 0 // 确保层叠不会产生副作用
                                } : {};

                                return (
                                    <img
                                        key={p.number}
                                        src={getImageUrl(p.number)}
                                        className={getScaleClasses(scaleMode, doublePage, !doublePage ? "drop-shadow-2xl" : "max-w-none")}
                                        style={{ ...getFilterStyle(imageFilter), ...overlapStyle }}
                                        alt={`Page ${p.number}`}
                                        draggable={false}
                                    />
                                );
                            })}
                        </div>

                        {/* 右触控区/按钮 */}
                        <div
                            className="absolute right-0 inset-y-0 w-[20vw] sm:w-1/3 z-10 flex items-center justify-end sm:px-8 cursor-pointer md:hover:bg-white/5 transition opacity-0 md:hover:opacity-100 group"
                            onClick={() => readDirection === 'ltr' ? handleNext() : handlePrev()}
                        >
                            <ChevronRight className="w-12 h-12 text-white/40 group-hover:text-white/80 drop-shadow-lg transition-colors hidden md:block" />
                        </div>

                    </div>
                )}

                {/* 底部进度与拖动条悬浮托盘 */}
                <div className={`absolute bottom-0 inset-x-0 bg-gradient-to-t from-komgaDark/90 via-komgaDark/45 to-transparent pb-8 pt-16 px-6 sm:px-12 flex flex-col items-center transition-all duration-300 z-20 ${showSettings ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4 hover:translate-y-0 hover:opacity-100'}`}>
                    <div className="w-full max-w-2xl flex items-center gap-4 bg-komgaDark/70 px-6 py-3 rounded-2xl backdrop-blur border border-white/10 shadow-2xl">
                        <span className="text-white font-medium text-sm whitespace-nowrap w-8 text-right drop-shadow-md">{currentPageIndex + 1}</span>
                        <div className="flex-1 relative h-6 flex items-center group/slider">
                            {hoverPage !== null && (
                                <div
                                    className="absolute bottom-full mb-3 bg-komgaPrimary text-white text-[10px] font-bold py-1 px-2 rounded-md shadow-[0_0_15px_rgba(168,85,247,0.4)] pointer-events-none transform -translate-x-1/2 whitespace-nowrap z-30 animate-in fade-in zoom-in-95 duration-150"
                                    style={{ left: `${hoverX}px` }}
                                >
                                    第 {hoverPage} 页
                                    {/* 小三角 */}
                                    <div className="absolute top-full left-1/2 -translate-x-1/2 border-x-[4px] border-x-transparent border-t-[4px] border-t-komgaPrimary"></div>
                                </div>
                            )}
                            <input
                                type="range"
                                min={1}
                                max={pages.length}
                                value={sliderValue}
                                onChange={(e) => {
                                    const val = parseInt(e.target.value, 10);
                                    setSliderValue(val);
                                }}
                                onMouseMove={(e) => {
                                    const rect = e.currentTarget.getBoundingClientRect();
                                    const x = e.clientX - rect.left;
                                    const percent = x / rect.width;
                                    const page = Math.round(percent * (pages.length - 1)) + 1;
                                    setHoverPage(Math.max(1, Math.min(pages.length, page)));
                                    setHoverX(x);
                                }}
                                onMouseLeave={() => setHoverPage(null)}
                                onMouseUp={(e) => {
                                    const val = parseInt((e.target as HTMLInputElement).value, 10);
                                    if (readMode === 'paged') {
                                        setCurrentPageIndex(val - 1);
                                    } else {
                                        const targetImg = document.querySelector(`img[data-page-number="${val}"]`);
                                        if (targetImg) targetImg.scrollIntoView({ behavior: 'auto', block: 'center' });
                                    }
                                }}
                                onTouchEnd={(e) => {
                                    const val = parseInt((e.target as HTMLInputElement).value, 10);
                                    if (readMode === 'paged') {
                                        setCurrentPageIndex(val - 1);
                                    } else {
                                        const targetImg = document.querySelector(`img[data-page-number="${val}"]`);
                                        if (targetImg) targetImg.scrollIntoView({ behavior: 'auto', block: 'center' });
                                    }
                                }}
                                className="w-full accent-komgaPrimary h-1.5 bg-gray-700/50 rounded-lg appearance-none cursor-pointer"
                            />
                        </div>
                        <span className="text-gray-400 font-medium text-sm whitespace-nowrap w-8 drop-shadow-md">{pages.length}</span>
                    </div>
                </div>
            </div>
        </div>
    );
}

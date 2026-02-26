import React, { useState, useEffect, useCallback, useRef } from 'react';
import axios from 'axios';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, Loader2, Settings, ChevronLeft, ChevronRight } from 'lucide-react';

interface Page {
    number: number;
    width: number;
    height: number;
}

type ReadMode = 'webtoon' | 'paged';
type ReadDirection = 'ltr' | 'rtl';
type ScaleMode = 'original' | 'fit-height' | 'fit-width' | 'fit-screen';
type ImageFilter = 'nearest' | 'average' | 'bilinear' | 'bicubic' | 'lanczos3' | 'waifu2x' | 'ncnn';

// Helper for localStorage
function useStickyState<T>(defaultValue: T, key: string): [T, React.Dispatch<React.SetStateAction<T>>] {
    const [value, setValue] = useState<T>(() => {
        const stickyValue = window.localStorage.getItem(key);
        return stickyValue !== null ? JSON.parse(stickyValue) : defaultValue;
    });
    useEffect(() => {
        window.localStorage.setItem(key, JSON.stringify(value));
    }, [key, value]);
    return [value, setValue];
}

export default function BookReader() {
    const { bookId } = useParams();
    const navigate = useNavigate();
    const [pages, setPages] = useState<Page[]>([]);
    const [loading, setLoading] = useState(true);
    const observer = useRef<IntersectionObserver | null>(null);

    // Reading Settings
    const [readMode, setReadMode] = useStickyState<ReadMode>('webtoon', 'manga_read_mode');
    const [readDirection, setReadDirection] = useStickyState<ReadDirection>('ltr', 'manga_read_direction');

    // --- 拖拉平移操控状态 ---
    const containerRef = useRef<HTMLDivElement>(null);
    const [isDragging, setIsDragging] = useState(false);
    const [dragStart, setDragStart] = useState({ x: 0, y: 0 });
    const [scrollStart, setScrollStart] = useState({ left: 0, top: 0 });
    const [doublePage, setDoublePage] = useStickyState<boolean>(false, 'manga_double_page');
    const [scaleMode, setScaleMode] = useStickyState<ScaleMode>('fit-screen', 'manga_scale_mode');
    const [imageFilter, setImageFilter] = useStickyState<ImageFilter>('bilinear', 'manga_image_filter');
    const [preloadCount, setPreloadCount] = useStickyState<number>(3, 'manga_preload_count');

    // Waifu2x 专属超分配置偏好
    const [w2xScale, setW2xScale] = useStickyState<number>(2, 'manga_waifu2x_scale');
    const [w2xNoise, setW2xNoise] = useStickyState<number>(0, 'manga_waifu2x_noise');

    // UI State
    const [showSettings, setShowSettings] = useState(false);
    // Paged mode state
    const [currentPageIndex, setCurrentPageIndex] = useState(0);
    // Book context for navigation
    const seriesIdRef = useRef<number | null>(null);
    const [nextBookId, setNextBookId] = useState<number | null>(null);
    const nextBookIdRef = useRef<number | null>(null);
    const [bookTitle, setBookTitle] = useState<string>('');
    const [bookVolume, setBookVolume] = useState<string>('');

    // 回传阅读进度
    const updateProgress = useCallback((pageNumber: number) => {
        if (!bookId) return;
        if (pageNumber <= 0) return;
        axios.post(`/api/books/${bookId}/progress`, { page: pageNumber })
            .catch(err => console.error("Failed to update read progress", err));
    }, [bookId]);

    // 获取图像资源 URL（纯净无防抖，以保证跟前方预加载 Preloader 抓取下的缓存完全字面一致击穿 304）
    const getImageUrl = useCallback((pageNum: number) => {
        let url = `/api/pages/${bookId}/${pageNum}?filter=${imageFilter}`;
        if (imageFilter === 'waifu2x') {
            url += `&w2x_scale=${w2xScale}&w2x_noise=${w2xNoise}`;
        }
        return url;
    }, [bookId, imageFilter, w2xScale, w2xNoise]);

    useEffect(() => {
        if (!bookId) return;

        // 切换书籍时重置所有运行时状态
        setPages([]);
        setLoading(true);
        setCurrentPageIndex(0);
        setNextBookId(null);
        setBookTitle('');

        Promise.all([
            axios.get(`/api/pages/${bookId}`),
            axios.get(`/api/book-info/${bookId}`)
        ]).then(([pagesRes, infoRes]) => {
            const sorted = pagesRes.data.sort((a: Page, b: Page) => a.number - b.number);
            setPages(sorted);

            // 恢复上次阅读进度
            const lastPage = infoRes.data.last_read_page?.Valid ? infoRes.data.last_read_page.Int64 : 1;
            seriesIdRef.current = infoRes.data.series_id || null;
            setBookTitle(infoRes.data.title?.Valid ? infoRes.data.title.String : infoRes.data.name);
            setBookVolume(infoRes.data.volume || '');
            // 获取下一本
            axios.get(`/api/book-next/${bookId}`)
                .then(res => { setNextBookId(res.data.id); nextBookIdRef.current = res.data.id; })
                .catch(() => { setNextBookId(null); nextBookIdRef.current = null; });
            if (lastPage > 1) {
                if (readMode === 'paged') {
                    // 页码为 1-based, 数组 index 为 0-based
                    setCurrentPageIndex(lastPage - 1);
                } else {
                    // 对于瀑布流，图片加载完后需滚动（放在宏任务延迟稍等布局挂载）
                    setTimeout(() => {
                        const targetImg = document.querySelector(`img[data-page-number="${lastPage}"]`);
                        if (targetImg) {
                            targetImg.scrollIntoView({ behavior: 'auto', block: 'start' });
                        }
                    }, 500);
                }
            }

            setLoading(false);
        }).catch(err => {
            console.error("Failed to load book data", err);
            setLoading(false);
        });
    }, [bookId]);

    // 预加载队列系统 (向后驱取指定页数放入浏览器缓存池)
    useEffect(() => {
        if (!pages.length || preloadCount <= 0 || loading) return;
        const startIndex = currentPageIndex + (readMode === 'paged' && doublePage ? 2 : 1);
        const endIndex = Math.min(startIndex + preloadCount, pages.length);
        for (let i = startIndex; i < endIndex; i++) {
            const img = new window.Image();
            img.src = getImageUrl(pages[i].number);
        }
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

    // Paged 模式翻页上报
    useEffect(() => {
        if (readMode === 'paged' && pages.length > 0) {
            updateProgress(pages[currentPageIndex].number);
        }
    }, [currentPageIndex, readMode, pages, updateProgress]);

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

    // 提取单页/双页渲染所需的图片数组
    const getPagedImages = () => {
        if (pages.length === 0) return [];
        const current = pages[currentPageIndex];

        if (!doublePage) return [current];

        // 双页模式下尝试捞下一张
        if (currentPageIndex + 1 < pages.length) {
            const next = pages[currentPageIndex + 1];
            // 返回形式遵循 direction，flex-row-reverse 也可以搞定，但手动排组更直白
            return readDirection === 'ltr' ? [current, next] : [next, current];
        }
        return [current];
    };

    // 获取响应式缩放与图像预处理滤镜方案
    const getScaleClasses = (baseClasses: string) => {
        let classes = baseClasses;
        switch (scaleMode) {
            case 'original': classes += ' w-auto h-auto max-w-none max-h-none block'; break;
            case 'fit-width': classes += ' w-screen min-w-full h-auto object-cover block m-0 p-0'; break;
            case 'fit-screen': classes += ' max-w-full max-h-screen object-contain block'; break;
            case 'fit-height':
            default: classes += ' h-screen w-auto object-contain max-h-screen max-w-none block'; break;
        }
        return classes;
    };

    const getFilterStyle = (): React.CSSProperties => {
        switch (imageFilter) {
            case 'nearest': return { imageRendering: 'pixelated' };
            case 'average':
            case 'bilinear': return { imageRendering: 'auto' };
            case 'bicubic':
            case 'lanczos3':
            case 'waifu2x':
            case 'ncnn':
                return { imageRendering: 'high-quality' as any };
            default: return {};
        }
    };

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
        <div className="absolute inset-0 bg-black flex flex-col z-50 overflow-hidden font-sans">
            {/* 顶栏控制面板区悬浮感应 */}
            <div className={`absolute top-0 inset-x-0 h-20 bg-gradient-to-b from-black/90 to-transparent flex flex-col justify-start pt-4 px-6 transition-all duration-300 z-20 ${showSettings ? 'opacity-100 translate-y-0' : 'opacity-0 -translate-y-4 hover:translate-y-0 hover:opacity-100'}`}>
                <div className="flex items-center justify-between w-full relative">
                    <button
                        onClick={() => {
                            if (seriesIdRef.current) {
                                if (bookVolume) {
                                    navigate(`/series/${seriesIdRef.current}?volume=${encodeURIComponent(bookVolume)}`);
                                } else {
                                    navigate(`/series/${seriesIdRef.current}`);
                                }
                            } else {
                                navigate('/');
                            }
                        }}
                        className="text-white hover:text-komgaPrimary transition flex items-center bg-black/60 rounded-full px-4 py-2 backdrop-blur border border-white/10 shadow-lg shrink-0 z-10"
                    >
                        <ArrowLeft className="w-5 h-5 mr-2" />
                        返回
                    </button>

                    {/* 绝对居中书名 */}
                    <div className="absolute inset-0 flex items-center justify-center pointer-events-none px-32">
                        <span className="text-white font-medium truncate drop-shadow-md text-center">{bookTitle}</span>
                    </div>

                    <button
                        onClick={() => setShowSettings(!showSettings)}
                        className={`text-white hover:text-komgaPrimary transition flex items-center bg-black/60 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg shrink-0 z-10 ${showSettings ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
                    >
                        <Settings className="w-5 h-5" />
                    </button>
                </div>

                {/* 设置体 */}
                {showSettings && (
                    <div className="self-end mt-4 bg-komgaSurface border border-gray-800 rounded-xl p-4 sm:p-5 shadow-2xl w-[90vw] sm:w-80 max-w-sm text-sm text-gray-300 flex flex-col gap-4 animate-in fade-in slide-in-from-top-4 origin-top-right">
                        <div>
                            <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">阅读模式</span>
                            <div className="flex bg-gray-900 rounded p-1">
                                <button className={`flex-1 py-1.5 rounded transition ${readMode === 'webtoon' ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadMode('webtoon')}>瀑布流</button>
                                <button className={`flex-1 py-1.5 rounded transition ${readMode === 'paged' ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadMode('paged')}>翻页</button>
                            </div>
                        </div>

                        {readMode === 'paged' && (
                            <>
                                <div>
                                    <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">单双页排版</span>
                                    <div className="flex bg-gray-900 rounded p-1">
                                        <button className={`flex-1 py-1.5 rounded transition ${!doublePage ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setDoublePage(false)}>单页居中</button>
                                        <button className={`flex-1 py-1.5 rounded transition ${doublePage ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setDoublePage(true)}>支持跨页</button>
                                    </div>
                                </div>
                                <div>
                                    <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">翻页与组合方向</span>
                                    <div className="flex bg-gray-900 rounded p-1">
                                        <button className={`flex-1 py-1.5 rounded transition ${readDirection === 'ltr' ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadDirection('ltr')}>左到右 (漫威)</button>
                                        <button className={`flex-1 py-1.5 rounded transition ${readDirection === 'rtl' ? 'bg-gray-700 text-white shadow' : 'hover:bg-gray-800'}`} onClick={() => setReadDirection('rtl')}>右到左 (日漫)</button>
                                    </div>
                                </div>
                            </>
                        )}

                        <div className="h-px bg-gray-800 my-2"></div>

                        <div>
                            <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 block">缩放方式</span>
                            <div className="flex bg-gray-900 rounded p-1">
                                {['original', 'fit-height', 'fit-width', 'fit-screen'].map(sm => (
                                    <button
                                        key={sm}
                                        className={`flex-1 py-1 px-0.5 rounded transition text-xs ${scaleMode === sm ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`}
                                        onClick={() => setScaleMode(sm as ScaleMode)}
                                        title={sm === 'original' ? '原始尺寸' : sm === 'fit-height' ? '符合高度' : sm === 'fit-width' ? '符合宽度' : '符合屏幕'}
                                    >
                                        {sm === 'original' ? '原始' : sm === 'fit-height' ? '等高' : sm === 'fit-width' ? '等宽' : '适屏'}
                                    </button>
                                ))}
                            </div>
                        </div>

                        <div>
                            <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 flex justify-between">
                                <span>插值过滤与缩放算法</span>
                                <span className="text-komgaPrimary">{imageFilter}</span>
                            </span>
                            <select
                                value={imageFilter}
                                onChange={(e) => setImageFilter(e.target.value as ImageFilter)}
                                className="w-full bg-gray-900 border border-gray-700 text-gray-300 text-xs rounded p-2 outline-none cursor-pointer"
                            >
                                <option value="nearest">相邻像素法 (Nearest / Pixelated)</option>
                                <option value="average">平均像素法 (Average)</option>
                                <option value="bilinear">双线性差值 (Bilinear / Auto)</option>
                                <option value="bicubic">Bicubic (高画质重排)</option>
                                <option value="lanczos3">Lanczos3 (需服务端支持)</option>
                                <option value="waifu2x">Waifu2x AI (需服务端支持)</option>
                                <option value="ncnn">ncnn 神经网络 (需服务端支持)</option>
                            </select>
                        </div>

                        {imageFilter === 'waifu2x' && (
                            <div className="bg-gray-900/50 p-3 rounded border border-komgaPrimary/30 animate-in fade-in slide-in-from-top-2">
                                <div className="mb-3">
                                    <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 flex justify-between">
                                        <span>引擎缩放倍数</span>
                                        <span className="text-komgaPrimary">{w2xScale}x</span>
                                    </span>
                                    <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                                        {[1, 2, 4, 8].map(s => (
                                            <button
                                                key={s}
                                                className={`flex-1 py-1 rounded transition text-xs font-semibold ${w2xScale === s ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`}
                                                onClick={() => setW2xScale(s)}
                                            >
                                                {s}x
                                            </button>
                                        ))}
                                    </div>
                                </div>
                                <div>
                                    <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider mb-2 flex justify-between">
                                        <span>降噪等级 (Noise)</span>
                                        <span className="text-komgaPrimary">{w2xNoise === -1 ? '关闭' : w2xNoise}</span>
                                    </span>
                                    <div className="flex bg-gray-900 rounded p-1 border border-gray-800">
                                        {[-1, 0, 1, 2, 3].map(n => (
                                            <button
                                                key={n}
                                                className={`flex-1 py-1 rounded transition text-xs font-semibold ${w2xNoise === n ? 'bg-komgaPrimary text-white shadow' : 'hover:bg-gray-800 text-gray-400'}`}
                                                onClick={() => setW2xNoise(n)}
                                            >
                                                {n === -1 ? '关' : n}
                                            </button>
                                        ))}
                                    </div>
                                </div>
                            </div>
                        )}

                        <div>
                            <div className="flex items-center justify-between mb-2">
                                <span className="text-gray-500 font-semibold uppercase text-xs tracking-wider">提前预缓存页数</span>
                                <span className="text-xs bg-gray-800 px-2 py-0.5 rounded text-gray-300">{preloadCount} 页</span>
                            </div>
                            <input
                                type="range"
                                min={0}
                                max={10}
                                step={1}
                                value={preloadCount}
                                onChange={(e) => setPreloadCount(parseInt(e.target.value, 10))}
                                className="w-full accent-komgaPrimary"
                            />
                        </div>
                    </div>
                )}
            </div>

            <div className="flex-1 w-full relative overflow-hidden ReaderScrollContainer">
                {loading ? (
                    <div className="flex items-center justify-center h-full">
                        <Loader2 className="w-10 h-10 animate-spin text-komgaPrimary" />
                    </div>
                ) : readMode === 'webtoon' ? (
                    <div className="flex flex-col items-center w-full bg-black relative h-full overflow-y-auto overflow-x-hidden">
                        {pages.map(page => (
                            <img
                                key={page.number}
                                data-page-number={page.number}
                                src={getImageUrl(page.number)}
                                loading="lazy"
                                decoding="async"
                                style={getFilterStyle()}
                                className={getScaleClasses("bg-gray-900 min-h-[50vh] drop-shadow-lg max-w-[100vw]")}
                                alt={`Page ${page.number}`}
                            />
                        ))}
                        {/* 瀑布流模式的续卷提示 */}
                        {nextBookId && (
                            <button
                                onClick={() => navigate(`/reader/${nextBookId}`, { replace: true })}
                                className="my-10 px-8 py-4 bg-komgaPrimary hover:bg-purple-600 text-white font-bold rounded-xl shadow-2xl text-lg transition-all duration-300 hover:scale-105"
                            >
                                ▶ 继续阅读下一本
                            </button>
                        )}
                    </div>
                ) : (
                    /* 翻页模式 */
                    <div className="flex items-center justify-center w-full h-full bg-black relative">
                        {/* 左触控区/按钮 */}
                        <div
                            className="absolute left-0 inset-y-0 w-[20vw] sm:w-1/3 z-10 flex items-center justify-start sm:px-8 cursor-pointer hover:bg-white/5 transition opacity-0 hover:opacity-100 group"
                            onClick={() => readDirection === 'ltr' ? handlePrev() : handleNext()}
                        >
                            <ChevronLeft className="w-12 h-12 text-white/40 group-hover:text-white/80 drop-shadow-lg transition-colors" />
                        </div>

                        {/* 图像容器 - 根据数量排列并赋予拖拽监听和原生弹性滚动 */}
                        <div
                            ref={containerRef}
                            className={`flex flex-col sm:flex-row items-center justify-center h-full max-w-full overflow-auto ${isDragging ? 'cursor-grabbing' : 'cursor-grab'} ${scaleMode === 'fit-width' ? 'px-0 w-full' : 'px-12 sm:px-20'} select-none`}
                            onMouseDown={handleMouseDown}
                            onMouseLeave={handleMouseLeave}
                            onMouseUp={handleMouseUp}
                            onMouseMove={handleMouseMove}
                        >
                            {getPagedImages().map((p) => (
                                <img
                                    key={p.number}
                                    src={getImageUrl(p.number)}
                                    className={getScaleClasses("drop-shadow-2xl")}
                                    style={getFilterStyle()}
                                    alt={`Page ${p.number}`}
                                    draggable={false}
                                />
                            ))}
                        </div>

                        {/* 右触控区/按钮 */}
                        <div
                            className="absolute right-0 inset-y-0 w-[20vw] sm:w-1/3 z-10 flex items-center justify-end sm:px-8 cursor-pointer hover:bg-white/5 transition opacity-0 hover:opacity-100 group"
                            onClick={() => readDirection === 'ltr' ? handleNext() : handlePrev()}
                        >
                            <ChevronRight className="w-12 h-12 text-white/40 group-hover:text-white/80 drop-shadow-lg transition-colors" />
                        </div>

                    </div>
                )}

                {/* 底部进度与拖动条悬浮托盘 */}
                <div className={`absolute bottom-0 inset-x-0 bg-gradient-to-t from-black/90 via-black/40 to-transparent pb-8 pt-16 px-6 sm:px-12 flex flex-col items-center transition-all duration-300 z-20 ${showSettings ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4 hover:translate-y-0 hover:opacity-100'}`}>
                    <div className="w-full max-w-2xl flex items-center gap-4 bg-black/60 px-6 py-3 rounded-2xl backdrop-blur border border-white/10 shadow-2xl">
                        <span className="text-white font-medium text-sm whitespace-nowrap w-8 text-right drop-shadow-md">{currentPageIndex + 1}</span>
                        <input
                            type="range"
                            min={1}
                            max={pages.length}
                            value={currentPageIndex + 1}
                            onChange={(e) => {
                                const val = parseInt(e.target.value, 10);
                                if (readMode === 'paged') {
                                    setCurrentPageIndex(val - 1);
                                } else {
                                    const targetImg = document.querySelector(`img[data-page-number="${val}"]`);
                                    if (targetImg) targetImg.scrollIntoView({ behavior: 'auto', block: 'center' });
                                }
                            }}
                            className="flex-1 accent-komgaPrimary h-1.5 bg-gray-700/50 rounded-lg appearance-none cursor-pointer"
                        />
                        <span className="text-gray-400 font-medium text-sm whitespace-nowrap w-8 drop-shadow-md">{pages.length}</span>
                    </div>
                </div>
            </div>
        </div>
    );
}

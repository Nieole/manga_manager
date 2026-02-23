import { useState, useEffect, useRef, useCallback } from 'react';
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
    const [doublePage, setDoublePage] = useStickyState<boolean>(false, 'manga_double_page');

    // UI State
    const [showSettings, setShowSettings] = useState(false);
    // Paged mode state
    const [currentPageIndex, setCurrentPageIndex] = useState(0);
    // Book context for navigation
    const [seriesId, setSeriesId] = useState<string | null>(null);
    const [nextBookId, setNextBookId] = useState<string | null>(null);

    // 回传阅读进度
    const updateProgress = useCallback((pageNumber: number) => {
        if (!bookId) return;
        if (pageNumber <= 0) return;
        axios.post(`/api/books/${bookId}/progress`, { page: pageNumber })
            .catch(err => console.error("Failed to update read progress", err));
    }, [bookId]);

    useEffect(() => {
        if (!bookId) return;

        // 切换书籍时重置所有运行时状态
        setPages([]);
        setLoading(true);
        setCurrentPageIndex(0);
        setNextBookId(null);
        setSeriesId(null);

        Promise.all([
            axios.get(`/api/pages/${bookId}`),
            axios.get(`/api/books/info/${bookId}`)
        ]).then(([pagesRes, infoRes]) => {
            const sorted = pagesRes.data.sort((a: Page, b: Page) => a.number - b.number);
            setPages(sorted);

            // 恢复上次阅读进度
            const lastPage = infoRes.data.last_read_page?.Valid ? infoRes.data.last_read_page.Int64 : 1;
            setSeriesId(infoRes.data.series_id || null);

            // 获取下一本
            axios.get(`/api/books/next/${bookId}`)
                .then(res => setNextBookId(res.data.id))
                .catch(() => setNextBookId(null));
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
        if (currentPageIndex + step >= pages.length) {
            // 已到最后一页，尝试跳转下一本
            if (nextBookId) {
                navigate(`/reader/${nextBookId}`, { replace: true });
            }
            return;
        }
        setCurrentPageIndex(prev => Math.min(prev + step, pages.length - 1));
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

    return (
        <div className="absolute inset-0 bg-black flex flex-col z-50 overflow-hidden font-sans">
            {/* 顶栏控制面板区悬浮感应 */}
            <div className={`absolute top-0 inset-x-0 h-20 bg-gradient-to-b from-black/90 to-transparent flex flex-col justify-start pt-4 px-6 transition-all duration-300 z-20 ${showSettings ? 'opacity-100 translate-y-0' : 'opacity-0 -translate-y-4 hover:translate-y-0 hover:opacity-100'}`}>
                <div className="flex items-center justify-between w-full">
                    <button
                        onClick={() => seriesId ? navigate(`/series/${seriesId}`) : navigate(-1)}
                        className="text-white hover:text-komgaPrimary transition flex items-center bg-black/60 rounded-full px-4 py-2 backdrop-blur border border-white/10 shadow-lg"
                    >
                        <ArrowLeft className="w-5 h-5 mr-2" />
                        退出阅读
                    </button>

                    <button
                        onClick={() => setShowSettings(!showSettings)}
                        className={`text-white hover:text-komgaPrimary transition flex items-center bg-black/60 rounded-full p-2.5 backdrop-blur border border-white/10 shadow-lg ${showSettings ? 'text-komgaPrimary border-komgaPrimary/50' : ''}`}
                    >
                        <Settings className="w-5 h-5" />
                    </button>
                </div>

                {/* 设置体 */}
                {showSettings && (
                    <div className="self-end mt-4 bg-komgaSurface border border-gray-800 rounded-xl p-5 shadow-2xl w-80 text-sm text-gray-300 flex flex-col gap-4 animate-in fade-in slide-in-from-top-4">
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
                    </div>
                )}
            </div>

            <div className="flex-1 w-full relative overflow-hidden ReaderScrollContainer">
                {loading ? (
                    <div className="flex items-center justify-center h-full">
                        <Loader2 className="w-10 h-10 animate-spin text-komgaPrimary" />
                    </div>
                ) : readMode === 'webtoon' ? (
                    /* 瀑布流模式 */
                    <div className="flex flex-col items-center w-full bg-black relative h-full overflow-y-auto pt-16">
                        {pages.map(page => (
                            <img
                                key={page.number}
                                data-page-number={page.number}
                                src={`/api/pages/${bookId}/${page.number}`}
                                loading="lazy"
                                decoding="async"
                                className="w-auto h-auto max-w-full lg:max-w-4xl max-h-screen object-contain mb-2 md:mb-4 bg-gray-900 min-h-[50vh]"
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
                            className="absolute left-0 inset-y-0 w-1/4 sm:w-32 z-10 flex items-center justify-start sm:px-4 cursor-pointer hover:bg-white/5 transition opacity-0 hover:opacity-100"
                            onClick={() => readDirection === 'ltr' ? handlePrev() : handleNext()}
                        >
                            <ChevronLeft className="w-10 h-10 text-white/50 drop-shadow-lg" />
                        </div>

                        {/* 图像容器 - 根据数量排列 */}
                        <div className="flex items-center justify-center h-full max-w-full max-h-full px-12 sm:px-20 select-none">
                            {getPagedImages().map((p) => (
                                <img
                                    key={p.number}
                                    src={`/api/pages/${bookId}/${p.number}`}
                                    className="object-contain max-h-screen max-w-full drop-shadow-2xl h-[95vh]"
                                    alt={`Page ${p.number}`}
                                    draggable={false}
                                />
                            ))}
                        </div>

                        {/* 右触控区/按钮 */}
                        <div
                            className="absolute right-0 inset-y-0 w-1/4 sm:w-32 z-10 flex items-center justify-end sm:px-4 cursor-pointer hover:bg-white/5 transition opacity-0 hover:opacity-100"
                            onClick={() => readDirection === 'ltr' ? handleNext() : handlePrev()}
                        >
                            <ChevronRight className="w-10 h-10 text-white/50 drop-shadow-lg" />
                        </div>

                        {/* 进度底边栏 */}
                        <div className="absolute bottom-6 inset-x-0 flex justify-center pointer-events-none">
                            <span className="bg-black/80 backdrop-blur text-white text-xs font-medium px-3 py-1.5 rounded-full border border-white/10 shadow-lg pointer-events-auto">
                                {currentPageIndex + 1} / {pages.length}
                            </span>
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

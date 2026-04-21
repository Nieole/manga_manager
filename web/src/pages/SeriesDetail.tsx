import { useState, useEffect, useMemo } from 'react';
import axios from 'axios';
import { useParams, Link, useNavigate, useOutletContext, useLocation } from 'react-router-dom';
import { AlertTriangle, BookImage, CheckCircle2, RotateCcw } from 'lucide-react';
import AddToCollectionModal from '../components/AddToCollectionModal';
import { SeriesContentSection } from './series-detail/SeriesContentSection';
import { SeriesHeader } from './series-detail/SeriesHeader';
import { SeriesMetadataEditorModal } from './series-detail/SeriesMetadataEditorModal';
import { SeriesSearchModal } from './series-detail/SeriesSearchModal';
import type { Author, Book, MetaTag, SearchResult, Series, SeriesLink } from './series-detail/types';

export default function SeriesDetail() {
    const { seriesId } = useParams();
    const navigate = useNavigate();
    const location = useLocation();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
    const [seriesInfo, setSeriesInfo] = useState<Series | null>(null);
    const [tags, setTags] = useState<MetaTag[]>([]);
    const [authors, setAuthors] = useState<Author[]>([]);
    const [books, setBooks] = useState<Book[]>([]);
    const [links, setLinks] = useState<SeriesLink[]>([]);
    const [loading, setLoading] = useState(true);

    const [allTags, setAllTags] = useState<MetaTag[]>([]);
    const [allAuthors, setAllAuthors] = useState<Author[]>([]);

    const [isSelectionMode, setIsSelectionMode] = useState(false);
    const [selectedBooks, setSelectedBooks] = useState<number[]>([]);
    const [selectedVolumes, setSelectedVolumes] = useState<string[]>([]);

    const [isEditing, setIsEditing] = useState(false);
    const [editForm, setEditForm] = useState<Partial<Series> & { tagsInput?: string[], authorsInput?: { name: string, role: string }[], linksInput?: { name: string, url: string }[] }>({});
    const [lockedFields, setLockedFields] = useState<Set<string>>(new Set());

    // 当前如果是阅读某个卷下的内容，记录被选中的卷名
    const [selectedVolume, setSelectedVolume] = useState<string | null>(null);
    const [isScraping, setIsScraping] = useState(false);
    const [isRescanning, setIsRescanning] = useState(false);
    const [isOpeningDirectory, setIsOpeningDirectory] = useState(false);
    const [scrapeMenuOpen, setScrapeMenuOpen] = useState(false);
    const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

    const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
    const [showSearchModal, setShowSearchModal] = useState(false);
    const [showCollectionModal, setShowCollectionModal] = useState(false);
    const [searchProvider, setSearchProvider] = useState('');
    const [modalSearchQuery, setModalSearchQuery] = useState('');
    const [currentOffset, setCurrentOffset] = useState(0);
    const [searchTotal, setSearchTotal] = useState(0);
    const [selectedSearchResult, setSelectedSearchResult] = useState<SearchResult | null>(null);
    const [relatedFailedTasks, setRelatedFailedTasks] = useState<Array<{
        key: string;
        type: string;
        scope_name?: string;
        message: string;
        error?: string;
        retryable: boolean;
        updated_at: string;
    }>>([]);
    const [retryingTaskKey, setRetryingTaskKey] = useState<string | null>(null);

    const showToast = (text: string, type: 'success' | 'error') => {
        setToastMsg({ text, type });
        setTimeout(() => setToastMsg(null), 3000);
    };

    const handleRescan = async () => {
        if (!seriesId) return;
        setIsRescanning(true);
        try {
            await axios.post(`/api/series/${seriesId}/rescan`);
            showToast('已下发重新扫描指令', 'success');
            setTimeout(() => window.location.reload(), 2000);
        } catch (err: any) {
            showToast('重新扫描抛出异常: ' + (err.response?.data?.error || err.message), 'error');
        } finally {
            setIsRescanning(false);
        }
    };

    const handleOpenDirectory = async () => {
        if (!seriesId) return;
        setIsOpeningDirectory(true);
        try {
            await axios.post(`/api/series/${seriesId}/open-dir`);
            showToast('已在系统文件管理器中打开该系列目录', 'success');
        } catch (err: any) {
            showToast(err.response?.data?.error || '打开系列目录失败', 'error');
        } finally {
            setIsOpeningDirectory(false);
        }
    };

    const handleScrape = async (providerKey: string) => {
        if (!seriesId) return;
        setScrapeMenuOpen(false);

        // 如果是 Bangumi，执行搜索预览逻辑
        if (providerKey === 'bangumi') {
            setIsScraping(true);
            try {
                const res = await axios.get(`/api/series/${seriesId}/scrape-search?provider=${providerKey}`);
                
                setSearchProvider(providerKey);
                setModalSearchQuery(seriesInfo?.title?.Valid && seriesInfo.title.String ? seriesInfo.title.String : (seriesInfo?.name || ''));
                setShowSearchModal(true);

                if (res.data.results && res.data.results.length > 0) {
                    setSearchResults(res.data.results);
                    setSelectedSearchResult(res.data.results[0]);
                } else {
                    setSearchResults([]);
                    setSelectedSearchResult(null);
                    showToast('未自动匹配到结果，请尝试手工更改关键词搜索', 'error');
                }
            } catch (err: any) {
                showToast('搜索失败: ' + (err.response?.data?.error || err.message), 'error');
            } finally {
                setIsScraping(false);
            }
            return;
        }

        // 其他（如 Ollama）维持原有的全自动刮削
        setIsScraping(true);
        try {
            const res = await axios.post(`/api/series/${seriesId}/scrape`, { provider: providerKey });
            if (res.data.scraped) {
                showToast(`[${res.data.provider}] ${res.data.message}`, 'success');
                setTimeout(() => window.location.reload(), 1500);
            } else {
                showToast(res.data.message || '未找到匹配的元数据', 'error');
            }
        } catch (err: any) {
            showToast('刮削失败: ' + (err.response?.data?.error || err.message), 'error');
        } finally {
            setIsScraping(false);
        }
    };

    const handleApplyMetadata = async (metadata: SearchResult) => {
        if (!seriesId) return;
        setShowSearchModal(false);
        setIsScraping(true);
        try {
            const res = await axios.post(`/api/series/${seriesId}/scrape-apply?provider=${searchProvider}`, metadata);
            if (res.data.success) {
                showToast('已成功应用选定的元数据', 'success');
                setTimeout(() => window.location.reload(), 1500);
            }
        } catch (err: any) {
            showToast('应用元数据失败: ' + (err.response?.data?.error || err.message), 'error');
        } finally {
            setIsScraping(false);
            setSearchResults([]);
            setSelectedSearchResult(null);
        }
    };

    const handleModalReSearch = async (offset = 0) => {
        if (!seriesId || !modalSearchQuery.trim()) return;
        setIsScraping(true);
        setCurrentOffset(offset);
        try {
            const res = await axios.get(`/api/series/${seriesId}/scrape-search?provider=${searchProvider}&q=${encodeURIComponent(modalSearchQuery)}&offset=${offset}`);
            if (res.data.results && res.data.results.length > 0) {
                setSearchResults(res.data.results);
                setSelectedSearchResult(res.data.results[0]);
                setSearchTotal(res.data.total || 0);
                showToast(`找到了 ${res.data.results.length} 条新结果`, 'success');
            } else {
                setSearchResults([]);
                setSelectedSearchResult(null);
                setSearchTotal(0);
                showToast('未找到匹配的条目，请尝试修改关键词', 'error');
            }
        } catch (err: any) {
            showToast('搜索失败: ' + (err.response?.data?.error || err.message), 'error');
        } finally {
            setIsScraping(false);
        }
    };

    const handleBulkProgressUpdate = async (isRead: boolean, ids?: number[]) => {
        const targetIds = ids || (() => {
            // 合并直接选中的书籍 ID 和 选中卷下的所有书籍 ID
            const volumeBookIds = volumes
                .filter(v => selectedVolumes.includes(v.name))
                .flatMap(v => v.books.map(b => b.id));
            return Array.from(new Set([...selectedBooks, ...volumeBookIds]));
        })();

        if (targetIds.length === 0) return;

        try {
            await axios.post('/api/books/bulk-progress', {
                book_ids: targetIds,
                is_read: isRead
            });
            if (!ids) {
                setIsSelectionMode(false);
                setSelectedBooks([]);
                setSelectedVolumes([]);
            }
            const res = await axios.get(`/api/books/${seriesId}`);
            setBooks(res.data || []);
            showToast(ids ? "状态已更新" : "批量更新进度成功", 'success');
        } catch (e) {
            console.error("Bulk progress update failed", e, ids);
            showToast("操作失败", 'error');
        }
    };

    const handleSelectAll = () => {
        if (selectedVolume) {
            // 在卷内，选中当前卷的所有书籍
            const allIds = activeVolumeBooks.map(b => b.id);
            const isAllSelected = allIds.every(id => selectedBooks.includes(id));
            if (isAllSelected) {
                setSelectedBooks(prev => prev.filter(id => !allIds.includes(id)));
            } else {
                setSelectedBooks(prev => Array.from(new Set([...prev, ...allIds])));
            }
        } else {
            // 在顶层，选中所有卷和所有独立书籍
            const allVolNames = volumes.map(v => v.name);
            const allStandaloneIds = standaloneBooks.map(b => b.id);

            const isAllVolsSelected = allVolNames.every(n => selectedVolumes.includes(n));
            const isAllStandaloneSelected = allStandaloneIds.every(id => selectedBooks.includes(id));

            if (isAllVolsSelected && isAllStandaloneSelected) {
                setSelectedVolumes([]);
                setSelectedBooks([]);
            } else {
                setSelectedVolumes(allVolNames);
                setSelectedBooks(allStandaloneIds);
            }
        }
    };

    const handleQuickMarkRead = (e: React.MouseEvent, bookId: number, isRead: boolean) => {
        e.preventDefault();
        e.stopPropagation();
        handleBulkProgressUpdate(isRead, [bookId]);
    };

    const handleQuickMarkVolumeRead = (e: React.MouseEvent, volName: string, isRead: boolean) => {
        e.preventDefault();
        e.stopPropagation();
        const vol = volumes.find(v => v.name === volName);
        if (vol) {
            handleBulkProgressUpdate(isRead, vol.books.map(b => b.id));
        }
    };

    // 解析 URL 上的 volume 返回参
    useEffect(() => {
        const queryParams = new URLSearchParams(location.search);
        const vol = queryParams.get('volume');
        if (vol) {
            setSelectedVolume(vol);
        }
    }, [location.search]);

    useEffect(() => {
        if (seriesId) {
            // 防闪烁：如果是重新刷新且已经有数据，则不显示全屏 loading
            setLoading(!seriesInfo && books.length === 0);
            axios.get(`/api/series/${seriesId}/context`)
                .then(res => {
                    const { series, books, tags, authors, links } = res.data;
                    setBooks(books || []);
                    setSeriesInfo(series);
                    setTags(tags || []);
                    setAuthors(authors || []);
                    setLinks(links || []);

                    if (series) {
                        setLockedFields(new Set(series.locked_fields?.Valid && series.locked_fields.String ? series.locked_fields.String.split(',') : []));
                        setEditForm({
                            title: series.title,
                            summary: series.summary,
                            publisher: series.publisher,
                            status: series.status,
                            rating: series.rating,
                            language: series.language,
                            tagsInput: (tags || []).map((t: MetaTag) => t.name),
                            authorsInput: (authors || []).map((a: Author) => ({ name: a.name, role: a.role })),
                            linksInput: (links || []).map((l: SeriesLink) => ({ name: l.name, url: l.url }))
                        });
                    }
                })
                .catch(err => {
                    console.error("Failed to load series context", err);
                })
                .finally(() => {
                    setLoading(false);
                });
        }
    }, [seriesId, refreshTrigger]);

    useEffect(() => {
        if (!seriesId) return;
        axios.get(`/api/system/tasks?scope=series&scope_id=${seriesId}&status=failed&limit=5`)
            .then((res) => {
                setRelatedFailedTasks(Array.isArray(res.data) ? res.data : []);
            })
            .catch(() => {
                setRelatedFailedTasks([]);
            });
    }, [seriesId, refreshTrigger]);

    const retryTask = async (taskKey: string) => {
        setRetryingTaskKey(taskKey);
        try {
            await axios.post(`/api/system/tasks/${encodeURIComponent(taskKey)}/retry`);
            showToast('任务已重新加入后台队列', 'success');
            const res = await axios.get(`/api/system/tasks?scope=series&scope_id=${seriesId}&status=failed&limit=5`);
            setRelatedFailedTasks(Array.isArray(res.data) ? res.data : []);
        } catch (err: any) {
            showToast(err.response?.data?.error || '任务重试失败', 'error');
        } finally {
            setRetryingTaskKey(null);
        }
    };

    const taskTypeLabel = (type: string) => {
        switch (type) {
            case 'scan_series':
                return '系列扫描';
            case 'scrape':
                return '元数据刮削';
            default:
                return type;
        }
    };

    useEffect(() => {
        if (isEditing) {
            Promise.all([
                axios.get('/api/tags/all').catch(() => ({ data: [] })),
                axios.get('/api/authors/all').catch(() => ({ data: [] }))
            ]).then(([t, a]) => {
                setAllTags(t.data || []);
                setAllAuthors(a.data || []);
            });
        }
    }, [isEditing]);

    const { volumes, standaloneBooks, activeVolumeBooks } = useMemo(() => {
        const volumeMap = new Map<string, Book[]>();
        const standalones: Book[] = [];

        books.forEach(b => {
            if (b.volume && b.volume.trim() !== "") {
                if (!volumeMap.has(b.volume)) {
                    volumeMap.set(b.volume, []);
                }
                volumeMap.get(b.volume)!.push(b);
            } else {
                standalones.push(b);
            }
        });

        const volumeArr = Array.from(volumeMap.entries()).map(([volName, volBooks]) => ({
            name: volName,
            books: volBooks,
            cover_path: volBooks.find(b => b.cover_path?.Valid && b.cover_path?.String)?.cover_path,
            cover_book_id: volBooks.find(b => b.cover_path?.Valid && b.cover_path?.String)?.id,
            total_pages: volBooks.reduce((sum, b) => sum + b.page_count, 0),
            read_pages: volBooks.reduce((sum, b) => sum + (b.last_read_page?.Valid ? b.last_read_page.Int64 : 0), 0)
        }));

        volumeArr.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));

        return {
            volumes: volumeArr,
            standaloneBooks: standalones,
            activeVolumeBooks: selectedVolume ? volumeMap.get(selectedVolume) || [] : []
        };
    }, [books, selectedVolume]);

    const displayCover = useMemo(() => {
        let b;
        if (selectedVolume) {
            b = activeVolumeBooks.find((book) => book.cover_path?.Valid && book.cover_path?.String) || activeVolumeBooks[0];
        } else {
            b = books.find((book) => book.cover_path?.Valid && book.cover_path?.String) || books[0];
        }
        return b ? `/api/covers/${b.id}${b.updated_at ? `?v=${new Date(b.updated_at).getTime()}` : ''}` : null;
    }, [books, selectedVolume, activeVolumeBooks]);

    // 返回导航逻辑：如果在卷内则退回卷列表，在顶层则退回资源库
    const handleBack = () => {
        if (selectedVolume) {
            setSelectedVolume(null);
        } else {
            const libId = books.length > 0 ? books[0].library_id : null;
            if (libId) {
                navigate(`/library/${libId}`);
            } else {
                navigate('/');
            }
        }
    };

    const handleSaveMetadata = async () => {
        if (!seriesInfo) return;
        try {
            await axios.put(`/api/series/info/${seriesId}`, {
                title: editForm.title?.String || '',
                summary: editForm.summary?.String || '',
                publisher: editForm.publisher?.String || '',
                status: editForm.status?.String || '',
                rating: editForm.rating?.Float64 || 0,
                language: editForm.language?.String || '',
                locked_fields: Array.from(lockedFields).join(','),
                tags: editForm.tagsInput || [],
                authors: editForm.authorsInput || [],
                links: editForm.linksInput || []
            });
            // Reload info
            const [infoRes, tagsRes, authorsRes, linksRes] = await Promise.all([
                axios.get(`/api/series/info/${seriesId}`),
                axios.get(`/api/series/${seriesId}/tags`),
                axios.get(`/api/series/${seriesId}/authors`),
                axios.get(`/api/series/${seriesId}/links`),
            ]);
            setSeriesInfo(infoRes.data);
            setTags(tagsRes.data || []);
            setAuthors(authorsRes.data || []);
            setLinks(linksRes.data || []);
            setIsEditing(false);
        } catch (err) {
            console.error("Failed to update metadata", err);
            showToast("保存失败", 'error');
        }
    };

    const toggleLock = (field: string) => {
        setLockedFields(prev => {
            const next = new Set(prev);
            if (next.has(field)) {
                next.delete(field);
            } else {
                next.add(field);
            }
            return next;
        });
    };

    const handleFormChange = (field: string, value: any) => {
        setEditForm(prev => {
            const next: any = { ...prev };
            if (field === 'rating') {
                next.rating = { Float64: Number(value), Valid: Number(value) > 0 };
            } else if (field === 'tagsInput' || field === 'authorsInput' || field === 'linksInput') {
                next[field] = value;
            } else {
                next[field] = { String: String(value), Valid: String(value).trim() !== '' };
            }
            return next;
        });
        // 自动锁定被随意修改的字段
        setLockedFields(prev => {
            const next = new Set(prev);
            const lockField = field === 'tagsInput' ? 'tags' : (field === 'authorsInput' ? 'authors' : field);
            next.add(lockField);
            return next;
        });
    };

    const renderBookCard = (book: Book) => {
        const isSelected = selectedBooks.includes(book.id);
        const handleCardClick = (e: React.MouseEvent) => {
            if (isSelectionMode) {
                e.preventDefault();
                setSelectedBooks(prev => prev.includes(book.id) ? prev.filter(id => id !== book.id) : [...prev, book.id]);
            }
        };

        return (
            <Link
                to={`/reader/${book.id}`}
                onClick={handleCardClick}
                key={book.id}
                className={`group flex flex-col rounded-xl overflow-hidden bg-komgaSurface border ${isSelected ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20' : 'border-gray-800 hover:border-komgaPrimary/50 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10'} transition-all duration-300 cursor-pointer`}
            >
                <div className="aspect-[3/4] w-full bg-gray-900 border-b border-gray-800 flex items-center justify-center relative overflow-hidden">
                    {isSelectionMode && (
                        <div className="absolute top-2 left-2 z-30">
                            <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'}`}>
                                {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                            </div>
                        </div>
                    )}
                    {book.cover_path?.Valid ? (
                        <img src={`/api/covers/${book.id}${book.updated_at ? `?v=${new Date(book.updated_at).getTime()}` : ''}`} className="absolute inset-0 w-full h-full object-cover transition-transform duration-500 group-hover:scale-105" alt="cover" loading="lazy" />
                    ) : (
                        <BookImage className="w-12 h-12 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
                    )}

                    {/* 快捷标记已读按钮 (非多选模式下显示) */}
                    {!isSelectionMode && (
                        <button
                            onClick={(e) => handleQuickMarkRead(e, book.id, !(book.last_read_page?.Valid && book.last_read_page.Int64 >= book.page_count))}
                            className="absolute top-2 right-2 z-30 p-1.5 rounded-full bg-black/60 border border-white/10 text-white/40 hover:text-green-400 hover:bg-green-400/20 hover:border-green-400/40 transition-all opacity-0 group-hover:opacity-100 backdrop-blur"
                            title={book.last_read_page?.Valid && book.last_read_page.Int64 >= book.page_count ? "标记为未读" : "快速标记为已读"}
                        >
                            <CheckCircle2 className={`w-4 h-4 ${book.last_read_page?.Valid && book.last_read_page.Int64 >= book.page_count ? 'text-green-400 fill-green-400/20' : ''}`} />
                        </button>
                    )}
                    <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent flex items-end p-3 z-10 pointer-events-none">
                        <span className="text-xs font-semibold text-white px-2 py-1 bg-black/60 rounded backdrop-blur drop-shadow-md">
                            {book.page_count} Pages
                        </span>
                    </div>
                    {/* 阅读进度条 */}
                    {book.page_count > 0 && book.last_read_page?.Valid && book.last_read_page.Int64 > 0 && (
                        <div className="absolute inset-x-0 bottom-0 h-1 bg-gray-800/40 z-20">
                            <div
                                className={`h-full transition-all duration-500 ${book.last_read_page.Int64 >= book.page_count ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                                style={{ width: `${Math.min(100, (book.last_read_page.Int64 / book.page_count) * 100)}%` }}
                            />
                        </div>
                    )}
                </div>
                <div className="p-4 flex-1 flex flex-col justify-between">
                    <div>
                        <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-komgaPrimary transition-colors">
                            {book.title?.Valid ? book.title.String : book.name}
                        </h4>
                        {book.last_read_page?.Valid && book.last_read_page.Int64 > 0 && (
                            <div className="mt-2 inline-flex items-center text-xs font-medium text-orange-400 bg-orange-400/10 border border-orange-400/20 px-2 py-0.5 rounded-sm">
                                阅读至 {book.last_read_page.Int64} 页
                            </div>
                        )}
                    </div>
                </div>
            </Link >
        );
    };

    return (
        <div className="relative min-h-screen">
            {displayCover && (
                <>
                    {/* 背景底色 */}
                    <div className="fixed inset-0 z-0 bg-gray-950 pointer-events-none"></div>
                    {/* 模糊图层: 去掉透明度衰减，降低模糊值，让封面的内容更明显 */}
                    <div 
                        className="fixed inset-0 z-0 bg-cover bg-[center_top] bg-no-repeat blur-lg opacity-100 transform scale-105 pointer-events-none" 
                        style={{ backgroundImage: `url("${displayCover}")` }} 
                    />
                    {/* 简单的压暗遮罩：用半透明黑底压暗图片，防止干扰白字 */}
                    <div className="fixed inset-0 z-0 bg-gray-950/70 pointer-events-none"></div>
                    <div className="fixed inset-0 z-0 bg-gradient-to-t from-gray-950 via-gray-950/40 to-transparent pointer-events-none"></div>
                </>
            )}
            <div className="relative z-10 p-6 lg:p-10">
                <SeriesHeader
                    coverUrl={displayCover}
                    selectedVolume={selectedVolume}
                seriesInfo={seriesInfo}
                books={books}
                volumes={volumes}
                standaloneBooks={standaloneBooks}
                activeVolumeBooks={activeVolumeBooks}
                tags={tags}
                authors={authors}
                links={links}
                lockedFields={lockedFields}
                isSelectionMode={isSelectionMode}
                isOpeningDirectory={isOpeningDirectory}
                isRescanning={isRescanning}
                isScraping={isScraping}
                scrapeMenuOpen={scrapeMenuOpen}
                onBack={handleBack}
                onToggleSelectionMode={() => {
                    setIsSelectionMode(!isSelectionMode);
                    setSelectedBooks([]);
                    setSelectedVolumes([]);
                }}
                onEdit={() => setIsEditing(true)}
                onAddToCollection={() => setShowCollectionModal(true)}
                onOpenDirectory={handleOpenDirectory}
                onRescan={handleRescan}
                onToggleScrapeMenu={() => setScrapeMenuOpen(!scrapeMenuOpen)}
                onCloseScrapeMenu={() => setScrapeMenuOpen(false)}
                onScrape={handleScrape}
            />

            <SeriesMetadataEditorModal
                open={isEditing}
                allTags={allTags}
                allAuthors={allAuthors}
                editForm={editForm}
                lockedFields={lockedFields}
                onClose={() => setIsEditing(false)}
                onSave={handleSaveMetadata}
                onToggleLock={toggleLock}
                onFormChange={handleFormChange}
            />

            <SeriesContentSection
                loading={loading}
                selectedVolume={selectedVolume}
                activeVolumeBooks={activeVolumeBooks}
                volumes={volumes}
                standaloneBooks={standaloneBooks}
                books={books}
                isSelectionMode={isSelectionMode}
                selectedVolumes={selectedVolumes}
                seriesUpdatedAt={seriesInfo?.updated_at}
                onSelectVolume={setSelectedVolume}
                onToggleSelectedVolume={(name) =>
                    setSelectedVolumes((prev) => (prev.includes(name) ? prev.filter((item) => item !== name) : [...prev, name]))
                }
                onQuickMarkVolumeRead={handleQuickMarkVolumeRead}
                renderBookCard={renderBookCard}
            />

            {!selectedVolume && relatedFailedTasks.length > 0 && (
                <div className="mt-6 rounded-2xl border border-red-500/20 bg-red-500/10 p-5">
                    <div className="flex items-center gap-2 mb-4 text-red-100">
                        <AlertTriangle className="w-5 h-5" />
                        <h3 className="text-base font-semibold">与当前系列相关的失败任务</h3>
                    </div>
                    <div className="space-y-3">
                        {relatedFailedTasks.map((task) => (
                            <div key={task.key} className="rounded-xl border border-red-500/10 bg-black/20 p-4">
                                <div className="mb-2 flex items-center gap-2 text-xs text-red-100/60">
                                    <span>{taskTypeLabel(task.type)}</span>
                                    {task.scope_name && <span>{task.scope_name}</span>}
                                </div>
                                <p className="text-sm font-medium text-white">{task.message}</p>
                                {task.error && <p className="mt-2 text-sm text-red-100/80">{task.error}</p>}
                                <div className="mt-3 flex items-center justify-between gap-3">
                                    <span className="text-xs text-red-100/60">{new Date(task.updated_at).toLocaleString()}</span>
                                    {task.retryable && (
                                        <button
                                            onClick={() => retryTask(task.key)}
                                            disabled={retryingTaskKey === task.key}
                                            className="inline-flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2 text-xs text-red-100 hover:bg-red-500/15 disabled:opacity-60"
                                        >
                                            <RotateCcw className={`w-3.5 h-3.5 ${retryingTaskKey === task.key ? 'animate-spin' : ''}`} />
                                            重试
                                        </button>
                                    )}
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* 悬浮多选操作栏 */}
            {isSelectionMode && (selectedBooks.length > 0 || selectedVolumes.length > 0) && (
                <div className="fixed bottom-8 left-1/2 -translate-x-1/2 bg-gray-900 border border-gray-700 shadow-[0_20px_50px_-12px_rgba(0,0,0,0.8)] rounded-2xl p-4 w-[90vw] sm:w-auto flex flex-col sm:flex-row items-center gap-4 sm:gap-6 z-50 animate-in slide-in-from-bottom-5">
                    <span className="text-white font-medium text-sm">已选择 {selectedBooks.length + selectedVolumes.length} 项</span>
                    <div className="flex items-center justify-between w-full sm:w-auto gap-3">
                        <button
                            onClick={handleSelectAll}
                            className="bg-komgaPrimary/10 hover:bg-komgaPrimary/20 text-komgaPrimary border border-komgaPrimary/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                        >
                            {((selectedVolume ? activeVolumeBooks.length : (volumes.length + standaloneBooks.length)) === (selectedBooks.length + selectedVolumes.length)) ? '取消全选' : '全选'}
                        </button>
                        <div className="w-px h-6 bg-gray-700 hidden sm:block"></div>
                        <button
                            onClick={() => handleBulkProgressUpdate(true)}
                            className="bg-green-500/10 hover:bg-green-500/20 text-green-500 border border-green-500/30 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                        >
                            标为已读
                        </button>
                        <button
                            onClick={() => handleBulkProgressUpdate(false)}
                            className="bg-gray-800 hover:bg-gray-700 text-gray-300 border border-gray-700 px-4 py-2 rounded-lg text-sm font-medium transition-colors"
                        >
                            标为未读
                        </button>
                    </div>
                </div>
            )}

            <SeriesSearchModal
                open={showSearchModal}
                modalSearchQuery={modalSearchQuery}
                isScraping={isScraping}
                searchResults={searchResults}
                currentOffset={currentOffset}
                searchTotal={searchTotal}
                onClose={() => {
                    setShowSearchModal(false);
                    setSearchResults([]);
                    setSelectedSearchResult(null);
                }}
                providerLabel={searchProvider === 'bangumi' ? 'Bangumi' : searchProvider}
                currentSeries={seriesInfo}
                currentTags={tags}
                lockedFields={lockedFields}
                selectedResult={selectedSearchResult}
                onSelectMetadata={setSelectedSearchResult}
                onSearchQueryChange={setModalSearchQuery}
                onReSearch={handleModalReSearch}
                onApplyMetadata={handleApplyMetadata}
            />

            {/* Toast 通知 */}
            {toastMsg && (
                <div className="fixed bottom-6 right-6 z-50 animate-in slide-in-from-bottom-5 fade-in duration-300">
                    <div className={`px-4 py-3 rounded-lg shadow-lg flex items-center gap-3 border ${toastMsg.type === 'success' ? 'bg-green-900 border-green-700 text-green-100' : 'bg-red-900 border-red-700 text-red-100'
                        }`}>
                        <span className="text-sm font-medium">{toastMsg.text}</span>
                        <button onClick={() => setToastMsg(null)} className="ml-2 text-white/50 hover:text-white">✕</button>
                    </div>
                </div>
            )}

            {/* 添加到合集弹窗 */}
            {showCollectionModal && seriesId && (
                <AddToCollectionModal
                    seriesIds={[Number(seriesId)]}
                    onClose={() => setShowCollectionModal(false)}
                    onSuccess={() => showToast('已成功添加到合集', 'success')}
                />
            )}
        </div>
        </div>
    );
}

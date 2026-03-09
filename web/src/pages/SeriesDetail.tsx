import { useState, useEffect, useMemo } from 'react';
import axios from 'axios';
import { useParams, Link, useNavigate, useOutletContext, useLocation } from 'react-router-dom';
import { ArrowLeft, BookImage, FolderOpen, Star, Tag, User, Globe, Building2, Info, Edit, X, Lock, Unlock, ExternalLink, Download, Pencil, FolderHeart, RefreshCw } from 'lucide-react';
import AddToCollectionModal from '../components/AddToCollectionModal';

interface NullString {
    String: string;
    Valid: boolean;
}

interface NullFloat64 {
    Float64: number;
    Valid: boolean;
}

interface Series {
    id: number;
    name: string;
    library_id: number;
    title?: NullString;
    summary?: NullString;
    publisher?: NullString;
    status?: NullString;
    rating?: NullFloat64;
    language?: NullString;
    book_count: number;
    locked_fields: NullString;
    updated_at?: string;
}

interface MetaTag {
    id: number;
    name: string;
}

interface Author {
    id: number;
    name: string;
    role: string;
}

interface SeriesLink {
    id: number;
    name: string;
    url: string;
}

interface Book {
    id: number;
    name: string;
    library_id: number;
    volume: string;
    title?: NullString;
    summary?: NullString;
    page_count: number;
    last_read_page?: { Valid: boolean; Int64: number };
    cover_path?: NullString;
    updated_at?: string;
}

interface SearchResult {
    Title: string;
    OriginalTitle: string;
    Summary: string;
    Publisher: string;
    CoverURL: string;
    Rating: number;
    Tags: string[];
    SourceID: number;
    ReleaseDate: string;
    VolumeCount: number;
}

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
    const [scrapeMenuOpen, setScrapeMenuOpen] = useState(false);
    const [toastMsg, setToastMsg] = useState<{ text: string; type: 'success' | 'error' } | null>(null);

    const [searchResults, setSearchResults] = useState<SearchResult[]>([]);
    const [showSearchModal, setShowSearchModal] = useState(false);
    const [showCollectionModal, setShowCollectionModal] = useState(false);
    const [searchProvider, setSearchProvider] = useState('');
    const [modalSearchQuery, setModalSearchQuery] = useState('');
    const [currentOffset, setCurrentOffset] = useState(0);
    const [searchTotal, setSearchTotal] = useState(0);

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

    const handleScrape = async (providerKey: string) => {
        if (!seriesId) return;
        setScrapeMenuOpen(false);

        // 如果是 Bangumi，执行搜索预览逻辑
        if (providerKey === 'bangumi') {
            setIsScraping(true);
            try {
                const res = await axios.get(`/api/series/${seriesId}/scrape-search?provider=${providerKey}`);
                if (res.data.results && res.data.results.length > 0) {
                    setSearchResults(res.data.results);
                    setSearchProvider(providerKey);
                    setModalSearchQuery(seriesInfo?.title?.Valid && seriesInfo.title.String ? seriesInfo.title.String : (seriesInfo?.name || ''));
                    setShowSearchModal(true);
                } else {
                    showToast('未找到匹配的条目', 'error');
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
                setSearchTotal(res.data.total || 0);
                showToast(`找到了 ${res.data.results.length} 条新结果`, 'success');
            } else {
                setSearchResults([]);
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
            alert("保存失败");
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

    const AutoCompleteTags = () => {
        const [inputValue, setInputValue] = useState('');
        const currentVals = editForm.tagsInput || [];
        const suggestions = allTags.filter(t => !currentVals.includes(t.name) && t.name.toLowerCase().includes(inputValue.toLowerCase()));

        const addTag = (n: string) => {
            if (n.trim() && !currentVals.includes(n.trim())) {
                handleFormChange('tagsInput', [...currentVals, n.trim()]);
            }
            setInputValue('');
        };

        const removeTag = (n: string) => {
            handleFormChange('tagsInput', currentVals.filter(t => t !== n));
        };

        return (
            <div className="w-full bg-gray-900 border border-gray-700 rounded-lg p-2 text-sm text-white focus-within:ring-2 focus-within:ring-komgaPrimary/50 transition-all">
                <div className="flex flex-wrap gap-2 mb-2">
                    {currentVals.map(t => (
                        <span key={t} className="flex items-center gap-1 bg-komgaPrimary/20 text-komgaPrimary px-2 py-1 rounded text-xs border border-komgaPrimary/30">
                            {t}
                            <button onClick={() => removeTag(t)} className="hover:text-red-400"><X className="w-3 h-3" /></button>
                        </span>
                    ))}
                </div>
                <div className="relative">
                    <input
                        type="text"
                        value={inputValue}
                        onChange={e => setInputValue(e.target.value)}
                        onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addTag(inputValue); } }}
                        placeholder="输入标签按回车添加..."
                        className="w-full bg-transparent border-none outline-none p-1 text-sm placeholder-gray-500"
                    />
                    {inputValue && suggestions.length > 0 && (
                        <div className="absolute top-10 left-0 w-full bg-komgaSurface border border-gray-700 rounded-lg shadow-xl z-20 max-h-40 overflow-y-auto">
                            {suggestions.map(s => (
                                <div key={s.id} onClick={() => addTag(s.name)} className="px-3 py-2 hover:bg-gray-800 cursor-pointer text-gray-300">
                                    {s.name}
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </div>
        );
    };

    const AutoCompleteAuthors = () => {
        const [inputName, setInputName] = useState('');
        const [inputRole, setInputRole] = useState('Writer');
        const currentVals = editForm.authorsInput || [];
        const suggestions = allAuthors.filter(a => !currentVals.find(c => c.name === a.name && c.role === a.role) && a.name.toLowerCase().includes(inputName.toLowerCase()));

        const addAuthor = (n: string, r: string) => {
            if (n.trim() && !currentVals.find(c => c.name === n.trim() && c.role === r)) {
                handleFormChange('authorsInput', [...currentVals, { name: n.trim(), role: r }]);
            }
            setInputName('');
        };

        const removeAuthor = (idx: number) => {
            handleFormChange('authorsInput', currentVals.filter((_, i) => i !== idx));
        };

        return (
            <div className="w-full bg-gray-900 border border-gray-700 rounded-lg p-2 text-sm text-white focus-within:ring-2 focus-within:ring-komgaPrimary/50 transition-all">
                <div className="flex flex-wrap gap-2 mb-2">
                    {currentVals.map((a, idx) => (
                        <span key={idx} className="flex items-center gap-1 bg-gray-800 text-gray-300 px-2 py-1 rounded text-xs border border-gray-700">
                            {a.name} <span className="text-gray-500">[{a.role}]</span>
                            <button onClick={() => removeAuthor(idx)} className="hover:text-red-400 ml-1"><X className="w-3 h-3" /></button>
                        </span>
                    ))}
                </div>
                <div className="flex gap-2 relative">
                    <input
                        type="text"
                        value={inputName}
                        onChange={e => setInputName(e.target.value)}
                        onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); addAuthor(inputName, inputRole); } }}
                        placeholder="输入作者并按回车..."
                        className="flex-1 bg-transparent border border-gray-800 rounded px-2 py-1 outline-none text-sm placeholder-gray-500"
                    />
                    <select
                        value={inputRole}
                        onChange={e => setInputRole(e.target.value)}
                        className="bg-gray-800 border-none outline-none rounded px-2 py-1 text-sm text-gray-300 cursor-pointer"
                    >
                        <option value="Writer">Writer</option>
                        <option value="Penciller">Penciller</option>
                        <option value="Inker">Inker</option>
                        <option value="Colorist">Colorist</option>
                        <option value="Letterer">Letterer</option>
                        <option value="Cover">Cover</option>
                        <option value="Editor">Editor</option>
                    </select>
                    {inputName && suggestions.length > 0 && (
                        <div className="absolute top-10 left-0 w-full bg-komgaSurface border border-gray-700 rounded-lg shadow-xl z-20 max-h-40 overflow-y-auto">
                            {suggestions.map(s => (
                                <div key={s.id + s.role} onClick={() => addAuthor(s.name, s.role)} className="px-3 py-2 hover:bg-gray-800 cursor-pointer flex justify-between text-gray-300">
                                    <span>{s.name}</span>
                                    <span className="text-gray-500 text-xs mt-0.5">{s.role}</span>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </div>
        );
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
                            <Pencil className={`w-4 h-4 ${book.last_read_page?.Valid && book.last_read_page.Int64 >= book.page_count ? 'text-green-400 fill-green-400/20' : ''}`} />
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
        <div className="p-6 lg:p-10">
            <div className="mb-6 flex justify-between items-end border-b border-gray-800 pb-4">
                <div>
                    <button
                        onClick={handleBack}
                        className="flex items-center text-gray-400 hover:text-white transition-colors text-sm font-medium mb-4"
                    >
                        <ArrowLeft className="w-4 h-4 mr-1" />
                        {selectedVolume ? "返回系列总览" : "返回资源库"}
                    </button>
                    <h2 className="text-2xl sm:text-3xl font-bold text-white tracking-tight flex flex-col sm:flex-row sm:items-center gap-4">
                        <div className="flex items-center break-all sm:break-normal">
                            {selectedVolume ? (
                                <>
                                    <FolderOpen className="w-8 h-8 mr-3 text-komgaPrimary" />
                                    {selectedVolume}
                                </>
                            ) : (
                                seriesInfo?.title?.Valid ? seriesInfo.title.String : (seriesInfo?.name || "系列总览")
                            )}
                            {seriesInfo && (
                                <div className="flex flex-wrap items-center gap-2 mt-2 sm:mt-0 w-full sm:w-auto sm:ml-4">
                                    <button
                                        onClick={() => {
                                            setIsSelectionMode(!isSelectionMode);
                                            setSelectedBooks([]);
                                            setSelectedVolumes([]);
                                        }}
                                        className={`flex-1 sm:flex-none px-3 py-1.5 text-sm font-medium rounded-lg transition-colors border focus:outline-none ${isSelectionMode ? 'bg-komgaPrimary border-komgaPrimary text-white shadow-md' : 'bg-transparent border-gray-700 text-gray-400 hover:border-gray-500 hover:text-white'}`}
                                    >
                                        {isSelectionMode ? '取消选择' : '批量操作'}
                                    </button>
                                    {!selectedVolume && (
                                        <>
                                            <button
                                                onClick={() => setIsEditing(true)}
                                                className="p-1.5 text-gray-500 hover:text-komgaPrimary bg-gray-900 border border-gray-800 hover:bg-komgaPrimary/10 rounded transition-colors"
                                                title="编辑元数据"
                                            >
                                                <Edit className="w-5 h-5" />
                                            </button>
                                            <button
                                                onClick={() => setShowCollectionModal(true)}
                                                className="p-1.5 text-gray-500 hover:text-komgaPrimary bg-gray-900 border border-gray-800 hover:bg-komgaPrimary/10 rounded transition-colors"
                                                title="添加到合集"
                                            >
                                                <FolderHeart className="w-5 h-5" />
                                            </button>
                                            <button
                                                onClick={handleRescan}
                                                disabled={isRescanning}
                                                className="p-1.5 text-gray-500 hover:text-blue-400 bg-gray-900 border border-gray-800 hover:bg-blue-400/10 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                                title="重新扫描该系列"
                                            >
                                                <RefreshCw className={`w-5 h-5 ${isRescanning ? 'animate-spin text-blue-400' : ''}`} />
                                            </button>
                                            <div className="relative">
                                                <button
                                                    onClick={() => setScrapeMenuOpen(!scrapeMenuOpen)}
                                                    disabled={isScraping}
                                                    className="p-1.5 text-gray-500 hover:text-green-400 bg-gray-900 border border-gray-800 hover:bg-green-400/10 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                                                    title="刮削元数据"
                                                >
                                                    {isScraping ? (
                                                        <div className="w-5 h-5 animate-spin rounded-full border-2 border-green-400 border-t-transparent" />
                                                    ) : (
                                                        <Download className="w-5 h-5" />
                                                    )}
                                                </button>

                                                {scrapeMenuOpen && !isScraping && (
                                                    <>
                                                        <div
                                                            className="fixed inset-0 z-40"
                                                            onClick={() => setScrapeMenuOpen(false)}
                                                        />
                                                        <div className="absolute right-0 mt-2 w-48 bg-gray-800 border border-gray-700 rounded-lg shadow-xl z-50 overflow-hidden animate-in fade-in zoom-in duration-200">
                                                            <div className="px-3 py-2 text-xs font-semibold text-gray-400 border-b border-gray-700 bg-gray-900">
                                                                选择刮削来源
                                                            </div>
                                                            <button
                                                                onClick={() => handleScrape('bangumi')}
                                                                className="w-full text-left px-4 py-3 text-sm text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors"
                                                            >
                                                                Bangumi (推荐)
                                                            </button>
                                                            <button
                                                                onClick={() => handleScrape('ollama')}
                                                                className="w-full text-left px-4 py-3 text-sm text-gray-200 hover:bg-komgaPrimary hover:text-white transition-colors border-t border-gray-700"
                                                            >
                                                                Ollama LLM
                                                            </button>
                                                        </div>
                                                    </>
                                                )}
                                            </div>
                                        </>
                                    )}
                                </div>
                            )}
                        </div>

                        {/* Rating, Language, Status Badges */}
                        {!selectedVolume && seriesInfo && (
                            <div className="flex flex-wrap items-center gap-2 text-sm font-normal mt-2 sm:mt-0">
                                {seriesInfo.rating?.Valid && (
                                    <span className="flex items-center text-yellow-500 bg-yellow-500/10 px-2.5 py-1 rounded-md border border-yellow-500/20 shadow-sm">
                                        <Star className="w-4 h-4 mr-1 fill-current" />
                                        {seriesInfo.rating.Float64.toFixed(1)}
                                    </span>
                                )}
                                {seriesInfo.status?.Valid && (
                                    <span className="flex items-center text-green-400 bg-green-400/10 px-2.5 py-1 rounded-md border border-green-400/20 shadow-sm">
                                        <Info className="w-4 h-4 mr-1" />
                                        {seriesInfo.status.String}
                                    </span>
                                )}
                                {seriesInfo.language?.Valid && (
                                    <span className="flex items-center text-blue-400 bg-blue-400/10 px-2.5 py-1 rounded-md border border-blue-400/20 shadow-sm uppercase font-semibold tracking-wider">
                                        <Globe className="w-4 h-4 mr-1" />
                                        {seriesInfo.language.String}
                                    </span>
                                )}
                                {seriesInfo.publisher?.Valid && (
                                    <span className="flex items-center text-purple-400 bg-purple-400/10 px-2.5 py-1 rounded-md border border-purple-400/20 shadow-sm">
                                        <Building2 className="w-4 h-4 mr-1" />
                                        {seriesInfo.publisher.String}
                                    </span>
                                )}
                            </div>
                        )}
                    </h2>

                    {!selectedVolume && seriesInfo?.summary?.Valid && (
                        <p className="text-gray-400 mt-5 text-sm leading-relaxed max-w-4xl line-clamp-3 hover:line-clamp-none transition-all cursor-pointer bg-gray-900/50 p-4 rounded-xl border border-gray-800/50 relative group">
                            <span className="absolute -left-2 top-4 w-1 h-1/2 bg-gray-700 rounded-full group-hover:bg-komgaPrimary transition-colors opacity-0 group-hover:opacity-100"></span>
                            {seriesInfo.summary.String}
                        </p>
                    )}

                    {/* External Links */}
                    {!selectedVolume && links.length > 0 && (
                        <div className="mt-5 flex flex-wrap gap-3">
                            {links.map((lnk, idx) => (
                                <a key={idx} href={lnk.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center text-xs font-semibold px-4 py-2 bg-gray-800 hover:bg-komgaPrimary hover:text-white text-gray-300 border border-gray-700/50 hover:border-komgaPrimary/50 rounded-lg transition-all shadow-sm group">
                                    <ExternalLink className="w-3.5 h-3.5 mr-2 opacity-60 group-hover:opacity-100" />
                                    {lnk.name}
                                </a>
                            ))}
                        </div>
                    )}

                    {!selectedVolume && (tags.length > 0 || authors.length > 0) && (
                        <div className="mt-5 flex flex-col gap-3">
                            {authors.length > 0 && (
                                <div className="flex items-start gap-3">
                                    <User className="w-4 h-4 text-gray-500 mt-1 shrink-0" />
                                    <div className="flex flex-wrap gap-2">
                                        {authors.map(a => (
                                            <span key={a.id} className="text-xs text-gray-300 bg-gray-800/80 px-2 py-1 rounded-md border border-gray-700 shadow-sm hover:bg-gray-700 transition-colors">
                                                {a.name} <span className="text-gray-500 ml-1.5 inline-block scale-90">({a.role})</span>
                                            </span>
                                        ))}
                                    </div>
                                </div>
                            )}
                            {tags.length > 0 && (
                                <div className="flex items-start gap-3">
                                    <Tag className="w-4 h-4 text-komgaPrimary/60 mt-1 shrink-0" />
                                    <div className="flex flex-wrap gap-2">
                                        {tags.map(t => (
                                            <span key={t.id} className="text-xs text-komgaPrimary bg-komgaPrimary/10 px-2 py-1 rounded-md border border-komgaPrimary/20 shadow-sm hover:bg-komgaPrimary/20 transition-colors cursor-pointer">
                                                {t.name}
                                            </span>
                                        ))}
                                    </div>
                                </div>
                            )}
                        </div>
                    )}

                    <p className="text-gray-500 mt-6 text-sm font-medium flex items-center gap-2">
                        <div className="w-1.5 h-1.5 rounded-full bg-komgaPrimary/50"></div>
                        {selectedVolume
                            ? `含 ${activeVolumeBooks.length} 话 · 总共 ${activeVolumeBooks.reduce((acc, b) => acc + b.page_count, 0)} 页`
                            : `共 ${books.length} 项资源 (${volumes.length} 卷, ${standaloneBooks.length} 独立册)`
                        }
                    </p>
                </div>
            </div>

            {/* 编辑元数据弹窗 */}
            {isEditing && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/80 backdrop-blur-sm">
                    <div className="bg-komgaSurface border border-gray-800 rounded-2xl w-full max-w-2xl overflow-hidden shadow-2xl flex flex-col max-h-[90vh]">
                        <div className="flex items-center justify-between p-6 border-b border-gray-800 bg-gray-900/50">
                            <h3 className="text-xl font-bold text-white">编辑系列元数据</h3>
                            <button onClick={() => setIsEditing(false)} className="text-gray-400 hover:text-white transition-colors">
                                <X className="w-6 h-6" />
                            </button>
                        </div>
                        <div className="p-6 overflow-y-auto space-y-6 flex-1">
                            {/* Form Fields */}
                            {[
                                { id: 'title', label: '系列标题 (Title)', type: 'text', val: editForm.title?.String || '' },
                                { id: 'summary', label: '简介 (Summary)', type: 'textarea', val: editForm.summary?.String || '' },
                                { id: 'publisher', label: '出版商 (Publisher)', type: 'text', val: editForm.publisher?.String || '' },
                                { id: 'status', label: '连载状态 (Status)', type: 'select', val: editForm.status?.String || '', options: ['已完结', '连载中', '已放弃', '有生之年'] },
                                { id: 'language', label: '语言 (Language ISO)', type: 'text', val: editForm.language?.String || '' },
                                { id: 'rating', label: '评分 (Rating 0-10)', type: 'number', val: editForm.rating?.Float64 || 0, step: "0.1", max: 10 },
                            ].map(f => (
                                <div key={f.id} className="space-y-2">
                                    <div className="flex items-center justify-between">
                                        <label className="text-sm font-medium text-gray-300">{f.label}</label>
                                        <button
                                            onClick={() => toggleLock(f.id)}
                                            className={`flex items-center text-xs px-2 py-1 rounded transition-colors ${lockedFields.has(f.id)
                                                ? 'bg-orange-500/20 text-orange-400 border border-orange-500/30'
                                                : 'text-gray-500 hover:text-gray-300'
                                                }`}
                                            title={lockedFields.has(f.id) ? "该字段已被锁定，扫描时不会被自动覆盖" : "点击锁定该字段，防止被扫描器覆盖"}
                                        >
                                            {lockedFields.has(f.id) ? <><Lock className="w-3 h-3 mr-1" /> 已锁定防覆盖</> : <><Unlock className="w-3 h-3 mr-1" /> 未锁定</>}
                                        </button>
                                    </div>
                                    {f.type === 'textarea' ? (
                                        <textarea
                                            value={f.val}
                                            onChange={e => handleFormChange(f.id, e.target.value)}
                                            className="w-full bg-gray-900 border border-gray-700 rounded-lg p-3 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all min-h-[100px]"
                                        />
                                    ) : f.type === 'select' ? (
                                        <select
                                            value={f.val}
                                            onChange={e => handleFormChange(f.id, e.target.value)}
                                            className="w-full bg-gray-900 border border-gray-700 rounded-lg p-3 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all cursor-pointer"
                                        >
                                            <option value="">- 无状态 -</option>
                                            {f.options?.map(opt => (
                                                <option key={opt} value={opt}>{opt}</option>
                                            ))}
                                        </select>
                                    ) : (
                                        <input
                                            type={f.type}
                                            step={f.step}
                                            max={f.max}
                                            value={f.val}
                                            onChange={e => handleFormChange(f.id, e.target.value)}
                                            className="w-full bg-gray-900 border border-gray-700 rounded-lg p-3 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all"
                                        />
                                    )}
                                </div>
                            ))}
                            {/* Tags Input */}
                            <div className="space-y-2">
                                <div className="flex items-center justify-between">
                                    <label className="text-sm font-medium text-gray-300">标签 (Tags)</label>
                                    <button
                                        onClick={() => toggleLock('tags')}
                                        className={`flex items-center text-xs px-2 py-1 rounded transition-colors ${lockedFields.has('tags')
                                            ? 'bg-orange-500/20 text-orange-400 border border-orange-500/30'
                                            : 'text-gray-500 hover:text-gray-300'
                                            }`}
                                        title={lockedFields.has('tags') ? "已锁定该字段防覆盖" : "点击锁定防覆盖"}
                                    >
                                        {lockedFields.has('tags') ? <><Lock className="w-3 h-3 mr-1" /> 已锁定防覆盖</> : <><Unlock className="w-3 h-3 mr-1" /> 未锁定</>}
                                    </button>
                                </div>
                                <AutoCompleteTags />
                            </div>
                            {/* Authors Input */}
                            <div className="space-y-2">
                                <div className="flex items-center justify-between">
                                    <label className="text-sm font-medium text-gray-300">编绘者 (Authors)</label>
                                    <button
                                        onClick={() => toggleLock('authors')}
                                        className={`flex items-center text-xs px-2 py-1 rounded transition-colors ${lockedFields.has('authors')
                                            ? 'bg-orange-500/20 text-orange-400 border border-orange-500/30'
                                            : 'text-gray-500 hover:text-gray-300'
                                            }`}
                                        title={lockedFields.has('authors') ? "已锁定该字段防覆盖" : "点击锁定防覆盖"}
                                    >
                                        {lockedFields.has('authors') ? <><Lock className="w-3 h-3 mr-1" /> 已锁定防覆盖</> : <><Unlock className="w-3 h-3 mr-1" /> 未锁定</>}
                                    </button>
                                </div>
                                <AutoCompleteAuthors />
                            </div>
                            {/* Links Input */}
                            <div className="space-y-2">
                                <label className="text-sm font-medium text-gray-300">外部链接 (External Links)</label>
                                <div className="space-y-3">
                                    {(editForm.linksInput || []).map((lnk, idx) => (
                                        <div key={idx} className="flex gap-2 items-center">
                                            <input type="text" value={lnk.name} onChange={e => {
                                                const newLinks = [...(editForm.linksInput || [])];
                                                newLinks[idx].name = e.target.value;
                                                handleFormChange('linksInput', newLinks);
                                            }} placeholder="Link Name (e.g. Anilist)" className="flex-1 bg-gray-900 border border-gray-700 rounded p-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-komgaPrimary" />
                                            <input type="text" value={lnk.url} onChange={e => {
                                                const newLinks = [...(editForm.linksInput || [])];
                                                newLinks[idx].url = e.target.value;
                                                handleFormChange('linksInput', newLinks);
                                            }} placeholder="URL" className="flex-[2] bg-gray-900 border border-gray-700 rounded p-2 text-sm text-white focus:outline-none focus:ring-1 focus:ring-komgaPrimary" />
                                            <button onClick={() => {
                                                const newLinks = (editForm.linksInput || []).filter((_, i) => i !== idx);
                                                handleFormChange('linksInput', newLinks);
                                            }} className="p-2 text-red-400 hover:bg-gray-800 rounded"><X className="w-4 h-4" /></button>
                                        </div>
                                    ))}
                                    <button onClick={() => {
                                        const newLinks = [...(editForm.linksInput || []), { name: '', url: '' }];
                                        handleFormChange('linksInput', newLinks);
                                    }} className="text-xs text-komgaPrimary font-medium border border-komgaPrimary/30 bg-komgaPrimary/10 hover:bg-komgaPrimary/20 px-3 py-1.5 rounded transition-colors block w-full text-center">+ 添加外部链接</button>
                                </div>
                            </div>
                        </div>
                        <div className="p-6 border-t border-gray-800 bg-gray-900/50 flex justify-end gap-3">
                            <button
                                onClick={() => setIsEditing(false)}
                                className="px-5 py-2 rounded-lg text-sm font-medium text-gray-300 hover:bg-gray-800 transition-colors"
                            >
                                取消
                            </button>
                            <button
                                onClick={handleSaveMetadata}
                                className="px-5 py-2 rounded-lg text-sm font-medium bg-komgaPrimary text-white hover:bg-komgaPrimary/80 transition-colors shadow-lg shadow-komgaPrimary/20"
                            >
                                保存更改
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {loading ? (
                <div className="text-center py-20 text-gray-500 animate-pulse">正在提取书籍关系元数据...</div>
            ) : selectedVolume ? (
                // 渲染单个卷内的话列表
                <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">
                    {activeVolumeBooks.map(renderBookCard)}
                </div>
            ) : (
                // 渲染顶层（卷文件夹 和 单独书册）
                <div className="space-y-10">
                    {volumes.length > 0 && (
                        <div>
                            <h3 className="text-lg font-semibold text-gray-300 mb-4 flex items-center">
                                <FolderOpen className="w-5 h-5 mr-2 text-komgaPrimary" /> 卷列表
                            </h3>
                            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">
                                {volumes.map(vol => {
                                    const isSelected = selectedVolumes.includes(vol.name);
                                    const handleVolClick = () => {
                                        if (isSelectionMode) {
                                            setSelectedVolumes(prev => prev.includes(vol.name) ? prev.filter(n => n !== vol.name) : [...prev, vol.name]);
                                        } else {
                                            setSelectedVolume(vol.name);
                                        }
                                    };

                                    return (
                                        <div
                                            key={vol.name}
                                            onClick={handleVolClick}
                                            className={`group flex flex-col rounded-xl overflow-hidden bg-gray-900 border ${isSelected ? 'border-komgaPrimary ring-2 ring-komgaPrimary shadow-lg shadow-komgaPrimary/20' : 'border-gray-800 hover:border-komgaPrimary/50 hover:bg-gray-800'} transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer`}
                                        >
                                            <div className="aspect-[3/4] w-full bg-komgaDark flex items-center justify-center relative overflow-hidden">
                                                {isSelectionMode && (
                                                    <div className="absolute top-2 left-2 z-30">
                                                        <div className={`w-5 h-5 rounded-full border-2 flex items-center justify-center transition-colors ${isSelected ? 'bg-komgaPrimary border-komgaPrimary' : 'bg-black/50 border-gray-400'}`}>
                                                            {isSelected && <span className="text-white text-xs font-bold leading-none select-none">✓</span>}
                                                        </div>
                                                    </div>
                                                )}
                                                {vol.cover_path?.Valid && vol.cover_path?.String && vol.cover_book_id ? (
                                                    <img src={`/api/covers/${vol.cover_book_id}${seriesInfo?.updated_at ? `?v=${new Date(seriesInfo.updated_at).getTime()}` : ''}`} className="absolute inset-0 w-full h-full object-cover opacity-80 transition-transform duration-500 group-hover:scale-105" alt="cover" loading="lazy" />
                                                ) : (
                                                    <FolderOpen className="w-16 h-16 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
                                                )}

                                                {/* 快捷按钮：卷标记 */}
                                                {!isSelectionMode && (
                                                    <button
                                                        onClick={(e) => handleQuickMarkVolumeRead(e, vol.name, !(vol.read_pages >= vol.total_pages))}
                                                        className="absolute top-2 right-2 z-30 p-1.5 rounded-full bg-black/60 border border-white/10 text-white/40 hover:text-green-400 hover:bg-green-400/20 hover:border-green-400/40 transition-all opacity-0 group-hover:opacity-100 backdrop-blur"
                                                        title={vol.read_pages >= vol.total_pages ? "将全卷标记为未读" : "将全卷标记为已读"}
                                                    >
                                                        <Pencil className={`w-4 h-4 ${vol.read_pages >= vol.total_pages ? 'text-green-400 fill-green-400/20' : ''}`} />
                                                    </button>
                                                )}
                                                {/* 底部叠加卷信息 */}
                                                <div className="absolute inset-0 bg-gradient-to-t from-gray-900/90 via-gray-900/30 to-transparent flex items-end p-3 z-10 pointer-events-none">
                                                    <div className="w-full flex justify-between items-center text-xs font-semibold text-gray-300">
                                                        <span>{vol.books.length} 话</span>
                                                        <span>{vol.total_pages} 页</span>
                                                    </div>
                                                </div>
                                                {/* 卷进度条 */}
                                                {vol.total_pages > 0 && vol.read_pages > 0 && (
                                                    <div className="absolute inset-x-0 bottom-0 h-1 bg-gray-800/40 z-20">
                                                        <div
                                                            className={`h-full transition-all duration-500 ${vol.read_pages >= vol.total_pages ? 'bg-green-500' : 'bg-komgaPrimary'}`}
                                                            style={{ width: `${Math.min(100, (vol.read_pages / vol.total_pages) * 100)}%` }}
                                                        />
                                                    </div>
                                                )}
                                            </div>
                                            {/* 右上角叠加卷叠层徽章 */}
                                            {!isSelectionMode && (
                                                <div className="absolute top-2 left-2 bg-komgaPrimary/90 text-white text-[10px] uppercase font-bold px-2 py-0.5 rounded shadow-lg opacity-80 group-hover:opacity-100 transition-opacity">
                                                    Volume
                                                </div>
                                            )}
                                            <div className="p-4 flex-1">
                                                <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-komgaPrimary transition-colors">
                                                    {vol.name}
                                                </h4>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    )}

                    {standaloneBooks.length > 0 && (
                        <div>
                            <h3 className="text-lg font-semibold text-gray-300 mb-4 flex items-center">
                                <BookImage className="w-5 h-5 mr-2 text-komgaPrimary" /> 单行本册子
                            </h3>
                            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 sm:gap-6">
                                {standaloneBooks.map(renderBookCard)}
                            </div>
                        </div>
                    )}

                    {books.length === 0 && (
                        <div className="text-center py-20 text-gray-500">此系列尚未包含任何资源</div>
                    )}
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

            {/* 搜索结果选择弹窗 (Bangumi / LLM 预览) */}
            {showSearchModal && (
                <div className="fixed inset-0 z-[60] flex items-center justify-center p-4 bg-black/90 backdrop-blur-md animate-in fade-in duration-300">
                    <div className="bg-komgaSurface border border-gray-800 rounded-3xl w-full max-w-5xl overflow-hidden shadow-2xl flex flex-col max-h-[85vh] scale-in-center animate-in zoom-in-95 duration-300">
                        <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between p-6 border-b border-gray-800 bg-gray-900/50 gap-4">
                            <div className="flex-1 w-full">
                                <h3 className="text-xl font-bold text-white flex items-center gap-3">
                                    <Globe className="w-5 h-5 text-komgaPrimary" />
                                    选择最佳匹配条目
                                </h3>
                                <div className="mt-4 flex gap-2 w-full max-w-xl">
                                    <div className="relative flex-1">
                                        <input
                                            type="text"
                                            value={modalSearchQuery}
                                            onChange={(e) => setModalSearchQuery(e.target.value)}
                                            onKeyDown={(e) => e.key === 'Enter' && handleModalReSearch(0)}
                                            placeholder="输入搜索关键词..."
                                            className="w-full bg-gray-900 border border-gray-700 rounded-xl px-4 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-komgaPrimary/50 transition-all pl-10"
                                        />
                                        <Edit className="w-4 h-4 text-gray-500 absolute left-3 top-1/2 -translate-y-1/2" />
                                    </div>
                                    <button
                                        onClick={() => handleModalReSearch(0)}
                                        disabled={isScraping}
                                        className="bg-komgaPrimary hover:bg-komgaPrimary/80 disabled:opacity-50 text-white px-4 py-2 rounded-xl text-sm font-medium transition-all shadow-lg shadow-komgaPrimary/20 flex items-center gap-2 shrink-0"
                                    >
                                        {isScraping ? (
                                            <div className="w-4 h-4 animate-spin rounded-full border-2 border-white/30 border-t-white" />
                                        ) : (
                                            <Download className="w-4 h-4" />
                                        )}
                                        重新搜索
                                    </button>
                                </div>
                            </div>
                            <button
                                onClick={() => {
                                    setShowSearchModal(false);
                                    setSearchResults([]);
                                }}
                                className="p-2 text-gray-400 hover:text-white hover:bg-gray-800 rounded-full transition-all shrink-0 self-start sm:self-center"
                            >
                                <X className="w-7 h-7" />
                            </button>
                        </div>

                        <div className="p-6 overflow-y-auto space-y-4 flex-1 custom-scrollbar">
                            {searchResults.length > 0 ? searchResults.map((res, idx) => (
                                <div
                                    key={idx}
                                    onClick={() => handleApplyMetadata(res)}
                                    className="group flex gap-6 p-6 rounded-2xl bg-gray-900/40 border border-gray-800 hover:border-komgaPrimary/50 hover:bg-komgaPrimary/5 transition-all cursor-pointer relative overflow-hidden active:scale-[0.99] min-h-[180px]"
                                >
                                    <div className="w-28 sm:w-36 shrink-0 aspect-[3/4] bg-gray-800 rounded-xl overflow-hidden border border-gray-700 shadow-xl self-start">
                                        {res.CoverURL ? (
                                            <img src={res.CoverURL} alt={res.Title} className="w-full h-full object-cover group-hover:scale-110 transition-transform duration-700" />
                                        ) : (
                                            <div className="w-full h-full flex items-center justify-center">
                                                <BookImage className="w-12 h-12 text-gray-700" />
                                            </div>
                                        )}
                                    </div>
                                    <div className="flex-1 min-w-0 flex flex-col justify-start">
                                        <div className="flex justify-between items-start gap-4">
                                            <div className="min-w-0 flex-1">
                                                <h4 className="text-xl font-bold text-white group-hover:text-komgaPrimary transition-colors leading-tight">
                                                    {res.Title}
                                                </h4>
                                                {res.OriginalTitle && res.OriginalTitle !== res.Title && (
                                                    <p className="text-sm text-gray-500 truncate mt-1 italic">{res.OriginalTitle}</p>
                                                )}
                                            </div>
                                            {res.Rating > 0 && (
                                                <div className="flex items-center text-yellow-500 text-sm font-bold shrink-0 bg-yellow-400/10 px-2 py-1 rounded-lg border border-yellow-500/20 shadow-sm">
                                                    <Star className="w-4 h-4 mr-1 fill-current" />
                                                    {res.Rating.toFixed(1)}
                                                </div>
                                            )}
                                        </div>
                                        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 mt-3">
                                            {res.Publisher && (
                                                <p className="text-purple-400 text-xs font-semibold flex items-center gap-1.5 bg-purple-400/5 px-2 py-1 rounded border border-purple-400/10">
                                                    <Building2 className="w-3.5 h-3.5" />
                                                    {res.Publisher}
                                                </p>
                                            )}
                                            {res.ReleaseDate && (
                                                <p className="text-blue-400 text-xs font-semibold flex items-center gap-1.5 bg-blue-400/5 px-2 py-1 rounded border border-blue-400/10">
                                                    <Info className="w-3.5 h-3.5" />
                                                    {res.ReleaseDate}
                                                </p>
                                            )}
                                            {res.VolumeCount > 0 && (
                                                <p className="text-green-400 text-xs font-semibold flex items-center gap-1.5 bg-green-400/5 px-2 py-1 rounded border border-green-400/10">
                                                    <FolderOpen className="w-3.5 h-3.5" />
                                                    {res.VolumeCount} 卷/册
                                                </p>
                                            )}
                                        </div>
                                        <div className="mt-4 flex flex-wrap gap-2">
                                            {res.Tags?.slice(0, 8).map(t => (
                                                <span key={t} className="text-[11px] bg-gray-800/60 text-gray-400 px-2.5 py-1 rounded-full border border-gray-700/50 hover:border-gray-600 transition-colors">
                                                    {t}
                                                </span>
                                            ))}
                                        </div>
                                        <p className="text-gray-400 text-sm mt-4 line-clamp-3 leading-relaxed italic border-l-2 border-komgaPrimary/30 pl-4 py-1">
                                            {res.Summary || '暂无简介...'}
                                        </p>
                                        <div className="mt-6 flex items-center justify-between">
                                            <span className="text-xs text-gray-600 font-mono tracking-wider">SOURCE ID: {res.SourceID}</span>
                                            <span className="text-sm font-bold text-komgaPrimary opacity-0 group-hover:opacity-100 translate-x-4 group-hover:translate-x-0 transition-all duration-300 flex items-center gap-2 bg-komgaPrimary/10 px-4 py-1 rounded-full border border-komgaPrimary/20">
                                                应用当前条目 <ArrowLeft className="w-4 h-4 rotate-180" />
                                            </span>
                                        </div>
                                    </div>
                                    <div className="absolute top-0 right-0 w-24 h-24 bg-komgaPrimary/5 -translate-y-12 translate-x-12 rotate-45 group-hover:translate-x-8 group-hover:-translate-y-8 transition-transform duration-700"></div>
                                </div>
                            )) : (
                                <div className="flex flex-col items-center justify-center py-20 text-gray-500 gap-4">
                                    <Globe className="w-16 h-16 opacity-20" />
                                    <p>未找到匹配条目，请尝试修改关键词重新搜索</p>
                                </div>
                            )}
                        </div>

                        <div className="p-6 border-t border-gray-800 bg-gray-900/50 flex flex-col sm:flex-row items-center justify-between gap-4">
                            <div className="flex items-center gap-3">
                                <button
                                    onClick={() => handleModalReSearch(Math.max(0, currentOffset - 20))}
                                    disabled={isScraping || currentOffset === 0}
                                    className="px-4 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-30 rounded-xl text-sm text-gray-300 transition-colors"
                                >
                                    上一页
                                </button>
                                <span className="text-gray-500 text-sm">
                                    第 {Math.floor(currentOffset / 20) + 1} / {Math.max(1, Math.ceil(searchTotal / 20))} 页
                                </span>
                                <button
                                    onClick={() => handleModalReSearch(currentOffset + 20)}
                                    disabled={isScraping || currentOffset + 20 >= searchTotal}
                                    className="px-4 py-2 bg-gray-800 hover:bg-gray-700 disabled:opacity-30 rounded-xl text-sm text-gray-300 transition-colors"
                                >
                                    下一页
                                </button>
                            </div>
                            <p className="text-gray-500 text-xs flex items-center gap-2 italic">
                                <Info className="w-4 h-4" />
                                请点击匹配最准确的条目以更新当前系列的元数据
                            </p>
                        </div>
                    </div>
                </div>
            )}

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
    );
}

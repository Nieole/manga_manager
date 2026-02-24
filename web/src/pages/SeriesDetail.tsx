import { useState, useEffect, useMemo } from 'react';
import axios from 'axios';
import { useParams, Link, useNavigate, useOutletContext } from 'react-router-dom';
import { ArrowLeft, BookImage, FolderOpen, Star, Tag, User, Globe, Building2, Info, Edit, X, Lock, Unlock } from 'lucide-react';

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
}

export default function SeriesDetail() {
    const { seriesId } = useParams();
    const navigate = useNavigate();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
    const [seriesInfo, setSeriesInfo] = useState<Series | null>(null);
    const [tags, setTags] = useState<MetaTag[]>([]);
    const [authors, setAuthors] = useState<Author[]>([]);
    const [books, setBooks] = useState<Book[]>([]);
    const [loading, setLoading] = useState(true);

    const [allTags, setAllTags] = useState<MetaTag[]>([]);
    const [allAuthors, setAllAuthors] = useState<Author[]>([]);

    const [isEditing, setIsEditing] = useState(false);
    const [editForm, setEditForm] = useState<Partial<Series> & { tagsInput?: string[], authorsInput?: { name: string, role: string }[] }>({});
    const [lockedFields, setLockedFields] = useState<Set<string>>(new Set());

    // 当前如果是阅读某个卷下的内容，记录被选中的卷名
    const [selectedVolume, setSelectedVolume] = useState<string | null>(null);

    useEffect(() => {
        if (seriesId) {
            setLoading(true);
            Promise.all([
                axios.get(`/api/books/${seriesId}`),
                axios.get(`/api/series/info/${seriesId}`),
                axios.get(`/api/series/${seriesId}/tags`),
                axios.get(`/api/series/${seriesId}/authors`),
            ])
                .then(([booksRes, infoRes, tagsRes, authorsRes]) => {
                    setBooks(booksRes.data || []);
                    const info = infoRes.data;
                    setSeriesInfo(info);

                    const tagsData = tagsRes.data || [];
                    const authorsData = authorsRes.data || [];
                    setTags(tagsData);
                    setAuthors(authorsData);

                    if (info) {
                        setLockedFields(new Set(info.locked_fields?.Valid && info.locked_fields.String ? info.locked_fields.String.split(',') : []));
                        setEditForm({
                            title: info.title,
                            summary: info.summary,
                            publisher: info.publisher,
                            status: info.status,
                            rating: info.rating,
                            language: info.language,
                            tagsInput: tagsData.map((t: MetaTag) => t.name),
                            authorsInput: authorsData.map((a: Author) => ({ name: a.name, role: a.role }))
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
            total_pages: volBooks.reduce((sum, b) => sum + b.page_count, 0)
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
                authors: editForm.authorsInput || []
            });
            // Reload info
            const [infoRes, tagsRes, authorsRes] = await Promise.all([
                axios.get(`/api/series/info/${seriesId}`),
                axios.get(`/api/series/${seriesId}/tags`),
                axios.get(`/api/series/${seriesId}/authors`),
            ]);
            setSeriesInfo(infoRes.data);
            setTags(tagsRes.data || []);
            setAuthors(authorsRes.data || []);
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
            } else if (field === 'tagsInput' || field === 'authorsInput') {
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

    const renderBookCard = (book: Book) => (
        <Link
            to={`/reader/${book.id}`}
            key={book.id}
            className="group flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary/50 transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer"
        >
            <div className="aspect-[3/4] w-full bg-gray-900 border-b border-gray-800 flex items-center justify-center relative overflow-hidden">
                {book.cover_path?.Valid ? (
                    <img src={`/api/covers/${book.id}`} className="absolute inset-0 w-full h-full object-cover transition-transform duration-500 group-hover:scale-105" alt="cover" loading="lazy" />
                ) : (
                    <BookImage className="w-12 h-12 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
                )}
                <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-transparent to-transparent flex items-end p-3 z-10 pointer-events-none">
                    <span className="text-xs font-semibold text-white px-2 py-1 bg-black/60 rounded backdrop-blur drop-shadow-md">
                        {book.page_count} Pages
                    </span>
                </div>
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
        </Link>
    );

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
                    <h2 className="text-3xl font-bold text-white tracking-tight flex flex-col sm:flex-row sm:items-center gap-4">
                        <div className="flex items-center">
                            {selectedVolume ? (
                                <>
                                    <FolderOpen className="w-8 h-8 mr-3 text-komgaPrimary" />
                                    {selectedVolume}
                                </>
                            ) : (
                                seriesInfo?.title?.Valid ? seriesInfo.title.String : (seriesInfo?.name || "系列总览")
                            )}
                            {!selectedVolume && seriesInfo && (
                                <button
                                    onClick={() => setIsEditing(true)}
                                    className="ml-4 p-1.5 text-gray-500 hover:text-komgaPrimary hover:bg-komgaPrimary/10 rounded transition-colors"
                                    title="编辑元数据"
                                >
                                    <Edit className="w-5 h-5" />
                                </button>
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
                                { id: 'status', label: '连载状态 (Status)', type: 'text', val: editForm.status?.String || '' },
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
                <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-6">
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
                            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-6">
                                {volumes.map(vol => (
                                    <div
                                        key={vol.name}
                                        onClick={() => setSelectedVolume(vol.name)}
                                        className="group flex flex-col rounded-xl overflow-hidden bg-gray-900 border border-gray-800 hover:border-komgaPrimary/50 hover:bg-gray-800 transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer"
                                    >
                                        <div className="aspect-[3/4] w-full bg-komgaDark flex items-center justify-center relative overflow-hidden">
                                            {vol.cover_path?.Valid && vol.cover_path?.String && vol.cover_book_id ? (
                                                <img src={`/api/covers/${vol.cover_book_id}`} className="absolute inset-0 w-full h-full object-cover opacity-80 transition-transform duration-500 group-hover:scale-105" alt="cover" loading="lazy" />
                                            ) : (
                                                <FolderOpen className="w-16 h-16 text-gray-700 opacity-50 group-hover:text-komgaPrimary transition-colors relative z-10" />
                                            )}
                                            {/* 底部叠加卷信息 */}
                                            <div className="absolute inset-0 bg-gradient-to-t from-gray-900/90 via-gray-900/30 to-transparent flex items-end p-3 z-10 pointer-events-none">
                                                <div className="w-full flex justify-between items-center text-xs font-semibold text-gray-300">
                                                    <span>{vol.books.length} 话</span>
                                                    <span>{vol.total_pages} 页</span>
                                                </div>
                                            </div>
                                            {/* 右上角叠加卷叠层徽章 */}
                                            <div className="absolute top-2 right-2 bg-komgaPrimary/90 text-white text-[10px] uppercase font-bold px-2 py-0.5 rounded shadow-lg">
                                                Volume
                                            </div>
                                        </div>
                                        <div className="p-4 flex-1">
                                            <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-komgaPrimary transition-colors">
                                                {vol.name}
                                            </h4>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    )}

                    {standaloneBooks.length > 0 && (
                        <div>
                            <h3 className="text-lg font-semibold text-gray-300 mb-4 flex items-center">
                                <BookImage className="w-5 h-5 mr-2 text-komgaPrimary" /> 单行本册子
                            </h3>
                            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-6">
                                {standaloneBooks.map(renderBookCard)}
                            </div>
                        </div>
                    )}

                    {books.length === 0 && (
                        <div className="text-center py-20 text-gray-500">此系列尚未包含任何资源</div>
                    )}
                </div>
            )}
        </div>
    );
}

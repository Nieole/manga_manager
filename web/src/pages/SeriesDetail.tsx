import { useState, useEffect, useMemo } from 'react';
import axios from 'axios';
import { useParams, Link, useNavigate, useOutletContext } from 'react-router-dom';
import { ArrowLeft, BookImage, FolderOpen } from 'lucide-react';

interface NullString {
    String: string;
    Valid: boolean;
}

interface Book {
    id: string;
    name: string;
    library_id: string;
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
    const [books, setBooks] = useState<Book[]>([]);
    const [loading, setLoading] = useState(true);

    // 当前如果是阅读某个卷下的内容，记录被选中的卷名
    const [selectedVolume, setSelectedVolume] = useState<string | null>(null);

    useEffect(() => {
        if (seriesId) {
            axios.get(`/api/books/${seriesId}`)
                .then(res => {
                    setBooks(res.data);
                    setLoading(false);
                })
                .catch(err => {
                    console.error("Failed to load books", err);
                    setLoading(false);
                });
        }
    }, [seriesId, refreshTrigger]);

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
            cover_path: volBooks.find(b => b.cover_path?.Valid)?.cover_path,
            cover_book_id: volBooks.find(b => b.cover_path?.Valid)?.id,
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
                    <h2 className="text-3xl font-bold text-white tracking-tight flex items-center">
                        {selectedVolume ? (
                            <>
                                <FolderOpen className="w-8 h-8 mr-3 text-komgaPrimary" />
                                {selectedVolume}
                            </>
                        ) : "系列总览"}
                    </h2>
                    <p className="text-gray-400 mt-2 text-sm">
                        {selectedVolume
                            ? `含 ${activeVolumeBooks.length} 话 · 总共 ${activeVolumeBooks.reduce((acc, b) => acc + b.page_count, 0)} 页`
                            : `共 ${books.length} 项资源 (${volumes.length} 卷, ${standaloneBooks.length} 独立册)`
                        }
                    </p>
                </div>
            </div>

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
                                            {vol.cover_path?.Valid && vol.cover_book_id ? (
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

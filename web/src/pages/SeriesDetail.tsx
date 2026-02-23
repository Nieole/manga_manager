import { useState, useEffect } from 'react';
import axios from 'axios';
import { useParams, Link, useNavigate, useOutletContext } from 'react-router-dom';
import { ArrowLeft, BookImage } from 'lucide-react';

interface NullString {
    String: string;
    Valid: boolean;
}

interface Book {
    id: string;
    name: string;
    library_id: string;
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

    return (
        <div className="p-6 lg:p-10">
            <div className="mb-6">
                <button
                    onClick={() => {
                        const libId = books.length > 0 ? books[0].library_id : null;
                        if (libId) {
                            navigate(`/library/${libId}`);
                        } else {
                            navigate('/');
                        }
                    }}
                    className="flex items-center text-komgaPrimary hover:text-purple-400 transition-colors text-sm font-medium mb-4"
                >
                    <ArrowLeft className="w-4 h-4 mr-1" />
                    返回
                </button>
                <h2 className="text-3xl font-bold text-white tracking-tight">系列包含的卷/册</h2>
                <p className="text-gray-400 mt-2 text-sm">{books.length} 本元数据</p>
            </div>

            {loading ? (
                <div className="text-gray-500 animate-pulse">正在提取数据库中的书籍...</div>
            ) : (
                <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-6">
                    {books.map(book => (
                        <Link
                            to={`/reader/${book.id}`}
                            key={book.id}
                            className="group flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary/50 cursor-pointer"
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
                                    <h4 className="text-sm font-bold text-gray-200 line-clamp-2 leading-snug group-hover:text-komgaPrimary">
                                        {book.title?.Valid ? book.title.String : book.name}
                                    </h4>
                                    {book.last_read_page?.Valid && book.last_read_page.Int64 > 0 && (
                                        <div className="mt-2 inline-flex items-center text-xs font-medium text-orange-400 bg-orange-400/10 px-2 py-0.5 rounded-sm">
                                            阅读至 {book.last_read_page.Int64} 页
                                        </div>
                                    )}
                                </div>
                            </div>
                        </Link>
                    ))}
                </div>
            )}
        </div>
    );
}

import { useState, useEffect } from 'react';
import axios from 'axios';
import { useParams, useNavigate } from 'react-router-dom';
import { ArrowLeft, Loader2 } from 'lucide-react';

interface Page {
    number: number;
    width: number;
    height: number;
}

export default function BookReader() {
    const { bookId } = useParams();
    const navigate = useNavigate();
    const [pages, setPages] = useState<Page[]>([]);
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        if (!bookId) return;

        axios.get(`/api/pages/${bookId}`)
            .then(res => {
                // 后端按页码返回
                const sorted = res.data.sort((a: Page, b: Page) => a.number - b.number);
                setPages(sorted);
                setLoading(false);
            })
            .catch(err => {
                console.error("Failed to load pages", err);
                setLoading(false);
            });
    }, [bookId]);

    return (
        <div className="absolute inset-0 bg-black flex flex-col z-50 overflow-hidden">
            {/* 悬浮顶栏 */}
            <div className="absolute top-0 inset-x-0 h-16 bg-gradient-to-b from-black/80 to-transparent flex items-center px-6 transition-opacity duration-300 opacity-0 hover:opacity-100 z-10">
                <button
                    onClick={() => navigate(-1)}
                    className="text-white hover:text-komgaPrimary transition flex items-center bg-black/40 rounded-full px-4 py-2 backdrop-blur"
                >
                    <ArrowLeft className="w-5 h-5 mr-2" />
                    退出阅读
                </button>
            </div>

            <div className="flex-1 overflow-y-auto w-full ReaderScrollContainer">
                {loading ? (
                    <div className="flex items-center justify-center h-full">
                        <Loader2 className="w-10 h-10 animate-spin text-komgaPrimary" />
                    </div>
                ) : (
                    <div className="flex flex-col items-center w-full bg-black">
                        {pages.map(page => (
                            <img
                                key={page.number}
                                src={`/api/pages/${bookId}/${page.number}`}
                                loading="lazy"
                                decoding="async"
                                className="w-auto h-auto max-w-full lg:max-w-4xl max-h-screen object-contain mb-2 md:mb-4 bg-gray-900 min-h-[50vh]"
                                alt={`Page ${page.number}`}
                            />
                        ))}
                    </div>
                )}
            </div>
        </div>
    );
}

import { useState, useEffect } from 'react';
import axios from 'axios';
import { useParams, Link, useOutletContext } from 'react-router-dom';
import { ImageIcon } from 'lucide-react';

interface NullString {
    String: string;
    Valid: boolean;
}

interface Series {
    id: string;
    name: string;
    title?: NullString;
    summary?: NullString;
}

export default function Home() {
    const { libId } = useParams();
    const { refreshTrigger } = useOutletContext<{ refreshTrigger: number }>() || { refreshTrigger: 0 };
    const [series, setSeries] = useState<Series[]>([]);
    const [loading, setLoading] = useState(false);

    useEffect(() => {
        if (libId) {
            setLoading(true);
            axios.get(`/api/series/${libId}`)
                .then(res => {
                    setSeries(res.data);
                    setLoading(false);
                })
                .catch(err => {
                    console.error("Failed to fetch series:", err);
                    setLoading(false);
                });
        }
    }, [libId, refreshTrigger]);

    if (!libId) {
        return (
            <div className="flex-1 flex items-center justify-center p-10 h-full text-gray-500">
                请在左侧选择一个扫描库以开始
            </div>
        );
    }

    return (
        <div className="p-6 lg:p-10">
            <div className="mb-8 flex justify-between items-end border-b border-gray-800 pb-4">
                <div>
                    <h2 className="text-3xl font-bold text-white tracking-tight mb-2">
                        浏览系列
                    </h2>
                    <p className="text-gray-400 text-sm">
                        共找到 {series.length} 个系列项目
                    </p>
                </div>
            </div>

            {loading ? (
                <div className="text-center py-20 text-gray-400 animate-pulse">正在加载目录与元数据...</div>
            ) : (
                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-6">
                    {series.map(s => (
                        <Link
                            key={s.id}
                            to={`/series/${s.id}`}
                            className="group relative flex flex-col rounded-xl overflow-hidden bg-komgaSurface border border-gray-800 hover:border-komgaPrimary/50 transition-all duration-300 hover:-translate-y-1 hover:shadow-xl hover:shadow-komgaPrimary/10 cursor-pointer"
                        >
                            <div className="aspect-[2/3] w-full bg-gray-900 flex items-center justify-center relative overflow-hidden">
                                <ImageIcon className="h-12 w-12 text-gray-700 opacity-50 transition-opacity group-hover:opacity-100" />
                                {/* 缩略图待实现： */}
                                {/* <img src={`/api/series/${s.id}/thumbnail`} className="absolute inset-0 w-full h-full object-cover" /> */}
                                <div className="absolute inset-0 bg-gradient-to-t from-black/80 via-black/20 to-transparent opacity-0 group-hover:opacity-100 transition-opacity duration-300" />
                            </div>
                            <div className="p-4 flex-1 flex flex-col justify-between">
                                <div>
                                    <h3 className="text-sm font-medium text-white line-clamp-2 leading-tight group-hover:text-komgaPrimary transition-colors">
                                        {s.title?.Valid ? s.title.String : s.name}
                                    </h3>
                                </div>
                                <div className="mt-3 flex items-center justify-between text-xs text-gray-500">
                                    <span>系列</span>
                                    <span className="opacity-0 group-hover:opacity-100 transition-opacity">→</span>
                                </div>
                            </div>
                        </Link>
                    ))}
                </div>
            )}
        </div>
    );
}

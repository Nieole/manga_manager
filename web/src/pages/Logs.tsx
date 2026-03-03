import { useEffect, useState } from 'react';
import { RefreshCw, AlertTriangle, AlertCircle, Info, Hash } from 'lucide-react';
import { format } from 'date-fns';

interface LogEntry {
    time: string;
    level: string;
    msg: string;
    raw: string;
}

export default function Logs() {
    const [logs, setLogs] = useState<LogEntry[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [filterLevel, setFilterLevel] = useState('ALL');

    const fetchLogs = async () => {
        setLoading(true);
        setError(null);
        try {
            const resp = await fetch(`/api/system/logs?limit=500&level=${filterLevel}`);
            if (!resp.ok) {
                throw new Error('Failed to fetch logs');
            }
            const data = await resp.json();
            setLogs(data || []);
        } catch (err: any) {
            setError(err.message || 'Unknown error');
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        fetchLogs();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [filterLevel]);

    const getLevelColor = (level: string) => {
        switch (level.toUpperCase()) {
            case 'ERROR':
                return 'text-red-500 bg-red-500/10 border-red-500/20';
            case 'WARN':
                return 'text-amber-500 bg-amber-500/10 border-amber-500/20';
            case 'INFO':
                return 'text-blue-500 bg-blue-500/10 border-blue-500/20';
            default:
                return 'text-slate-400 bg-slate-800 border-slate-700';
        }
    };

    const getLevelIcon = (level: string) => {
        switch (level.toUpperCase()) {
            case 'ERROR':
                return <AlertCircle className="w-4 h-4 text-red-500" />;
            case 'WARN':
                return <AlertTriangle className="w-4 h-4 text-amber-500" />;
            case 'INFO':
                return <Info className="w-4 h-4 text-blue-500" />;
            default:
                return <Hash className="w-4 h-4 text-slate-400" />;
        }
    };

    const formatLogTime = (timeStr: string) => {
        try {
            if (!timeStr) return '';
            const d = new Date(timeStr);
            return format(d, 'yyyy-MM-dd HH:mm:ss');
        } catch {
            return timeStr;
        }
    };

    return (
        <div className="p-6 max-w-[1600px] mx-auto space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
                <div>
                    <h1 className="text-3xl font-bold bg-gradient-to-r from-slate-100 to-slate-400 bg-clip-text text-transparent">
                        System Logs
                    </h1>
                    <p className="text-slate-400 mt-1">View backend server logs and monitor system health.</p>
                </div>

                <div className="flex items-center gap-3">
                    <select
                        value={filterLevel}
                        onChange={(e) => setFilterLevel(e.target.value)}
                        className="bg-slate-800 border-slate-700 text-slate-300 rounded-lg px-3 py-2 outline-none focus:ring-2 focus:ring-blue-500/50"
                    >
                        <option value="ALL">All Levels</option>
                        <option value="ERROR">Error Only</option>
                        <option value="WARN">Warning & Error</option>
                        <option value="INFO">Info</option>
                    </select>

                    <button
                        onClick={fetchLogs}
                        disabled={loading}
                        className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 text-white rounded-lg transition-colors disabled:opacity-50 disabled:cursor-not-allowed group shadow-lg shadow-blue-500/20"
                    >
                        <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : 'group-hover:rotate-180 transition-transform duration-500'}`} />
                        Refresh
                    </button>
                </div>
            </div>

            <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden flex flex-col min-h-[600px] shadow-2xl relative">
                {/* Terminal Header */}
                <div className="bg-slate-950 px-4 py-3 flex items-center gap-2 border-b border-slate-800">
                    <div className="flex gap-1.5">
                        <div className="w-3 h-3 rounded-full bg-red-500/80"></div>
                        <div className="w-3 h-3 rounded-full bg-amber-500/80"></div>
                        <div className="w-3 h-3 rounded-full bg-green-500/80"></div>
                    </div>
                    <span className="text-xs font-mono text-slate-500 ml-2">manga_manager.log</span>
                </div>

                {/* Console Body */}
                <div className="flex-1 overflow-x-auto p-4 font-mono text-sm">
                    {loading && logs.length === 0 ? (
                        <div className="flex flex-col items-center justify-center h-full text-slate-500 gap-3">
                            <RefreshCw className="w-8 h-8 animate-spin text-blue-500/50" />
                            <span>Fetching logs...</span>
                        </div>
                    ) : error ? (
                        <div className="flex flex-col items-center justify-center h-full text-red-400 gap-3">
                            <AlertTriangle className="w-10 h-10" />
                            <span>{error}</span>
                        </div>
                    ) : logs.length === 0 ? (
                        <div className="flex flex-col items-center justify-center h-full text-slate-500 gap-3">
                            <Info className="w-10 h-10 text-slate-600" />
                            <span>No logs found for the selected filter.</span>
                        </div>
                    ) : (
                        <div className="space-y-1">
                            {logs.map((log, idx) => (
                                <div
                                    key={idx}
                                    className={`flex items-start gap-3 py-1.5 px-2 rounded hover:bg-slate-800/50 transition-colors ${log.level === 'ERROR' ? 'bg-red-950/20' : ''
                                        }`}
                                >
                                    <div className="min-w-fit mt-0.5 opacity-80">
                                        {getLevelIcon(log.level)}
                                    </div>
                                    <div className="min-w-[150px] text-slate-500 shrink-0">
                                        {formatLogTime(log.time)}
                                    </div>
                                    <div className={`shrink-0 px-2 py-0.5 rounded text-xs border uppercase tracking-wider font-semibold ${getLevelColor(log.level)}`}>
                                        {log.level || 'LOG'}
                                    </div>
                                    <div className="text-slate-300 break-words flex-1 whitespace-pre-wrap">
                                        <span className="font-medium text-slate-200">{log.msg}</span>
                                        {log.raw && log.msg !== log.raw && (
                                            <div className="text-slate-500 text-xs mt-1 block">
                                                {log.raw}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

import { useCallback, useEffect, useRef, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../../api/client';
import type {
  Author,
  Book,
  MetaTag,
  MetadataProvenance,
  MetadataReview,
  Series,
  SeriesContextResponse,
  SeriesContinue,
  SeriesFailedTask,
  SeriesLink,
  SeriesRelation,
} from '../types';

interface UseSeriesContextParams {
  seriesId: string | undefined;
  refreshTrigger: number;
}

export interface SeriesContextState {
  loading: boolean;
  // error 为最近一次加载系列上下文失败的可读消息（成功后清空）；供详情页在主数据缺失时
  // 渲染错误 + 重试。
  error: string | null;
  retry: () => void;
  series: Series | null;
  books: Book[];
  tags: MetaTag[];
  authors: Author[];
  links: SeriesLink[];
  // metadataVersion 是本次取到的元数据版本，用户按下保存时原样带回，服务端据此判断编辑
  // 期间有没有别的途径写过这个系列。系列未加载时为 null。
  metadataVersion: string | null;
  relations: SeriesRelation[];
  metadataReviews: MetadataReview[];
  metadataProvenance: MetadataProvenance[];
  failedTasks: SeriesFailedTask[];
  continueInfo: SeriesContinue | null;
  reload: () => Promise<void>;
  setRelations: React.Dispatch<React.SetStateAction<SeriesRelation[]>>;
  setMetadataReviews: React.Dispatch<React.SetStateAction<MetadataReview[]>>;
  setMetadataProvenance: React.Dispatch<React.SetStateAction<MetadataProvenance[]>>;
  setFailedTasks: React.Dispatch<React.SetStateAction<SeriesFailedTask[]>>;
}

export function useSeriesContext({ seriesId, refreshTrigger }: UseSeriesContextParams): SeriesContextState {
  const [loading, setLoading] = useState(true);
  const [series, setSeries] = useState<Series | null>(null);
  const [books, setBooks] = useState<Book[]>([]);
  const [tags, setTags] = useState<MetaTag[]>([]);
  const [authors, setAuthors] = useState<Author[]>([]);
  const [links, setLinks] = useState<SeriesLink[]>([]);
  const [metadataVersion, setMetadataVersion] = useState<string | null>(null);
  const [relations, setRelations] = useState<SeriesRelation[]>([]);
  const [metadataReviews, setMetadataReviews] = useState<MetadataReview[]>([]);
  const [metadataProvenance, setMetadataProvenance] = useState<MetadataProvenance[]>([]);
  const [failedTasks, setFailedTasks] = useState<SeriesFailedTask[]>([]);
  const [continueInfo, setContinueInfo] = useState<SeriesContinue | null>(null);
  const [error, setError] = useState<string | null>(null);

  // 取上下文的世代号：在关系图/合集/作品群里连点系列时，A 的 /context 还在飞就已经跳到了 B，
  // A 的响应必须被丢弃。写内容、写错误、清 loading 三条路径都只认最新一次请求——否则地址栏是 B
  // 而页面挂着 A 的书列表，用户在这页上做批量已读就把进度写到了 A 的书 id 上。
  const latestRequestIDRef = useRef(0);
  // 当前状态属于哪个系列。只能记在 ref 里：effect 里读 series 状态拿到的是上一轮渲染的闭包值，
  // 切系列时它还是上一个系列的非空值。
  const shownSeriesIdRef = useRef<string | undefined>(undefined);

  const load = useCallback(
    async (silent: boolean) => {
      if (!seriesId) return;
      const requestID = latestRequestIDRef.current + 1;
      latestRequestIDRef.current = requestID;
      if (!silent) setLoading(true);
      setError(null);
      try {
        const res = await apiClient.get<SeriesContextResponse>(`/api/series/${seriesId}/context`);
        if (requestID !== latestRequestIDRef.current) return;
        const data = res.data;
        setSeries(data.series);
        setBooks(Array.isArray(data.books) ? data.books : []);
        setTags(Array.isArray(data.tags) ? data.tags : []);
        setAuthors(Array.isArray(data.authors) ? data.authors : []);
        setLinks(Array.isArray(data.links) ? data.links : []);
        setMetadataVersion(data.metadata_version ?? null);
        setRelations(Array.isArray(data.relations) ? data.relations : []);
        setMetadataReviews(Array.isArray(data.metadata_review?.reviews) ? data.metadata_review!.reviews : []);
        setMetadataProvenance(Array.isArray(data.metadata_review?.provenance) ? data.metadata_review!.provenance : []);
        setFailedTasks(Array.isArray(data.failed_tasks) ? data.failed_tasks : []);
        setContinueInfo(data.continue ?? null);
      } catch (err) {
        if (requestID !== latestRequestIDRef.current) return;
        console.error('Failed to load series context', err);
        setError(getApiErrorMessage(err, ''));
      } finally {
        if (requestID === latestRequestIDRef.current) setLoading(false);
      }
    },
    [seriesId],
  );

  // reload：写操作后的静默重取（不显示 loading）。
  const reload = useCallback(() => load(true), [load]);

  useEffect(() => {
    if (!seriesId) return;
    const switching = shownSeriesIdRef.current !== seriesId;
    shownSeriesIdRef.current = seriesId;
    if (switching) {
      // 切系列时先清空上一个系列的内容：详情页的加载判据是 loading && !series，不清空就会在新
      // 系列的上下文到达前继续渲染上一个系列的简介、书列表、标签与提案，且不给任何加载指示。
      // 这一步同时关掉了 useSeriesEdit.save 的窗口——seriesId 已是 B 而 series 仍是 A 时保存，
      // 会把 A 的 title/summary/tags 打到 PUT /api/series/info/{B}。
      setSeries(null);
      setBooks([]);
      setTags([]);
      setAuthors([]);
      setLinks([]);
      setMetadataVersion(null);
      setRelations([]);
      setMetadataReviews([]);
      setMetadataProvenance([]);
      setFailedTasks([]);
      setContinueInfo(null);
    }
    // 同一系列的刷新（refreshTrigger 变化）保持静默，不闪加载态。
    void load(!switching);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seriesId, refreshTrigger]);

  // retry：主数据加载失败后的重试入口，非静默重取（显示 loading、清除错误）。
  const retry = useCallback(() => {
    void load(false);
  }, [load]);

  return {
    loading,
    error,
    retry,
    series,
    books,
    tags,
    authors,
    links,
    metadataVersion,
    relations,
    metadataReviews,
    metadataProvenance,
    failedTasks,
    continueInfo,
    reload,
    setRelations,
    setMetadataReviews,
    setMetadataProvenance,
    setFailedTasks,
  };
}

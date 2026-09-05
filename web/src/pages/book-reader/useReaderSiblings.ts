import { useEffect, useMemo, useRef, useState } from 'react';
import { apiClient, isAxiosError } from '../../api/client';
import type { ReaderBookInfo } from './types';

export interface SiblingBook {
  id: number;
  name: string;
  title: string;
  volume: string;
}

export interface VolumeBookEntry {
  id: number;
  name: string;
  title: string;
  volume: string;
}

interface SeriesContextBook {
  id: number;
  name: string;
  volume?: string;
  title?: { Valid?: boolean; String?: string };
}

interface SeriesContextLite {
  series?: { id: number };
  books?: SeriesContextBook[];
}

function toSibling(info: ReaderBookInfo | null): SiblingBook | null {
  if (!info || !info.id) return null;
  const title = info.title?.Valid && info.title.String ? info.title.String : info.name;
  return {
    id: info.id,
    name: info.name,
    title,
    volume: info.volume || '',
  };
}

function toVolumeEntry(book: SeriesContextBook): VolumeBookEntry {
  const title = book.title?.Valid && book.title.String ? book.title.String : book.name;
  return {
    id: book.id,
    name: book.name,
    title,
    volume: book.volume || '',
  };
}

interface UseReaderSiblingsOptions {
  bookId?: string;
  seriesIdRef: { current: number | null };
  bookVolume: string;
  loading: boolean;
}

export interface UseReaderSiblingsResult {
  prev: SiblingBook | null;
  next: SiblingBook | null;
  allInVolume: VolumeBookEntry[];
  currentVolume: string;
  currentIndexInVolume: number;
}

export function useReaderSiblings({
  bookId,
  seriesIdRef,
  bookVolume,
  loading,
}: UseReaderSiblingsOptions): UseReaderSiblingsResult {
  const [prev, setPrev] = useState<SiblingBook | null>(null);
  const [next, setNext] = useState<SiblingBook | null>(null);
  const [contextBooks, setContextBooks] = useState<SeriesContextBook[]>([]);
  const [contextSeriesId, setContextSeriesId] = useState<number | null>(null);
  const lastSeriesFetchRef = useRef<number | null>(null);

  useEffect(() => {
    if (!bookId || loading) return undefined;
    let cancelled = false;
     
    setPrev(null);
    setNext(null);

    apiClient.get<ReaderBookInfo>(`/api/book-prev/${bookId}`)
      .then((res) => {
        if (!cancelled) setPrev(toSibling(res.data));
      })
      .catch((err) => {
        if (cancelled) return;
        if (!isAxiosError(err) || err.response?.status !== 404) {
          console.error('Failed to load previous book', err);
        }
        setPrev(null);
      });

    apiClient.get<ReaderBookInfo>(`/api/book-next/${bookId}`)
      .then((res) => {
        if (!cancelled) setNext(toSibling(res.data));
      })
      .catch((err) => {
        if (cancelled) return;
        if (!isAxiosError(err) || err.response?.status !== 404) {
          console.error('Failed to load next book', err);
        }
        setNext(null);
      });

    return () => { cancelled = true; };
  }, [bookId, loading]);

  // lastSeriesFetchRef 兼作「已经为哪个系列取过上下文」的标记与世代号：
  // 响应回来时它若已换成别的系列，说明用户已经翻去了另一个系列，这份整个丢掉。
  // 失败时必须把它退回 null——留着的话，一次网络抖动就让同一系列内换书永不重取，
  // allInVolume 恒空，顶栏的章节列表按钮就此消失，且没有任何报错。
  useEffect(() => {
    const seriesId = seriesIdRef.current;
    if (!seriesId || loading) return;
    if (lastSeriesFetchRef.current === seriesId) return;
    lastSeriesFetchRef.current = seriesId;
    // 上一个系列的书必须当场丢掉，不能留到新上下文回来才换：两个系列都有「第 1 卷」时（极常见）
    // 交集非空，顶栏的卷内章节列表整段请求期间都亮着，点进去跳到另一个系列的书。
    setContextBooks([]);
    setContextSeriesId(null);

    apiClient.get<SeriesContextLite>(`/api/series/${seriesId}/context`)
      .then((res) => {
        if (lastSeriesFetchRef.current !== seriesId) return;
        const books = Array.isArray(res.data?.books) ? res.data.books : [];
        setContextBooks(books);
        setContextSeriesId(seriesId);
      })
      .catch((err) => {
        if (lastSeriesFetchRef.current !== seriesId) return;
        lastSeriesFetchRef.current = null;
        console.error('Failed to load series context for siblings', err);
        setContextBooks([]);
      });
  }, [seriesIdRef, bookId, loading]);

  const allInVolume = useMemo(() => {
    if (!bookVolume || contextSeriesId == null) return [] as VolumeBookEntry[];
    return contextBooks
      .filter((b) => (b.volume || '') === bookVolume)
      .map(toVolumeEntry);
  }, [bookVolume, contextBooks, contextSeriesId]);

  const currentIndexInVolume = useMemo(() => {
    if (!bookId) return -1;
    const idNum = Number(bookId);
    return allInVolume.findIndex((b) => b.id === idNum);
  }, [allInVolume, bookId]);

  return {
    prev,
    next,
    allInVolume,
    currentVolume: bookVolume,
    currentIndexInVolume,
  };
}

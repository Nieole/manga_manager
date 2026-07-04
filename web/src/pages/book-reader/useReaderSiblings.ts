/**
 * 业务说明：本文件是业务实现，属于前端阅读器页面，负责呈现漫画页、阅读偏好、键盘/触控操作、进度同步和缓存体验。
 * 它直接承载用户阅读主流程，需要把后端页面 API、缩放模式和本地偏好组合成稳定交互。
 * 维护时应关注页面预加载、错误恢复、移动端布局、进度写回频率和快捷操作一致性。
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import axios from 'axios';
import { apiClient } from '../../api/client';
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
        if (!axios.isAxiosError(err) || err.response?.status !== 404) {
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
        if (!axios.isAxiosError(err) || err.response?.status !== 404) {
          console.error('Failed to load next book', err);
        }
        setNext(null);
      });

    return () => { cancelled = true; };
  }, [bookId, loading]);

  useEffect(() => {
    const seriesId = seriesIdRef.current;
    if (!seriesId || loading) return undefined;
    if (lastSeriesFetchRef.current === seriesId) return undefined;
    lastSeriesFetchRef.current = seriesId;
    let cancelled = false;

    apiClient.get<SeriesContextLite>(`/api/series/${seriesId}/context`)
      .then((res) => {
        if (cancelled) return;
        const books = Array.isArray(res.data?.books) ? res.data.books : [];
        setContextBooks(books);
        setContextSeriesId(seriesId);
      })
      .catch((err) => {
        if (cancelled) return;
        console.error('Failed to load series context for siblings', err);
        setContextBooks([]);
      });

    return () => { cancelled = true; };
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

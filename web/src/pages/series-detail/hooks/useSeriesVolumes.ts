/**
 * 业务说明：本文件是业务实现，属于前端系列详情页面，负责展示系列信息、卷册列表、元数据审核、关系维护和阅读入口。
 * 它把数据库中的书籍聚合、外部元数据和人工编辑结果组织成单个系列的业务视图。
 * 维护时应关注编辑态与展示态同步、批量选择、关系变更后刷新和移动端信息密度。
 */

import { useMemo } from 'react';
import type { Book, NullString } from '../types';
import { compareBooksForDisplay, compareOrdinalLabels } from '../utils/ordinal';

export interface VolumeItem {
  name: string;
  books: Book[];
  cover_path?: NullString;
  cover_book_id?: number;
  total_pages: number;
  read_pages: number;
}

export interface UseSeriesVolumesResult {
  volumes: VolumeItem[];
  standaloneBooks: Book[];
  allBookIds: number[];
}

export function useSeriesVolumes(books: Book[]): UseSeriesVolumesResult {
  return useMemo(() => {
    const volumeMap = new Map<string, Book[]>();
    const standalones: Book[] = [];

    books.forEach((b) => {
      if (b.volume && b.volume.trim() !== '') {
        if (!volumeMap.has(b.volume)) volumeMap.set(b.volume, []);
        volumeMap.get(b.volume)!.push(b);
      } else {
        standalones.push(b);
      }
    });
    standalones.sort(compareBooksForDisplay);
    volumeMap.forEach((volBooks) => volBooks.sort(compareBooksForDisplay));

    const volumeArr: VolumeItem[] = Array.from(volumeMap.entries()).map(([name, volBooks]) => {
      const coverBook = volBooks.find((b) => b.cover_path?.Valid && b.cover_path?.String);
      return {
        name,
        books: volBooks,
        cover_path: coverBook?.cover_path,
        cover_book_id: coverBook?.id,
        total_pages: volBooks.reduce((sum, b) => sum + b.page_count, 0),
        read_pages: volBooks.reduce((sum, b) => sum + (b.last_read_page?.Valid ? b.last_read_page.Int64 : 0), 0),
      };
    });
    volumeArr.sort((a, b) => compareOrdinalLabels(a.name, b.name));

    const allBookIds = [
      ...volumeArr.flatMap((v) => v.books.map((b) => b.id)),
      ...standalones.map((b) => b.id),
    ];

    return { volumes: volumeArr, standaloneBooks: standalones, allBookIds };
  }, [books]);
}

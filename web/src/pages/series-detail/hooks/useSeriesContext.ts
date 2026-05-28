import { useCallback, useEffect, useState } from 'react';
import axios from 'axios';
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
  series: Series | null;
  books: Book[];
  tags: MetaTag[];
  authors: Author[];
  links: SeriesLink[];
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
  const [relations, setRelations] = useState<SeriesRelation[]>([]);
  const [metadataReviews, setMetadataReviews] = useState<MetadataReview[]>([]);
  const [metadataProvenance, setMetadataProvenance] = useState<MetadataProvenance[]>([]);
  const [failedTasks, setFailedTasks] = useState<SeriesFailedTask[]>([]);
  const [continueInfo, setContinueInfo] = useState<SeriesContinue | null>(null);

  const reload = useCallback(async () => {
    if (!seriesId) return;
    const res = await axios.get<SeriesContextResponse>(`/api/series/${seriesId}/context`);
    const data = res.data;
    setSeries(data.series);
    setBooks(Array.isArray(data.books) ? data.books : []);
    setTags(Array.isArray(data.tags) ? data.tags : []);
    setAuthors(Array.isArray(data.authors) ? data.authors : []);
    setLinks(Array.isArray(data.links) ? data.links : []);
    setRelations(Array.isArray(data.relations) ? data.relations : []);
    setMetadataReviews(Array.isArray(data.metadata_review?.reviews) ? data.metadata_review!.reviews : []);
    setMetadataProvenance(Array.isArray(data.metadata_review?.provenance) ? data.metadata_review!.provenance : []);
    setFailedTasks(Array.isArray(data.failed_tasks) ? data.failed_tasks : []);
    setContinueInfo(data.continue ?? null);
  }, [seriesId]);

  useEffect(() => {
    if (!seriesId) return;
    setLoading(!series && books.length === 0);
    reload()
      .catch((err) => console.error('Failed to load series context', err))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [seriesId, refreshTrigger]);

  return {
    loading,
    series,
    books,
    tags,
    authors,
    links,
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

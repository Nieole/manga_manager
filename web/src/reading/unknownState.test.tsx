// @vitest-environment jsdom
import { cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { SeriesBookCard } from '../pages/series-detail/SeriesBookCard';
import { useSeriesVolumes } from '../pages/series-detail/hooks/useSeriesVolumes';
import type { Book } from '../pages/series-detail/types';

vi.mock('../i18n/LocaleProvider',()=>({useI18n:()=>({t:(key:string)=>key})}));
vi.mock('../auth/AuthProvider',()=>({useAuth:()=>({isAdmin:false})}));
vi.mock('../components/ToastProvider',()=>({useToast:()=>({showToast:vi.fn()})}));
afterEach(cleanup);

it('shows an unknown-page completion as mark-unread without inventing volume pages',()=>{
 const book:Book={id:1,library_id:1,name:'Unknown',volume:'Volume 1',page_count:0,is_read:true,last_read_page:{Valid:false,Int64:0}};
 const toggle=vi.fn();
 render(<MemoryRouter><SeriesBookCard book={book} isSelectionMode={false} isSelected={false} onCardClick={vi.fn()} onQuickToggleRead={toggle} onExportComicInfo={vi.fn()} onWriteComicInfo={vi.fn()} onCopyPath={vi.fn()} /></MemoryRouter>);
 fireEvent.click(screen.getByTitle('series.book.markUnread'));
 expect(toggle).toHaveBeenCalledWith(book,false);
 expect(document.body.textContent).not.toContain('99999');
 const {result}=renderHook(()=>useSeriesVolumes([book]));
 expect(result.current.volumes[0]).toMatchObject({is_read:true,total_pages:0,read_pages:0});
});

import { expect, it } from 'vitest';
import { bookReadingState } from './bookState';

it('keeps unknown-page completion separate from read page evidence',()=>{
 const state=bookReadingState({is_read:true,page_count:0,last_read_page:{Valid:false,Int64:0}});
 expect(state.completed).toBe(true);
 expect(state.readPages).toBe(0);
 const discovered=bookReadingState({is_read:true,page_count:20,last_read_page:{Valid:false,Int64:0}});
 expect(discovered.completed).toBe(true);
 expect(discovered.readPages).toBe(0);
});

it('retains known-page completion and clamps displayed positions',()=>{
 expect(bookReadingState({page_count:20,last_read_page:{Valid:true,Int64:22}})).toMatchObject({completed:true,readPages:20});
 expect(bookReadingState({page_count:20,last_read_page:{Valid:true,Int64:4}})).toMatchObject({completed:false,readPages:4});
});

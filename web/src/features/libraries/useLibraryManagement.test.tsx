// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { useLibraryManagement } from './useLibraryManagement';

const { get, post, put, toast, invalidate, axiosError } = vi.hoisted(() => ({get:vi.fn(),post:vi.fn(),put:vi.fn(),toast:vi.fn(),invalidate:vi.fn(),axiosError:vi.fn()}));
vi.mock('../../api/client',()=>({apiClient:{get,post,put},isAxiosError:axiosError}));
vi.mock('../../components/ToastProvider',()=>({useToast:()=>({showToast:toast})}));
vi.mock('../../i18n/LocaleProvider',()=>({useI18n:()=>({t:(key:string)=>key})}));
vi.mock('../../session/ApplicationSessionProvider',()=>({useLibraryInvalidation:()=>invalidate,useLibraryRefresh:()=>0}));
beforeEach(()=>{ vi.clearAllMocks(); axiosError.mockReturnValue(false); get.mockResolvedValue({data:[]}); });

it('keeps drafts and browser location after rejected save, then distinguishes committed save from failed navigation refresh',async()=>{
 const {result}=renderHook(()=>useLibraryManagement(false,vi.fn()));
 await waitFor(()=>expect(result.current.loading).toBe(false));
 act(()=>{result.current.setShowAddModal(true); result.current.addForm.bindings.onNameChange('New'); result.current.addForm.setPath('/books');});
 get.mockResolvedValueOnce({data:{current:'/chosen',parent:'/',dirs:[],drives:[]}});
 await act(()=>result.current.openDirectoryBrowser());
 post.mockRejectedValueOnce(new Error('save failed'));
 await act(()=>result.current.handleAddLibrary({preventDefault(){}} as React.FormEvent));
 expect(result.current.showAddModal).toBe(true); expect(result.current.addForm.values.path).toBe('/books');
 expect(result.current.browseCurrent).toBe('/chosen');
 post.mockResolvedValueOnce({data:{saved:true,id:4,initial_scan:{admitted:false},warnings:['initial_scan_not_admitted']}});
 get.mockRejectedValueOnce(new Error('refresh failed'));
 await act(()=>result.current.handleAddLibrary({preventDefault(){}} as React.FormEvent));
 expect(result.current.showAddModal).toBe(false); expect(result.current.addForm.values.path).toBe('');
 expect(invalidate).toHaveBeenCalled(); expect(toast).toHaveBeenLastCalledWith('librarySave.refreshFailed','error');
});

it('keeps the edit draft and explains temporary occupancy from its API code',async()=>{
 get.mockResolvedValue({data:[{id:1,name:'Existing',path:'/original',scan_mode:'none',scan_formats:'cbz'}]});
 const {result}=renderHook(()=>useLibraryManagement(false,vi.fn()));
 await waitFor(()=>expect(result.current.libraries).toHaveLength(1));
 act(()=>{ result.current.openEditLibraryModal(1); });
 act(()=>{ result.current.editForm.setPath('/changed'); });
 axiosError.mockReturnValue(true); put.mockRejectedValueOnce({response:{data:{code:'library_unavailable',conflicts:[{reason:'wait_for_reads'}]}}});
 await act(()=>result.current.handleEditLibrarySubmit({preventDefault(){}} as React.FormEvent));
 expect(result.current.showEditModal).toBe(true); expect(result.current.editForm.values.path).toBe('/changed');
 expect(toast).toHaveBeenLastCalledWith('librarySave.unavailable','error');
});

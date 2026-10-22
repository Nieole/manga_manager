import React, { useState, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { LocaleProvider } from './src/i18n/LocaleProvider';
import { ThemeProvider } from './src/theme/ThemeProvider';
import { RelocationModal } from './src/components/layout/RelocationModal';
import { SidebarTaskBubble } from './src/components/SidebarTaskBubble';
import { apiClient } from './src/api/client';
import './src/index.css';

let tick = 0;
let activeProbe = 'original';
const cardCount = Number(new URLSearchParams(location.search).get('cards') ?? 72);
const plan = { id: 12, purpose: 'relocation', library_id: 1, library_name: 'GPU fixture (500,000 books)', library_root: 'E:\\Comics', source_root: 'E:\\Comics', target_root: 'F:\\Comics', run_id: 7, status: 'checking', series_count: 5000, book_count: 500000, blocker_count: 0, anomaly_count: 0, policy_count: 0, anomaly_counts: {}, samples: [], created_at: '', updated_at: '' };
apiClient.defaults.adapter = async (config) => {
  let data: unknown;
  const url = config.url || '';
  if (activeProbe === 'no-poll' && (url.includes('/tasks/live') || url.endsWith('/relocation-plans/12'))) return new Promise(() => {});
  if (url.includes('source-roots')) data = { library_root: plan.library_root, records_outside_current_root: false, candidates: [] };
  else if (url.includes('/details')) data = { items: [], total: 0, offset: 0, limit: 50 };
  else if (url.includes('/tasks/live')) data = { runs: [{ run_id: 7, status: 'running', phase: 'check', current: ++tick * 100, total: 500000, can_cancel: true }] };
  else if (url.includes('relocation-plans')) data = plan;
  else data = {};
  return { data, status: 200, statusText: 'OK', headers: {}, config };
};
function Fixture() {
  const [open, setOpen] = useState(false);
  const [probe, setProbe] = useState('original');
  const [tasks, setTasks] = useState(true);
  const [progress, setProgress] = useState(100000);
  useEffect(() => { const timer = window.setInterval(() => setProgress(n => n + 100), 200); return () => window.clearInterval(timer); }, []);
  return <><style>{probe === 'no-modal-blur' ? '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter: none !important; -webkit-backdrop-filter: none !important; }' : probe === 'no-blur' ? '[class*="backdrop-blur"] { backdrop-filter: none !important; -webkit-backdrop-filter: none !important; }' : probe === 'no-animation' ? '* { animation: none !important; transition: none !important; }' : ''}</style>
    <div className="relative min-h-screen bg-gray-950 p-6 text-gray-200">
      <h1 className="mb-4 text-xl">Isolated relocation fixture: 500,000 records</h1>
      <div className="grid grid-cols-8 gap-4">{Array.from({length: cardCount}, (_, i) => <article key={i} className="rounded-lg border border-gray-700 bg-gray-900 p-2"><div className="h-40 rounded" style={{background: `linear-gradient(${i * 13}deg, hsl(${i * 29} 70% 30%), hsl(${i * 29 + 100} 70% 55%))`}}/><p>Series {i + 1}</p><p className="text-xs text-gray-400">100 books</p></article>)}</div>
      {tasks && <div className="fixed bottom-5 left-5 w-72"><SidebarTaskBubble tasks={[{ taskId: 1, type: 'relocation_check', status: 'running', message: 'Checking record ' + progress + ' / 500,000', current: progress, total: 500000, updatedAt: Date.now() }]} onDismiss={() => {}} onClearFinished={() => {}} /></div>}
    </div>
    {open && <RelocationModal library={{id: 1, name: plan.library_name, path: plan.library_root}} onClose={() => setOpen(false)} />}
    <div style={{position:'fixed', zIndex: 9999, top:0, right:0, padding:8, background:'#111', color:'white', display:'flex', gap:8}}>
      <button onClick={() => setOpen(!open)}>{open ? 'Close fixture modal' : 'Open fixture modal'}</button>
      <select aria-label="Probe" value={probe} onChange={e=>{activeProbe=e.target.value;setProbe(e.target.value)}}><option value="original">original</option><option value="no-blur">no-blur</option><option value="no-modal-blur">no-modal-blur</option><option value="no-animation">no-animation</option><option value="no-poll">no-poll</option></select>
      <button onClick={()=>setTasks(!tasks)}>{tasks ? 'Hide task bubble' : 'Show task bubble'}</button>
    </div>
  </>;
}
createRoot(document.getElementById('root')!).render(<BrowserRouter><ThemeProvider><LocaleProvider><Fixture /></LocaleProvider></ThemeProvider></BrowserRouter>);

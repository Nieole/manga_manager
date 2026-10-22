import React, { useState, useEffect } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { LocaleProvider } from './src/i18n/LocaleProvider';
import { ThemeProvider } from './src/theme/ThemeProvider';
import { RelocationModal } from './src/components/layout/RelocationModal';
import { ModalShell } from './src/components/ui/ModalShell';
import { Loader2 } from 'lucide-react';
import { SidebarTaskBubble } from './src/components/SidebarTaskBubble';
import { apiClient } from './src/api/client';
import './src/index.css';

let tick = 0;
let activeProbe = 'original';
const cardCount = Number(new URLSearchParams(location.search).get('cards') ?? 72);
const sharedDialog = new URLSearchParams(location.search).get('dialog') === 'shared';
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
const probeStyles: Record<string, string> = {
  'single-blur': '.fixed.inset-0 > .grid > * { backdrop-filter:none !important; } .fixed.inset-0 > .absolute:not(.grid) { backdrop-filter:blur(4px) !important; }',
  'no-modal-blur': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter: none !important; -webkit-backdrop-filter: none !important; }',
  'no-blur': '[class*="backdrop-blur"] { backdrop-filter: none !important; -webkit-backdrop-filter: none !important; }',
  'no-animation': '* { animation: none !important; transition: none !important; }',
  'pause-background': '#root, #root *, #root::before, #root::after, #root *::before, #root *::after { animation-play-state: paused !important; transition: none !important; }',
  layer: '.fixed.inset-0 > .backdrop-blur-xs { transform: translateZ(0); will-change: transform; contain: paint; }',
  'root-filter': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter: none !important; } #root > .relative { filter: blur(4px); }',
  'root-static': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter: none !important; } #root > .relative { filter: blur(4px); } #root * { animation-play-state:paused !important; transition:none !important; }',
  'scale-2': '.fixed.inset-0 > .backdrop-blur-xs { inset:25% !important; transform:scale(2); backdrop-filter:blur(2px) !important; }',
  'scale-4': '.fixed.inset-0 > .backdrop-blur-xs { inset:37.5% !important; transform:scale(4); backdrop-filter:blur(1px) !important; }',
  'steps-12': '* { animation-timing-function:steps(12) !important; transition:none !important; }',
  tiles: '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter:none !important; }',
  'tiles-16': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter:none !important; }',
  'tiles-64': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter:none !important; }',
  'tiles-128': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter:none !important; }',
  'tiles-50': '.fixed.inset-0 > .backdrop-blur-xs { backdrop-filter:none !important; }',
};
function SharedDialog({ onClose }: { onClose: () => void }) {
  const [nested, setNested] = useState(false);
  return <>
    <ModalShell open onClose={onClose} title="Shared modal probe">
      <Loader2 data-testid="modal-spinner" className="h-6 w-6 animate-spin" />
      <p className="text-white">Foreground animation stays running</p>
      <button onClick={() => setNested(true)}>Open nested modal</button>
    </ModalShell>
    {nested && <ModalShell open onClose={() => setNested(false)} title="Nested modal probe">
      <Loader2 className="h-6 w-6 animate-spin" /><button onClick={() => setNested(false)}>Close nested modal</button>
    </ModalShell>}
  </>;
}
function Fixture() {
  const [open, setOpen] = useState(false);
  const [probe, setProbe] = useState('original');
  const [tasks, setTasks] = useState(true);
  const [progress, setProgress] = useState(100000);
  useEffect(() => { const timer = window.setInterval(() => setProgress(n => n + 100), 200); return () => window.clearInterval(timer); }, []);
  return <><style>{probeStyles[probe] || ''}</style>
    <div className="relative min-h-screen bg-gray-950 p-6 text-gray-200">
      <h1 className="mb-4 text-xl">Isolated relocation fixture: 500,000 records</h1>
      <div className="grid grid-cols-8 gap-4">{Array.from({length: cardCount}, (_, i) => <article key={i} className="rounded-lg border border-gray-700 bg-gray-900 p-2"><div className="h-40 rounded" style={{background: `linear-gradient(${i * 13}deg, hsl(${i * 29} 70% 30%), hsl(${i * 29 + 100} 70% 55%))`}}/><p>Series {i + 1}</p><p className="text-xs text-gray-400">100 books</p></article>)}</div>
      {tasks && <div className="fixed bottom-5 left-5 w-72"><SidebarTaskBubble tasks={[{ taskId: 1, type: 'relocation_check', status: 'running', message: 'Checking record ' + progress + ' / 500,000', current: progress, total: 500000, updatedAt: Date.now() }]} onDismiss={() => {}} onClearFinished={() => {}} /></div>}
    </div>
    {open && probe.startsWith('tiles') && <div style={{position:'fixed',inset:0,zIndex:49,pointerEvents:'none',display:'grid',gridTemplateColumns:probe === 'tiles-16' ? 'repeat(4,1fr)' : probe === 'tiles-50' ? 'repeat(10,1fr)' : probe === 'tiles-128' ? 'repeat(16,1fr)' : 'repeat(8,1fr)',gridTemplateRows:probe === 'tiles-50' ? 'repeat(5,1fr)' : ['tiles-64','tiles-128'].includes(probe) ? 'repeat(8,1fr)' : 'repeat(4,1fr)'}}>{Array.from({length:probe === 'tiles-16' ? 16 : probe === 'tiles-64' ? 64 : probe === 'tiles-128' ? 128 : probe === 'tiles-50' ? 50 : 32},(_,i)=><div key={i} style={{backdropFilter:'blur(4px)'}} />)}</div>}
    {open && (sharedDialog ? <SharedDialog onClose={() => setOpen(false)} /> : <RelocationModal library={{id: 1, name: plan.library_name, path: plan.library_root}} onClose={() => setOpen(false)} />)}
    <div style={{position:'fixed', zIndex: 9999, top:0, right:0, padding:8, background:'#111', color:'white', display:'flex', gap:8}}>
      <button onClick={() => setOpen(!open)}>{open ? 'Close fixture modal' : 'Open fixture modal'}</button>
      <select aria-label="Probe" value={probe} onChange={e=>{activeProbe=e.target.value;setProbe(e.target.value)}}><option value="original">original</option><option value="single-blur">single-blur</option><option value="root-static">root-static</option><option value="scale-2">scale-2</option><option value="scale-4">scale-4</option><option value="steps-12">steps-12</option><option value="tiles">tiles</option><option value="tiles-16">tiles-16</option><option value="tiles-64">tiles-64</option><option value="tiles-128">tiles-128</option><option value="tiles-50">tiles-50</option><option value="pause-background">pause-background</option><option value="layer">layer</option><option value="root-filter">root-filter</option><option value="no-blur">no-blur</option><option value="no-modal-blur">no-modal-blur</option><option value="no-animation">no-animation</option><option value="no-poll">no-poll</option></select>
      <button onClick={()=>setTasks(!tasks)}>{tasks ? 'Hide task bubble' : 'Show task bubble'}</button>
    </div>
  </>;
}
createRoot(document.getElementById('root')!).render(<BrowserRouter><ThemeProvider><LocaleProvider><Fixture /></LocaleProvider></ThemeProvider></BrowserRouter>);

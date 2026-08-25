let currentView = 'torrents';
let currentFilter = 'all';
let torrentsData = [];
let activeConfig = null;

// Search state
let rawSearchResults = [];
let currentSourceFilter = 'all';
let currentSortBy = 'relevance';

// Details Modal state
let currentDetailData = null;
let currentDetailTab = 'overview';

// Crisp SVG Icons (Libadwaita / Lucide style)
const ICONS = {
  magnet: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15v-4a6 6 0 1 1 12 0v4"></path><path d="M6 11H2"></path><path d="M22 11h-4"></path><path d="M2 15h4"></path><path d="M18 15h4"></path></svg>`,
  info: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>`,
  play: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>`,
  pause: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="6" y="4" width="4" height="16"></rect><rect x="14" y="4" width="4" height="16"></rect></svg>`,
  trash: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg>`,
  verify: `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -1px; margin-right: 3px;"><polyline points="23 4 23 10 17 10"></polyline><polyline points="1 20 1 14 7 14"></polyline><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path></svg>`,
  copy: `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>`,
  download: `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v14m0 0l-4-4m4 4l4-4M4 20h16"></path></svg>`,
  globe: `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -1px; margin-right: 3px;"><circle cx="12" cy="12" r="10"></circle><line x1="2" y1="12" x2="22" y2="12"></line><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"></path></svg>`,
  package: `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -1px; margin-right: 3px;"><line x1="16.5" y1="9.4" x2="7.5" y2="4.21"></line><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path><polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline><line x1="12" y1="22.08" x2="12" y2="12"></line></svg>`,
  arrowUp: `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -1px; margin-right: 2px;"><line x1="12" y1="19" x2="12" y2="5"></line><polyline points="5 12 12 5 19 12"></polyline></svg>`,
  arrowDown: `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -1px; margin-right: 2px;"><line x1="12" y1="5" x2="12" y2="19"></line><polyline points="19 12 12 19 5 12"></polyline></svg>`,
  zap: `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: -1px; margin-right: 3px;"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon></svg>`
};

// Helpers
function formatBytes(bytes, decimals = 1) {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const dm = decimals < 0 ? 0 : decimals;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;');
}

function formatSpeed(bytesPerSec) {
  return formatBytes(bytesPerSec) + '/s';
}

function formatETA(seconds) {
  if (!seconds || seconds <= 0 || seconds > 86400 * 7) return '';
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  const hrs = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  return `${hrs}h ${mins}m`;
}

function copyToClipboard(text, btnElement) {
  navigator.clipboard.writeText(text).then(() => {
    if (btnElement) {
      const orig = btnElement.innerHTML;
      btnElement.innerHTML = '✓ Copied!';
      setTimeout(() => { btnElement.innerHTML = orig; }, 1500);
    } else {
      alert("Copied to clipboard!");
    }
  }).catch(err => {
    prompt("Copy this:", text);
  });
}

function copyValue(inputId) {
  const el = document.getElementById(inputId);
  if (el) {
    copyToClipboard(el.value);
  }
}

// Navigation & Filters
function switchMainView(view) {
  currentView = view;
  document.getElementById('tab-torrents').classList.toggle('active', view === 'torrents');
  document.getElementById('tab-search').classList.toggle('active', view === 'search');
  
  document.getElementById('view-torrents').style.display = view === 'torrents' ? 'block' : 'none';
  document.getElementById('view-search').style.display = view === 'search' ? 'block' : 'none';
  document.getElementById('torrent-filters').style.display = view === 'torrents' ? 'flex' : 'none';
}

function setFilter(filter) {
  currentFilter = filter;
  const filterBtns = document.getElementById('torrent-filters').querySelectorAll('.view-btn');
  filterBtns.forEach(btn => {
    btn.classList.toggle('active', btn.textContent.toLowerCase() === filter);
  });
  renderTorrents();
}

// Real-time Event Stream (SSE)
function initEventStream() {
  const evtSource = new EventSource('/api/events');

  evtSource.onmessage = function(event) {
    try {
      const data = JSON.parse(event.data);
      if (data.torrents) {
        torrentsData = data.torrents;
        if (currentView === 'torrents') {
          renderTorrents();
        }
      }
      if (data.stats) {
        renderGlobalStats(data.stats);
      }
    } catch (err) {
      console.error("SSE parse error:", err);
    }
  };

  evtSource.onerror = function() {
    console.warn("SSE connection interrupted, retrying...");
  };
}

function renderGlobalStats(stats) {
  document.getElementById('stat-download-rate').textContent = `↓ ${formatSpeed(stats.download_rate)}`;
  document.getElementById('stat-upload-rate').textContent = `↑ ${formatSpeed(stats.upload_rate)}`;
  document.getElementById('stat-active-count').textContent = `${stats.active_count} active / ${stats.total_count} total`;
  let dhtText = `DHT: ${stats.dht_nodes} nodes`;
  if (stats.dht_indexed_count !== undefined && stats.dht_indexed_count > 0) {
    dhtText += ` • ${stats.dht_indexed_count} indexed`;
  }
  document.getElementById('stat-dht-nodes').textContent = dhtText;
}

// Torrent Rendering
function renderTorrents() {
  const container = document.getElementById('torrent-list-container');
  const emptyState = document.getElementById('torrents-empty');

  let filtered = [...torrentsData];
  if (currentFilter === 'downloading') {
    filtered = filtered.filter(t => t.state === 'downloading' || t.state === 'metadata');
  } else if (currentFilter === 'completed') {
    filtered = filtered.filter(t => t.state === 'seeding' || t.state === 'completed' || t.progress >= 100);
  }

  // Deterministic sort: newest first, then alphabetical by name
  filtered.sort((a, b) => {
    if ((b.added_at || 0) !== (a.added_at || 0)) {
      return (b.added_at || 0) - (a.added_at || 0);
    }
    return (a.name || '').localeCompare(b.name || '');
  });

  if (filtered.length === 0) {
    container.innerHTML = '';
    emptyState.style.display = 'block';
    return;
  }

  emptyState.style.display = 'none';

  container.innerHTML = filtered.map(t => {
    const isPaused = t.state === 'paused';
    const isSeeding = t.state === 'seeding' || t.state === 'completed';
    const isMeta = t.state === 'metadata';

    let metaString = `${formatBytes(t.completed_bytes)} of ${formatBytes(t.total_bytes)} (${t.progress.toFixed(1)}%)`;
    if (isMeta) {
      metaString = 'Downloading metadata from peers...';
    } else if (t.download_rate > 0) {
      metaString += ` • ↓ ${formatSpeed(t.download_rate)}`;
      const eta = formatETA(t.eta_seconds);
      if (eta) metaString += ` • ETA: ${eta}`;
    }
    if (t.upload_rate > 0) {
      metaString += ` • ↑ ${formatSpeed(t.upload_rate)}`;
    }
    metaString += ` • ${t.peers} peers`;
    if (t.webseeds && t.webseeds.length > 0) {
      metaString += ` • <span style="color: #57e389; font-weight: 600;">${ICONS.globe}${t.webseeds.length} WebSeed${t.webseeds.length > 1 ? 's' : ''}</span>`;
    }

    let swarmBanner = '';
    if (t.suggested_swarm) {
      const matchText = t.suggested_swarm.is_partial ?
        `⚡ <strong>Partial Match in Pack!</strong> "${t.suggested_swarm.name}" (${t.suggested_swarm.seeders} seeds). Upgrade to swarm?` :
        `⚡ <strong>Equivalent Swarm Found!</strong> Verified with ${t.suggested_swarm.seeders} seeds. Upgrade to hybrid swarm?`;
      swarmBanner = `
        <div class="swarm-suggestion-banner">
          <div>${matchText}</div>
          <button class="btn btn-primary" style="padding: 3px 10px; font-size: 11px; white-space: nowrap;" onclick="upgradeToSwarm('${t.info_hash}')">
            Upgrade to Swarm
          </button>
        </div>
      `;
    }

    const isWebDownload = t.magnet_uri && (t.magnet_uri.startsWith('http://') || t.magnet_uri.startsWith('https://'));

    return `
      <div class="torrent-card">
        <div class="card-header">
          <div class="torrent-title" title="${t.name}" style="cursor: pointer;" onclick="openDetailsModal('${t.info_hash}')">${t.name}</div>
          <span class="torrent-badge badge-${t.state}">${t.state}</span>
        </div>

        <div class="progress-bar-container" style="cursor: pointer;" onclick="openDetailsModal('${t.info_hash}')">
          <div class="progress-bar-fill ${isSeeding ? 'seeding' : ''}" style="width: ${Math.min(100, Math.max(0, t.progress))}%;"></div>
        </div>

        ${swarmBanner}

        <div class="card-footer">
          <div class="torrent-meta">${metaString}</div>
          <div class="card-actions">
            ${!isWebDownload ? 
              `<button class="btn btn-icon" title="Verify Local Data (Recheck)" onclick="verifyTorrent('${t.info_hash}', this)">${ICONS.verify}</button>` : ''
            }
            <button class="btn btn-icon" title="Copy Magnet / URL" onclick="copyToClipboard('${encodeURI(t.magnet_uri || '')}', this)">${ICONS.magnet}</button>
            <button class="btn btn-icon" title="Inspect Details & Peers" onclick="openDetailsModal('${t.info_hash}')">${ICONS.info}</button>
            ${isPaused ? 
              `<button class="btn btn-icon" title="Resume" onclick="resumeTorrent('${t.info_hash}')">${ICONS.play}</button>` :
              `<button class="btn btn-icon" title="Pause" onclick="pauseTorrent('${t.info_hash}')">${ICONS.pause}</button>`
            }
            <button class="btn btn-icon" style="color: var(--adw-error);" title="Delete" onclick="promptDeleteTorrent('${t.info_hash}', '${t.name.replace(/'/g, "\\'")}')">${ICONS.trash}</button>
          </div>
        </div>
      </div>
    `;
  }).join('');
}

async function verifyTorrent(hash, btn) {
  showToast("🔄 Rechecking and verifying local piece hashes...", "info", 3000);
  if (btn) btn.disabled = true;
  try {
    const res = await fetch(`/api/torrents/${hash}/verify`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("✓ Hash verification triggered! Rechecking data...", "accent", 3500);
    } else {
      showToast("Verification failed: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error verifying: " + err.message, "error", 4000);
  }
  if (btn) btn.disabled = false;
}

// Toast Notification System
function showToast(message, type = 'info', duration = 4000) {
  const container = document.getElementById('toast-container');
  if (!container) return;

  const toast = document.createElement('div');
  toast.className = `toast ${type === 'accent' ? 'toast-accent' : type === 'error' ? 'toast-error' : ''}`;
  toast.innerHTML = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateY(12px)';
    setTimeout(() => toast.remove(), 300);
  }, duration);
}

// Torrent Actions
async function pauseTorrent(hash) {
  await fetch(`/api/torrents/${hash}/pause`, { method: 'POST' });
}

async function resumeTorrent(hash) {
  await fetch(`/api/torrents/${hash}/resume`, { method: 'POST' });
}

async function triggerFindSwarm(hash, btn) {
  let orig = "";
  if (btn) {
    orig = btn.innerHTML;
    btn.disabled = true;
    btn.innerHTML = '<span style="font-size: 11px;">🔍</span>';
  }

  showToast("🔍 Scanning indexers & official host mirrors in background...", "info", 3000);

  try {
    const res = await fetch(`/api/torrents/${hash}/find-swarm`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      if (btn) btn.innerHTML = '✓';
      showToast("⚡ Equivalent BitTorrent swarm verified! Click 'Upgrade to Swarm' on card.", "accent", 5000);
      if (document.getElementById('modal-details').classList.contains('open')) {
        const dRes = await fetch(`/api/torrents/${hash}/details`);
        if (dRes.ok) {
          currentDetailData = await dRes.json();
          switchDetailTab('overview');
        }
      }
    } else {
      showToast("No equivalent BitTorrent swarm found on indexers for this file.", "info", 4000);
      if (btn) btn.innerHTML = orig;
    }
  } catch (err) {
    showToast("Swarm search error: " + err.message, "error", 4000);
    if (btn) btn.innerHTML = orig;
  }
  if (btn) btn.disabled = false;
}

async function upgradeToSwarm(hash) {
  try {
    const res = await fetch(`/api/torrents/${hash}/upgrade-to-swarm`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("🚀 Upgraded to hybrid P2P swarm with WebSeed acceleration!", "accent", 4000);
      if (document.getElementById('modal-details').classList.contains('open')) {
        closeDetailsModal();
      }
    } else {
      showToast("Upgrade failed: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error upgrading: " + err.message, "error", 4000);
  }
}

// Partial Download & File Priorities
async function toggleFileDownload(hash, fileIndex, checked) {
  const priority = checked ? 1 : 0;
  await updateFilePriority(hash, fileIndex, priority);
}

async function updateFilePriority(hash, fileIndex, priority) {
  try {
    await fetch(`/api/torrents/${hash}/files/${fileIndex}/priority`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ priority: parseInt(priority, 10) })
    });
    const res = await fetch(`/api/torrents/${hash}/details`);
    if (res.ok) {
      currentDetailData = await res.json();
      switchDetailTab('files');
    }
  } catch (err) {
    console.error("Error setting file priority:", err);
  }
}

async function setAllFilesPriority(hash, priority) {
  if (!currentDetailData || !currentDetailData.files) return;
  for (let i = 0; i < currentDetailData.files.length; i++) {
    const f = currentDetailData.files[i];
    const idx = f.index !== undefined ? f.index : i;
    await fetch(`/api/torrents/${hash}/files/${idx}/priority`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ priority: parseInt(priority, 10) })
    });
  }
  const res = await fetch(`/api/torrents/${hash}/details`);
  if (res.ok) {
    currentDetailData = await res.json();
    switchDetailTab('files');
  }
}

// Distinct Delete Confirmation
let pendingDeleteHash = null;

function promptDeleteTorrent(hash, name) {
  pendingDeleteHash = hash;
  document.getElementById('delete-modal-msg').textContent = `Are you sure you want to remove "${name}"?`;
  document.getElementById('modal-delete').classList.add('open');
}

function closeDeleteModal() {
  document.getElementById('modal-delete').classList.remove('open');
  pendingDeleteHash = null;
}

async function confirmDelete(deleteFiles) {
  if (!pendingDeleteHash) return;
  const hash = pendingDeleteHash;
  closeDeleteModal();
  try {
    const res = await fetch(`/api/torrents/${encodeURIComponent(hash)}?delete_files=${deleteFiles}`, { method: 'DELETE' });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("✓ Transfer removed.", "info", 2500);
      torrentsData = torrentsData.filter(t => t.info_hash !== hash);
      renderTorrents();
    } else {
      showToast("Delete failed: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error deleting: " + err.message, "error", 4000);
  }
}

async function openDownloadFolder() {
  await fetch('/api/open-folder', { method: 'POST' });
}

// Torrent Details Inspector
async function openDetailsModal(hash) {
  try {
    const res = await fetch(`/api/torrents/${hash}/details`);
    if (!res.ok) throw new Error("Could not load details");
    currentDetailData = await res.json();

    document.getElementById('detail-modal-title').textContent = currentDetailData.name || currentDetailData.info_hash;
    switchDetailTab('overview');
    document.getElementById('modal-details').classList.add('open');
  } catch (err) {
    alert("Error loading torrent details: " + err.message);
  }
}

function closeDetailsModal() {
  document.getElementById('modal-details').classList.remove('open');
  currentDetailData = null;
}

function switchDetailTab(tab) {
  currentDetailTab = tab;
  ['overview', 'files', 'peers', 'webseeds', 'trackers'].forEach(t => {
    const btn = document.getElementById(`dtab-${t}`);
    if (btn) btn.classList.toggle('active', t === tab);
  });

  const content = document.getElementById('detail-tab-content');
  if (!currentDetailData) return;

  if (tab === 'overview') {
    const suggHtml = currentDetailData.suggested_swarm ? `
      <div class="swarm-suggestion-banner" style="grid-column: 1 / -1; margin-bottom: 8px;">
        <div>
          ⚡ <strong>${currentDetailData.suggested_swarm.is_partial ? 'Partial Match in Collection!' : 'Equivalent BitTorrent Swarm Verified!'}</strong> (${currentDetailData.suggested_swarm.seeders} seeds).
        </div>
        <button class="btn btn-primary" style="padding: 3px 10px; font-size: 11px;" onclick="upgradeToSwarm('${currentDetailData.info_hash}')">
          Upgrade to Swarm
        </button>
      </div>
    ` : '';

    content.innerHTML = `
      <div class="detail-grid">
        ${suggHtml}

        <span class="detail-label">Name:</span>
        <span class="detail-val" style="font-weight: 600;">${currentDetailData.name}</span>

        <span class="detail-label">InfoHash / ID:</span>
        <div class="detail-code-box">
          <span>${currentDetailData.info_hash}</span>
          <button class="btn" style="padding: 2px 8px; font-size: 11px;" onclick="copyToClipboard('${currentDetailData.info_hash}', this)">Copy</button>
        </div>

        <span class="detail-label">Source / Magnet:</span>
        <div class="detail-code-box" style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
          <span style="overflow: hidden; text-overflow: ellipsis;">${currentDetailData.magnet_uri}</span>
          <button class="btn" style="padding: 2px 8px; font-size: 11px;" onclick="copyToClipboard('${encodeURI(currentDetailData.magnet_uri)}', this)">Copy</button>
        </div>

        <span class="detail-label">Total Size:</span>
        <span class="detail-val">${formatBytes(currentDetailData.total_bytes)}</span>

        <span class="detail-label">Downloaded:</span>
        <span class="detail-val">${formatBytes(currentDetailData.completed_bytes)} (${currentDetailData.progress.toFixed(1)}%)</span>

        <span class="detail-label">Pieces:</span>
        <span class="detail-val">${currentDetailData.num_pieces || '1'} pieces (${formatBytes(currentDetailData.piece_length || currentDetailData.total_bytes)} per piece)</span>

        <span class="detail-label">Storage Path:</span>
        <span class="detail-val" style="font-family: monospace;">${currentDetailData.download_dir}</span>

        <span class="detail-label">Data Integrity:</span>
        <div>
          <button class="btn" style="padding: 3px 10px; font-size: 11px;" onclick="verifyTorrent('${currentDetailData.info_hash}', this)">
            ${ICONS.verify} Force Recheck & Verify Pieces
          </button>
        </div>
      </div>
    `;
  } else if (tab === 'files') {
    if (!currentDetailData.files || currentDetailData.files.length === 0) {
      content.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 30px;">Files metadata resolving or single file...</div>';
      return;
    }
    const isTorrent = currentDetailData.created_by !== 'Multi-Source HTTP Downloader' && currentDetailData.created_by !== 'Direct HTTP Downloader';

    content.innerHTML = `
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px;">
        <span style="color: var(--adw-dim-label); font-size: 12px;">Total ${currentDetailData.files.length} file(s)</span>
        ${isTorrent ? `
          <div style="display: flex; gap: 6px;">
            <button class="btn" style="padding: 2px 8px; font-size: 11px;" onclick="setAllFilesPriority('${currentDetailData.info_hash}', 1)">Select All</button>
            <button class="btn" style="padding: 2px 8px; font-size: 11px;" onclick="setAllFilesPriority('${currentDetailData.info_hash}', 0)">Deselect All</button>
          </div>
        ` : ''}
      </div>
      <table class="adw-table">
        <thead>
          <tr>
            ${isTorrent ? '<th style="width: 32px;"></th>' : ''}
            <th>Filename</th>
            <th>Size</th>
            <th>Progress</th>
            ${isTorrent ? '<th style="width: 90px;">Priority</th>' : ''}
          </tr>
        </thead>
        <tbody>
          ${currentDetailData.files.map((f, i) => {
            const isIncluded = f.priority !== 0;
            const fileIdx = f.index !== undefined ? f.index : i;
            return `
              <tr>
                ${isTorrent ? `
                  <td>
                    <input type="checkbox" ${isIncluded ? 'checked' : ''} onchange="toggleFileDownload('${currentDetailData.info_hash}', ${fileIdx}, this.checked)">
                  </td>
                ` : ''}
                <td style="word-break: break-all; ${!isIncluded ? 'opacity: 0.5; text-decoration: line-through;' : ''}">${f.path}</td>
                <td style="white-space: nowrap;">${formatBytes(f.length)}</td>
                <td style="white-space: nowrap;">${f.progress.toFixed(0)}%</td>
                ${isTorrent ? `
                  <td>
                    <select class="sort-select" style="padding: 2px 4px; font-size: 11px;" onchange="updateFilePriority('${currentDetailData.info_hash}', ${fileIdx}, this.value)">
                      <option value="1" ${f.priority === 1 || f.priority === undefined ? 'selected' : ''}>Normal</option>
                      <option value="2" ${f.priority === 2 ? 'selected' : ''}>High</option>
                      <option value="0" ${f.priority === 0 ? 'selected' : ''}>Skip</option>
                    </select>
                  </td>
                ` : ''}
              </tr>
            `;
          }).join('')}
        </tbody>
      </table>
    `;
  } else if (tab === 'peers') {
    if (!currentDetailData.peers || currentDetailData.peers.length === 0) {
      content.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 30px;">No active peer connections at this moment.</div>';
      return;
    }
    content.innerHTML = `
      <table class="adw-table">
        <thead>
          <tr>
            <th>Peer IP / Address</th>
            <th>Connection Status</th>
          </tr>
        </thead>
        <tbody>
          ${currentDetailData.peers.map(p => `
            <tr>
              <td style="font-family: monospace;">${p.addr}</td>
              <td>${p.source || 'Active Swarm'}</td>
            </tr>
          `).join('')}
        </tbody>
      </table>
    `;
  } else if (tab === 'webseeds') {
    const seeds = currentDetailData.webseeds || [];
    content.innerHTML = `
      <div style="display: flex; flex-direction: column; gap: 12px;">
        <div style="font-size: 12.5px; color: var(--adw-dim-label);">
          Add direct HTTP/HTTPS seed URLs or CDN mirrors to accelerate this swarm (BEP 19):
        </div>
        <div style="display: flex; gap: 8px;">
          <input type="text" id="add-webseed-input" class="input-text" placeholder="https://mirror.example.com/file.iso">
          <button class="btn btn-primary" onclick="submitAddWebSeed('${currentDetailData.info_hash}')">Add Mirror</button>
        </div>
        <div style="margin-top: 8px;">
          <div style="font-weight: 600; font-size: 12px; margin-bottom: 6px;">Active WebSeed Mirrors (${seeds.length}):</div>
          ${seeds.length === 0 ? 
            '<div style="color: var(--adw-dim-label); font-size: 12px;">No custom HTTP seed locations added yet.</div>' :
            `<ul style="list-style: none; display: flex; flex-direction: column; gap: 6px;">
              ${seeds.map(s => `
                <li class="detail-code-box">
                  <span>${s}</span>
                  <span style="color: var(--adw-success); font-size: 11px;">Active</span>
                </li>
              `).join('')}
            </ul>`
          }
        </div>
      </div>
    `;
  } else if (tab === 'trackers') {
    if (!currentDetailData.trackers || currentDetailData.trackers.length === 0) {
      content.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 30px;">Using BitTorrent Mainline DHT & Peer Exchange (PEX).</div>';
      return;
    }
    content.innerHTML = `
      <ul style="list-style: none; display: flex; flex-direction: column; gap: 8px; padding: 6px;">
        ${currentDetailData.trackers.map(tr => `
          <li class="detail-code-box">
            <span>${tr}</span>
            <span style="color: var(--adw-success); font-size: 11px;">Active</span>
          </li>
        `).join('')}
      </ul>
    `;
  }
}

async function submitAddWebSeed(hash) {
  const input = document.getElementById('add-webseed-input');
  const url = input.value.trim();
  if (!url) return;

  try {
    const res = await fetch(`/api/torrents/${hash}/webseeds`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url })
    });
    if (res.ok) {
      if (!currentDetailData.webseeds) currentDetailData.webseeds = [];
      currentDetailData.webseeds.push(url);
      switchDetailTab('webseeds');
    } else {
      alert("Failed to attach WebSeed source.");
    }
  } catch (err) {
    alert("Error adding WebSeed: " + err.message);
  }
}

function copyCurrentMagnet() {
  if (currentDetailData && currentDetailData.magnet_uri) {
    copyToClipboard(currentDetailData.magnet_uri);
  }
}

function exportCurrentTorrentFile(btn) {
  if (!currentDetailData || !currentDetailData.info_hash) {
    showToast("Torrent details not loaded", "warning");
    return;
  }
  const hash = currentDetailData.info_hash;
  const link = document.createElement('a');
  link.href = `/api/torrents/${hash}/export`;
  link.download = `${currentDetailData.name || hash}.torrent`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  showToast("Downloading .torrent file...", "success", 2000);
}

// Send as Torrent (Create & Share)
let currentSendTab = 'local';

function switchSendTab(tab) {
  currentSendTab = tab;
  document.getElementById('send-tab-local').classList.toggle('active', tab === 'local');
  document.getElementById('send-tab-bridge').classList.toggle('active', tab === 'bridge');
  document.getElementById('send-view-local').style.display = tab === 'local' ? 'block' : 'none';
  document.getElementById('send-view-bridge').style.display = tab === 'bridge' ? 'block' : 'none';
  const btn = document.getElementById('btn-create-torrent');
  btn.textContent = tab === 'local' ? "Create & Start Seeding" : "Bridge & Seed to Swarm";
}

function openSendModal() {
  document.getElementById('send-form-view').style.display = 'block';
  document.getElementById('send-result-view').style.display = 'none';
  document.getElementById('send-path-input').value = '';
  document.getElementById('send-url-input').value = '';
  document.getElementById('send-comment-input').value = '';
  switchSendTab('local');
  document.getElementById('modal-send').classList.add('open');
}

function closeSendModal() {
  document.getElementById('modal-send').classList.remove('open');
}

async function submitCreateTorrent() {
  const comment = document.getElementById('send-comment-input').value.trim();
  const btn = document.getElementById('btn-create-torrent');

  if (currentSendTab === 'local') {
    const path = document.getElementById('send-path-input').value.trim();
    if (!path) {
      showToast("Please enter the absolute file or directory path to share.", "error");
      return;
    }

    btn.disabled = true;
    btn.textContent = "Hashing & Creating...";

    try {
      const res = await fetch('/api/torrents/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path, comment })
      });
      const data = await res.json();
      btn.disabled = false;
      btn.textContent = "Create & Start Seeding";

      if (data.status === 'ok') {
        document.getElementById('send-form-view').style.display = 'none';
        document.getElementById('send-result-view').style.display = 'flex';
        document.getElementById('created-magnet-val').value = data.magnet_uri;
        document.getElementById('created-hash-val').value = data.info_hash;
        showToast("✓ Torrent created and now seeding to DHT network!", "accent");
      } else {
        showToast("Failed to create torrent: " + (data.error || 'Unknown error'), "error");
      }
    } catch (err) {
      btn.disabled = false;
      btn.textContent = "Create & Start Seeding";
      showToast("Error creating torrent: " + err.message, "error");
    }
  } else {
    const url = document.getElementById('send-url-input').value.trim();
    if (!url) {
      showToast("Please enter a direct HTTP/HTTPS URL to bridge.", "error");
      return;
    }

    btn.disabled = true;
    btn.textContent = "Verifying & Bridging Swarm...";

    try {
      const res = await fetch('/api/torrents/create-bridge', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, comment })
      });
      const data = await res.json();
      btn.disabled = false;
      btn.textContent = "Bridge & Seed to Swarm";

      if (data.status === 'ok') {
        document.getElementById('send-form-view').style.display = 'none';
        document.getElementById('send-result-view').style.display = 'flex';
        document.getElementById('created-magnet-val').value = data.magnet_uri;
        document.getElementById('created-hash-val').value = data.info_hash;
        showToast("✓ Web mirror bridged to BitTorrent swarm with WebSeed acceleration!", "accent");
      } else {
        showToast("Bridge failed: " + (data.error || 'Unknown error'), "error");
      }
    } catch (err) {
      btn.disabled = false;
      btn.textContent = "Bridge & Seed to Swarm";
      showToast("Error creating bridge: " + err.message, "error");
    }
  }
}

// Search Functionality
async function handleSearchSubmit(e) {
  e.preventDefault();
  const input = document.getElementById('search-input');
  const query = input.value.trim();
  if (!query) return;

  const spinner = document.getElementById('search-spinner');
  const container = document.getElementById('search-results-container');
  const empty = document.getElementById('search-empty');
  const controls = document.getElementById('search-controls');

  spinner.style.display = 'block';
  container.innerHTML = '';
  empty.style.display = 'none';
  controls.style.display = 'none';

  try {
    const res = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
    rawSearchResults = await res.json() || [];

    spinner.style.display = 'none';

    if (rawSearchResults.length === 0) {
      empty.style.display = 'block';
      return;
    }

    currentSourceFilter = 'all';
    controls.style.display = 'flex';
    renderSourceFilterChips();
    renderSearchResults();
  } catch (err) {
    spinner.style.display = 'none';
    showToast("Search failed: " + err.message, "error", 3500);
  }
}

function renderSourceFilterChips() {
  const chipsGroup = document.getElementById('source-filter-chips');
  if (!chipsGroup) return;

  if (!rawSearchResults || rawSearchResults.length === 0) {
    chipsGroup.innerHTML = '';
    return;
  }

  // Aggregate counts per provider_type
  const counts = { all: rawSearchResults.length };
  const providerNames = {};

  rawSearchResults.forEach(r => {
    const pType = r.provider_type || 'other';
    counts[pType] = (counts[pType] || 0) + 1;
    if (r.provider && !providerNames[pType]) {
      providerNames[pType] = r.provider;
    }
  });

  const types = Object.keys(counts).filter(t => t !== 'all');

  let chipsHTML = `
    <button class="source-chip ${currentSourceFilter === 'all' ? 'active' : ''}" onclick="setSourceFilter('all')">
      All Sources <span style="opacity: 0.75; font-size: 10px; margin-left: 3px; font-weight: 700;">(${counts.all})</span>
    </button>
  `;

  types.forEach(t => {
    const name = providerNames[t] || t.toUpperCase();
    const count = counts[t];
    const isActive = currentSourceFilter === t;
    chipsHTML += `
      <button class="source-chip ${isActive ? 'active' : ''}" onclick="setSourceFilter('${escapeHtml(t)}')">
        ${escapeHtml(name)} <span style="opacity: 0.75; font-size: 10px; margin-left: 3px; font-weight: 700;">(${count})</span>
      </button>
    `;
  });

  chipsGroup.innerHTML = chipsHTML;
}

function setSourceFilter(source) {
  currentSourceFilter = source;
  renderSourceFilterChips();
  renderSearchResults();
}

function handleSortChange(sortType) {
  currentSortBy = sortType;
  renderSearchResults();
}

function renderSearchResults() {
  const container = document.getElementById('search-results-container');
  const empty = document.getElementById('search-empty');

  let filtered = rawSearchResults;
  if (currentSourceFilter !== 'all') {
    filtered = rawSearchResults.filter(r => r.provider_type === currentSourceFilter);
  }

  if (filtered.length === 0) {
    container.innerHTML = '';
    empty.style.display = 'block';
    return;
  }
  empty.style.display = 'none';

  // Client-side sorting
  const sorted = [...filtered].sort((a, b) => {
    if (currentSortBy === 'relevance') {
      return (b.score || 0) - (a.score || 0);
    } else if (currentSortBy === 'seeders') {
      const sA = a.seeders !== undefined && a.seeders >= 0 ? a.seeders : -1;
      const sB = b.seeders !== undefined && b.seeders >= 0 ? b.seeders : -1;
      return sB - sA;
    } else if (currentSortBy === 'size') {
      return (b.size_bytes || 0) - (a.size_bytes || 0);
    }
    return 0;
  });

  container.innerHTML = sorted.map((r, idx) => {
    const tagClass = `tag-${r.provider_type || 'torrentscsv'}`;
    const scoreText = r.score > 0 ? `<span class="score-badge">Relevance: ${r.score.toFixed(0)}</span>` : '';
    const fileCountBadge = (r.files && r.files.length > 0) ? `<span style="font-size: 11px; opacity: 0.85;">📁 ${r.files.length} ${r.files.length === 1 ? 'file' : 'files'}</span>` : '';

    let seedersHtml = '';
    if (r.seeders !== undefined && r.seeders >= 0) {
      const seedColor = r.seeders > 0 ? 'var(--adw-success)' : 'var(--adw-dim-label)';
      seedersHtml = `<span style="color: ${seedColor}; font-weight: 600;">${ICONS.arrowUp}${r.seeders} seeds</span>`;
    } else {
      seedersHtml = `<span class="seed-probe-btn" style="color: var(--adw-dim-label); opacity: 0.85; cursor: pointer; text-decoration: underline dotted;" title="Swarm health unknown. Click to probe live swarm." onclick="scrapeSwarmCard(${idx}, event)">${ICONS.arrowUp}? seeds</span>`;
    }

    let leechersHtml = '';
    if (r.leechers !== undefined && r.leechers >= 0) {
      leechersHtml = `<span>${ICONS.arrowDown}${r.leechers} peers</span>`;
    } else {
      leechersHtml = `<span style="color: var(--adw-dim-label); opacity: 0.85;" title="Peer count unknown">${ICONS.arrowDown}? peers</span>`;
    }

    let healthBadge = '';
    if (r.health) {
      if (r.health.status === 'periodic') {
        const titleTooltip = escapeHtml(`Historical Swarm Pattern: ${r.health.description || ''}`);
        const peakText = r.health.peak_window ? `~${r.health.peak_window}` : 'Periodic';
        healthBadge = `<span class="health-badge health-periodic" style="background: rgba(230, 162, 60, 0.15); color: #e6a23c; border: 1px solid rgba(230, 162, 60, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500;" title="${titleTooltip}">🟡 ${peakText}</span>`;
      } else if (r.health.status === 'dormant') {
        const titleTooltip = escapeHtml(`Swarm History: ${r.health.description || 'No active seeders recorded in checks'}`);
        healthBadge = `<span class="health-badge health-dormant" style="background: rgba(245, 108, 108, 0.15); color: #f56c6c; border: 1px solid rgba(245, 108, 108, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500;" title="${titleTooltip}">🔴 Dormant</span>`;
      } else if (r.health.status === 'active') {
        healthBadge = `<span class="health-badge health-active" style="background: rgba(103, 194, 58, 0.15); color: #67c23a; border: 1px solid rgba(103, 194, 58, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500;" title="Live seeders active right now">🟢 Active</span>`;
      }
    }

    return `
      <div class="search-card">
        <div class="search-info">
          <div class="search-title" title="${escapeHtml(r.title)}">${escapeHtml(r.title)}</div>
          <div class="search-sub">
            <span>${ICONS.package}${formatBytes(r.size_bytes)}</span>
            ${seedersHtml}
            ${leechersHtml}
            <span class="provider-badge ${tagClass}">${escapeHtml(r.provider)}</span>
            ${healthBadge}
            ${fileCountBadge}
            ${scoreText}
          </div>
        </div>
        <div style="display: flex; gap: 6px; align-items: center;">
          <button class="btn" title="Inspect files inside this torrent" onclick="openInspectModal(${idx})">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="vertical-align: -1px; margin-right: 3px;"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>
            <span>Files</span>
          </button>
          <button class="btn btn-icon" title="Copy Magnet" onclick="copyToClipboard('${encodeURI(r.magnet_uri)}', this)">${ICONS.magnet}</button>
          <button class="btn btn-primary" onclick="downloadFromSearch('${encodeURIComponent(r.magnet_uri)}', this)">
            ${ICONS.download}
            <span>Download</span>
          </button>
        </div>
      </div>
    `;
  }).join('');
}

async function scrapeSwarmCard(idx, event) {
  if (event) {
    event.stopPropagation();
  }
  const result = rawSearchResults[idx];
  if (!result) return;

  const targetEl = event ? event.currentTarget : null;
  if (targetEl) {
    targetEl.innerHTML = '⏳ probing...';
  }

  try {
    const res = await fetch(`/api/torrents/scrape?hash=${encodeURIComponent(result.info_hash)}&magnet=${encodeURIComponent(result.magnet_uri)}`);
    if (!res.ok) {
      throw new Error("Probe timed out");
    }
    const data = await res.json();
    if (data.seeders !== undefined && data.seeders >= 0) {
      result.seeders = data.seeders;
      result.leechers = data.leechers !== undefined ? data.leechers : 0;
      result.health = {
        status: data.seeders > 0 ? 'active' : 'dormant',
        description: data.seeders > 0 ? `${data.seeders} live seeders active` : '0 seeders seen',
        peak_window: data.seeders > 0 ? 'Active Now' : ''
      };
    }
  } catch (err) {
    if (targetEl) {
      targetEl.innerHTML = `${ICONS.arrowUp}0 seeds`;
      result.seeders = 0;
      result.leechers = 0;
      result.health = { status: 'dormant', description: 'Probe timed out (no responding peers)' };
    }
  }
  renderSearchResults();
}

let currentInspectData = null;
let currentInspectFiles = [];

async function openInspectModal(resultIdx) {
  const result = rawSearchResults[resultIdx];
  if (!result) return;

  const modal = document.getElementById('modal-inspect');
  const titleEl = document.getElementById('inspect-title');
  const sizeEl = document.getElementById('inspect-size');
  const countEl = document.getElementById('inspect-file-count');
  const hashEl = document.getElementById('inspect-hash');
  const filesContainer = document.getElementById('inspect-files-container');
  const filterInput = document.getElementById('inspect-file-filter');
  if (filterInput) filterInput.value = '';

  modal.classList.add('open');

  titleEl.textContent = result.title || 'Unknown Torrent';
  sizeEl.textContent = `Total Size: ${formatBytes(result.size_bytes)}`;
  countEl.textContent = 'Files: ...';
  hashEl.textContent = `Hash: ${result.info_hash || '—'}`;
  hashEl.dataset.hash = result.info_hash || '';

  currentInspectData = result;

  // If the search result already contains files list
  if (result.files && result.files.length > 0) {
    currentInspectFiles = result.files.map((f, i) => ({
      index: i,
      path: f.path || f,
      length: f.size_bytes || 0
    }));
    renderInspectFiles(currentInspectFiles);
    countEl.textContent = `Files: ${currentInspectFiles.length}`;
    return;
  }

  // Otherwise, query live BEP 9 metadata / DHT inspect endpoint
  filesContainer.innerHTML = `
    <div style="text-align: center; padding: 40px; color: var(--adw-dim-label);">
      <div style="font-size: 24px; margin-bottom: 8px;">⏳</div>
      <div style="font-weight: 600; margin-bottom: 4px; color: var(--adw-fg-color);">Resolving torrent metadata...</div>
      <div style="font-size: 11.5px;">Fetching file directory structure from DHT swarm peers</div>
    </div>
  `;

  try {
    const res = await fetch(`/api/torrents/inspect?hash=${encodeURIComponent(result.info_hash)}&magnet=${encodeURIComponent(result.magnet_uri)}`);
    if (!res.ok) {
      throw new Error("Metadata resolution timed out (no DHT peers responded in 8s). You can still start the download directly.");
    }
    const data = await res.json();
    if (data.name && data.name.trim() !== '') {
      result.title = data.name;
    }
    if (data.total_size && data.total_size > 0) {
      result.size_bytes = data.total_size;
    }
    if (data.magnet_uri) {
      result.magnet_uri = data.magnet_uri;
    }
    if (data.seeders !== undefined && data.seeders >= 0) {
      result.seeders = data.seeders;
      result.leechers = data.leechers !== undefined ? data.leechers : 0;
      result.health = {
        status: data.seeders > 0 ? 'active' : 'dormant',
        description: data.seeders > 0 ? `${data.seeders} live seeders active` : '0 seeders seen',
        peak_window: data.seeders > 0 ? 'Active Now' : ''
      };
    }

    currentInspectData = {
      ...result,
      magnet_uri: data.magnet_uri || result.magnet_uri,
      title: result.title,
      size_bytes: result.size_bytes
    };

    titleEl.textContent = currentInspectData.title;
    sizeEl.textContent = `Total Size: ${formatBytes(currentInspectData.size_bytes)}`;
    countEl.textContent = `Files: ${data.files ? data.files.length : 0}`;

    currentInspectFiles = (data.files || []).map((f, i) => ({
      index: f.index !== undefined ? f.index : i,
      path: f.path,
      length: f.length
    }));

    // Cache files on the search result item so subsequent inspect clicks are instant!
    result.files = currentInspectFiles;

    renderInspectFiles(currentInspectFiles);
    // Refresh search results list to reflect resolved title and file count
    renderSearchResults();
  } catch (err) {
    filesContainer.innerHTML = `
      <div style="text-align: center; padding: 35px 20px; color: var(--adw-dim-label);">
        <div style="font-size: 22px; margin-bottom: 6px; color: var(--adw-warning);">⚠️</div>
        <div style="font-size: 13px; font-weight: 600; color: var(--adw-fg-color); margin-bottom: 4px;">Live Metadata Swarm Lookup</div>
        <div style="font-size: 12px; margin-bottom: 12px;">${escapeHtml(err.message)}</div>
        <div style="font-size: 11.5px; opacity: 0.8;">Single file torrent: <strong style="color: var(--adw-fg-color);">${escapeHtml(result.title)}</strong> (${formatBytes(result.size_bytes)})</div>
      </div>
    `;
    countEl.textContent = 'Files: 1';
  }
}

function renderInspectFiles(files) {
  const container = document.getElementById('inspect-files-container');
  if (!container) return;

  if (!files || files.length === 0) {
    container.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 30px;">No files found.</div>';
    return;
  }

  const getIconForFile = (path) => {
    const ext = path.split('.').pop().toLowerCase();
    if (['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'ts'].includes(ext)) return '🎬';
    if (['mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'opus', 'wma'].includes(ext)) return '🎵';
    if (['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'iso'].includes(ext)) return '📦';
    if (['pdf', 'epub', 'mobi', 'doc', 'docx', 'txt', 'rtf'].includes(ext)) return '📄';
    if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp'].includes(ext)) return '🖼️';
    if (['exe', 'msi', 'deb', 'rpm', 'apk', 'dmg', 'AppImage'].includes(ext)) return '⚙️';
    return '📄';
  };

  container.innerHTML = files.map(f => {
    const sizeStr = f.length > 0 ? formatBytes(f.length) : '';
    const icon = getIconForFile(f.path);
    return `
      <div style="display: flex; justify-content: space-between; align-items: center; padding: 7px 12px; border-bottom: 1px solid rgba(128,128,128,0.1); font-size: 12px;">
        <div style="display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; padding-right: 12px;">
          <span style="font-size: 14px;">${icon}</span>
          <span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis;" title="${escapeHtml(f.path)}">${escapeHtml(f.path)}</span>
        </div>
        <div style="color: var(--adw-dim-label); font-weight: 500; font-size: 11.5px; white-space: nowrap;">${sizeStr}</div>
      </div>
    `;
  }).join('');
}

function filterInspectFiles(query) {
  query = (query || '').toLowerCase().trim();
  if (!query) {
    renderInspectFiles(currentInspectFiles);
    return;
  }
  const filtered = currentInspectFiles.filter(f => f.path.toLowerCase().includes(query));
  renderInspectFiles(filtered);
}

function closeInspectModal() {
  document.getElementById('modal-inspect').classList.remove('open');
  currentInspectData = null;
  currentInspectFiles = [];
}

function copyInspectMagnet(btn) {
  if (currentInspectData && currentInspectData.magnet_uri) {
    copyToClipboard(currentInspectData.magnet_uri, btn);
  }
}

function saveInspectTorrentFile(btn) {
  if (!currentInspectData || !currentInspectData.info_hash) {
    showToast("Metadata not loaded yet", "warning");
    return;
  }
  const hash = currentInspectData.info_hash;
  const link = document.createElement('a');
  link.href = `/api/torrents/${hash}/export`;
  link.download = `${currentInspectData.name || hash}.torrent`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  showToast("Downloading .torrent file...", "success", 2000);
}

async function startDownloadFromInspect(btn) {
  if (!currentInspectData || !currentInspectData.magnet_uri) return;
  const uri = currentInspectData.magnet_uri;
  closeInspectModal();
  await downloadFromSearch(encodeURIComponent(uri), btn);
}

async function downloadFromSearch(encodedURI, btn) {
  const uri = decodeURIComponent(encodedURI);
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `<span style="opacity: 0.7;">Adding...</span>`;
  }
  showToast("Adding download...", "info", 1500);

  try {
    const res = await fetch('/api/torrents/add', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: uri })
    });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("✓ Transfer started!", "info", 2500);
      switchMainView('torrents');
    } else {
      showToast("Failed to add download: " + (data.error || 'Unknown error'), "error", 4000);
      if (btn) {
        btn.disabled = false;
        btn.innerHTML = `${ICONS.download}<span>Download</span>`;
      }
    }
  } catch (err) {
    showToast("Download request failed: " + err.message, "error", 4000);
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `${ICONS.download}<span>Download</span>`;
    }
  }
}

// Add Modal
function openAddModal() {
  document.getElementById('modal-add').classList.add('open');
  document.getElementById('add-magnet-input').focus();
}

function closeAddModal() {
  document.getElementById('modal-add').classList.remove('open');
  document.getElementById('add-magnet-input').value = '';
  document.getElementById('add-file-input').value = '';
}

async function submitAddTorrent() {
  const inputVal = document.getElementById('add-magnet-input').value.trim();
  const fileInput = document.getElementById('add-file-input');

  if (fileInput.files.length > 0) {
    closeAddModal();
    showToast("Adding .torrent file...", "info", 1800);
    try {
      const formData = new FormData();
      formData.append('torrent_file', fileInput.files[0]);
      const res = await fetch('/api/torrents/add', { method: 'POST', body: formData });
      const data = await res.json();
      if (data.status === 'ok') {
        showToast("✓ Transfer added!", "info", 2500);
        switchMainView('torrents');
      } else {
        showToast("Failed: " + (data.error || 'Unknown error'), "error", 4000);
      }
    } catch (err) {
      showToast("Request error: " + err.message, "error", 4000);
    }
    return;
  }

  if (inputVal) {
    closeAddModal();
    showToast("Adding download...", "info", 1800);
    try {
      const res = await fetch('/api/torrents/add', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: inputVal })
      });
      const data = await res.json();
      if (data.status === 'ok') {
        showToast("✓ Transfer started!", "info", 2500);
        switchMainView('torrents');
      } else {
        showToast("Failed: " + (data.error || 'Unknown error'), "error", 4000);
      }
    } catch (err) {
      showToast("Request error: " + err.message, "error", 4000);
    }
    return;
  }

  showToast("Please enter a magnet link, HTTP direct URL, infohash, or choose a .torrent file.", "warning", 3000);
}

// Settings & Providers Modal Management
let activeSettingsTab = 'providers';
let editingProviderIndex = -1; // -1 for new provider

async function openSettingsModal() {
  try {
    const res = await fetch('/api/config');
    activeConfig = await res.json();

    const dlDirEl = document.getElementById('cfg-download-dir');
    if (dlDirEl) dlDirEl.value = activeConfig.download_dir || '';

    const dlLimEl = document.getElementById('cfg-dl-limit');
    if (dlLimEl) dlLimEl.value = activeConfig.download_limit_kb || '';

    const ulLimEl = document.getElementById('cfg-ul-limit');
    if (ulLimEl) ulLimEl.value = activeConfig.upload_limit_kb || '';

    const dhtEl = document.getElementById('cfg-enable-dht');
    if (dhtEl) dhtEl.checked = activeConfig.enable_dht !== false;

    const upnpEl = document.getElementById('cfg-enable-upnp');
    if (upnpEl) upnpEl.checked = activeConfig.enable_upnp !== false;

    const dnsEl = document.getElementById('cfg-fallback-dns');
    if (dnsEl) dnsEl.value = (activeConfig.fallback_dns || ['8.8.8.8:53', '1.1.1.1:53', '8.8.4.4:53', '9.9.9.9:53']).join(', ');

    renderProvidersList();
    switchSettingsTab('providers');
    const modal = document.getElementById('modal-settings');
    if (modal) modal.classList.add('open');
  } catch (err) {
    console.error('Error opening settings modal:', err);
    showToast('Failed to open settings: ' + err.message, 'error', 3500);
  }
}

function closeSettingsModal() {
  document.getElementById('modal-settings').classList.remove('open');
}

function switchSettingsTab(tab) {
  activeSettingsTab = tab;
  ['providers', 'general', 'yaml'].forEach(t => {
    const btn = document.getElementById(`stab-${t}`);
    const view = document.getElementById(`settings-tab-${t}`);
    if (btn) btn.classList.toggle('active', t === tab);
    if (view) view.style.display = t === tab ? 'flex' : 'none';
  });

  if (tab === 'yaml') {
    loadRawYAML();
  }
}

function renderProvidersList() {
  const provList = document.getElementById('cfg-providers-list');
  if (!activeConfig || !activeConfig.search_providers || activeConfig.search_providers.length === 0) {
    provList.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 20px;">No search providers configured. Click "+ Add Provider" or "Reset Defaults".</div>';
    return;
  }

  provList.innerHTML = activeConfig.search_providers.map((p, idx) => {
    let typeBadge = p.type.toUpperCase();
    let badgeColor = 'rgba(53, 132, 228, 0.2)';
    let badgeTextColor = '#78aeed';

    if (p.type === 'btdig' || p.type === 'bitsearch' || p.type === 'dht') {
      badgeColor = 'rgba(230, 97, 0, 0.2)';
      badgeTextColor = '#ff9e3b';
      typeBadge = 'DHT';
    } else if (p.type === 'yts' || p.type === 'eztv') {
      badgeColor = 'rgba(154, 99, 212, 0.2)';
      badgeTextColor = '#dc8add';
      typeBadge = p.type === 'yts' ? 'MOVIES' : 'TV';
    } else if (p.type.includes('json') || p.type.includes('html')) {
      badgeColor = 'rgba(51, 209, 122, 0.2)';
      badgeTextColor = '#57e389';
      typeBadge = 'CUSTOM';
    }

    return `
      <div style="display: flex; align-items: center; justify-content: space-between; background: rgba(0,0,0,0.18); border: 1px solid var(--adw-border); padding: 10px 14px; border-radius: 8px; gap: 12px;">
        <div style="display: flex; align-items: center; gap: 10px; flex: 1; min-width: 0;">
          <input type="checkbox" id="cfg-prov-enabled-${idx}" ${p.enabled ? 'checked' : ''} onchange="activeConfig.search_providers[${idx}].enabled=this.checked" style="cursor: pointer;">
          <div style="overflow: hidden;">
            <div style="display: flex; align-items: center; gap: 6px;">
              <span style="font-size: 13px; font-weight: 600; text-overflow: ellipsis; overflow: hidden; white-space: nowrap;">${escapeHtml(p.name)}</span>
              <span style="font-size: 9.5px; font-weight: 700; background: ${badgeColor}; color: ${badgeTextColor}; padding: 1px 5px; border-radius: 4px;">${typeBadge}</span>
            </div>
            <div style="font-size: 11px; color: var(--adw-dim-label); text-overflow: ellipsis; overflow: hidden; white-space: nowrap;">${escapeHtml(p.url || p.type)}</div>
          </div>
        </div>

        <div style="display: flex; align-items: center; gap: 8px; flex-shrink: 0;">
          <select id="cfg-prov-weight-${idx}" class="sort-select" style="padding: 2px 6px; font-size: 11.5px;" onchange="activeConfig.search_providers[${idx}].weight=parseFloat(this.value)||1.0">
            <option value="1.5" ${p.weight >= 1.4 ? 'selected' : ''}>High (1.5x)</option>
            <option value="1.2" ${p.weight >= 1.15 && p.weight < 1.4 ? 'selected' : ''}>Elevated (1.2x)</option>
            <option value="1.0" ${p.weight >= 0.85 && p.weight < 1.15 ? 'selected' : ''}>Normal (1.0x)</option>
            <option value="0.7" ${p.weight >= 0.55 && p.weight < 0.85 ? 'selected' : ''}>Moderate (0.7x)</option>
            <option value="0.4" ${p.weight < 0.55 ? 'selected' : ''}>Low (0.4x)</option>
          </select>
          <button class="btn" style="padding: 3px 8px; font-size: 11px;" id="btn-test-prov-${idx}" onclick="testSingleProvider(${idx}, this)" title="Test live latency and results">⚡ Test</button>
          <button class="btn" style="padding: 3px 8px; font-size: 11px;" onclick="openEditProviderModal(${idx})" title="Edit provider details">✏️</button>
          <button class="btn" style="padding: 3px 8px; font-size: 11px; color: var(--adw-error);" onclick="deleteProvider(${idx})" title="Remove provider">🗑️</button>
        </div>
      </div>
    `;
  }).join('');
}

async function testSingleProvider(idx, btnEl) {
  const p = activeConfig.search_providers[idx];
  if (!p) return;

  const originalText = btnEl.textContent;
  btnEl.textContent = '...';
  btnEl.disabled = true;

  try {
    const res = await fetch('/api/providers/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider: p, query: 'ubuntu' })
    });
    const data = await res.json();
    if (data.ok) {
      showToast(`✓ ${p.name}: ${data.count} hits (${data.duration_ms}ms)`, "success", 3000);
      btnEl.textContent = `✓ ${data.count} (${data.duration_ms}ms)`;
    } else {
      showToast(`✕ ${p.name} failed: ${data.error || 'Timeout'}`, "warning", 3500);
      btnEl.textContent = '✕ Fail';
    }
  } catch (err) {
    showToast(`Error testing ${p.name}: ${err.message}`, "error", 3000);
    btnEl.textContent = '✕ Error';
  } finally {
    setTimeout(() => {
      btnEl.textContent = originalText;
      btnEl.disabled = false;
    }, 4000);
  }
}

function openAddProviderModal() {
  editingProviderIndex = -1;
  document.getElementById('provider-edit-title').textContent = 'Add Search Provider';
  document.getElementById('ped-name').value = '';
  document.getElementById('ped-type').value = 'btdig';
  document.getElementById('ped-weight').value = '1.0';
  document.getElementById('ped-url').value = 'https://';
  document.getElementById('ped-apikey').value = '';
  document.getElementById('ped-enabled').checked = true;
  document.getElementById('ped-test-feedback').style.display = 'none';

  onProviderTypeChange();
  document.getElementById('modal-provider-edit').classList.add('open');
}

function openEditProviderModal(idx) {
  editingProviderIndex = idx;
  const p = activeConfig.search_providers[idx];
  if (!p) return;

  document.getElementById('provider-edit-title').textContent = `Edit Provider: ${p.name}`;
  document.getElementById('ped-name').value = p.name || '';
  document.getElementById('ped-type').value = p.type || 'btdig';
  document.getElementById('ped-weight').value = (p.weight || 1.0).toFixed(1);
  document.getElementById('ped-url').value = p.url || '';
  document.getElementById('ped-apikey').value = p.api_key || '';
  document.getElementById('ped-enabled').checked = p.enabled !== false;

  document.getElementById('ped-json-results').value = p.results_path || '';
  document.getElementById('ped-json-title').value = p.title_path || '';
  document.getElementById('ped-json-hash').value = p.hash_path || '';
  document.getElementById('ped-json-magnet').value = p.magnet_path || '';
  document.getElementById('ped-json-size').value = p.size_path || '';
  document.getElementById('ped-json-seeds').value = p.seeds_path || '';

  document.getElementById('ped-html-row').value = p.row_regex || '';
  document.getElementById('ped-html-title').value = p.title_regex || '';
  document.getElementById('ped-html-magnet').value = p.magnet_regex || '';

  document.getElementById('ped-test-feedback').style.display = 'none';

  onProviderTypeChange();
  document.getElementById('modal-provider-edit').classList.add('open');
}

function closeProviderEditModal() {
  document.getElementById('modal-provider-edit').classList.remove('open');
}

function onProviderTypeChange() {
  const type = document.getElementById('ped-type').value;
  const apikeyGroup = document.getElementById('ped-apikey-group');
  const jsonRules = document.getElementById('ped-json-rules');
  const htmlRules = document.getElementById('ped-html-rules');
  const urlInput = document.getElementById('ped-url');

  apikeyGroup.style.display = (type === 'torznab' || type === 'generic_json') ? 'block' : 'none';
  jsonRules.style.display = type === 'generic_json' ? 'flex' : 'none';
  htmlRules.style.display = type === 'generic_html' ? 'flex' : 'none';

  // Preset default URLs if empty or modifying template
  if (!urlInput.value || urlInput.value === 'https://' || urlInput.dataset.auto) {
    urlInput.dataset.auto = 'true';
    switch (type) {
      case 'btdig': urlInput.value = 'https://btdig.com'; break;
      case 'bitsearch': urlInput.value = 'https://bitsearch.to'; break;
      case 'apibay': urlInput.value = 'https://apibay.org'; break;
      case 'eztv': urlInput.value = 'https://eztv.re'; break;
      case 'yts': urlInput.value = 'https://yts.mx'; break;
      case 'solidtorrents': urlInput.value = 'https://solidtorrents.to'; break;
      case 'torrentscsv': urlInput.value = 'https://torrents-csv.com'; break;
      case 'limetorrents': urlInput.value = 'https://www.limetorrents.lol'; break;
      case 'torlock': urlInput.value = 'https://www.torlock.com'; break;
      case 'archiveorg': urlInput.value = 'https://archive.org'; break;
      case 'torznab': urlInput.value = 'http://localhost:9696/api/v1/search'; break;
      case 'generic_json': urlInput.value = 'https://example.com/api/search?q={query}'; break;
      case 'generic_html': urlInput.value = 'https://example.com/search?q={query}'; break;
    }
  }
}

async function testCurrentProviderEdit() {
  const feedback = document.getElementById('ped-test-feedback');
  feedback.style.display = 'block';
  feedback.style.background = 'rgba(53, 132, 228, 0.2)';
  feedback.style.color = '#78aeed';
  feedback.textContent = 'Testing connection and querying sample data...';

  const prov = {
    name: document.getElementById('ped-name').value.trim() || 'Test Provider',
    type: document.getElementById('ped-type').value,
    url: document.getElementById('ped-url').value.trim(),
    api_key: document.getElementById('ped-apikey').value.trim(),
    enabled: true,
    weight: parseFloat(document.getElementById('ped-weight').value) || 1.0,
    results_path: document.getElementById('ped-json-results').value.trim(),
    title_path: document.getElementById('ped-json-title').value.trim(),
    hash_path: document.getElementById('ped-json-hash').value.trim(),
    magnet_path: document.getElementById('ped-json-magnet').value.trim(),
    size_path: document.getElementById('ped-json-size').value.trim(),
    seeds_path: document.getElementById('ped-json-seeds').value.trim(),
    row_regex: document.getElementById('ped-html-row').value.trim(),
    title_regex: document.getElementById('ped-html-title').value.trim(),
    magnet_regex: document.getElementById('ped-html-magnet').value.trim(),
  };

  try {
    const res = await fetch('/api/providers/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ provider: prov, query: 'ubuntu' })
    });
    const data = await res.json();
    if (data.ok) {
      feedback.style.background = 'rgba(46, 194, 126, 0.2)';
      feedback.style.color = '#57e389';
      feedback.textContent = `✓ Success! Retrieved ${data.count} torrents in ${data.duration_ms}ms.`;
    } else {
      feedback.style.background = 'rgba(224, 27, 36, 0.2)';
      feedback.style.color = '#ff7b63';
      feedback.textContent = `✕ Test Failed: ${data.error || 'Could not parse response'}`;
    }
  } catch (err) {
    feedback.style.background = 'rgba(224, 27, 36, 0.2)';
    feedback.style.color = '#ff7b63';
    feedback.textContent = `✕ Network Error: ${err.message}`;
  }
}

function saveProviderEdit() {
  const name = document.getElementById('ped-name').value.trim();
  const url = document.getElementById('ped-url').value.trim();
  if (!name || !url) {
    showToast("Please provide a name and valid URL for the provider.", "warning", 3000);
    return;
  }

  const prov = {
    name: name,
    type: document.getElementById('ped-type').value,
    url: url,
    api_key: document.getElementById('ped-apikey').value.trim(),
    enabled: document.getElementById('ped-enabled').checked,
    weight: parseFloat(document.getElementById('ped-weight').value) || 1.0,
    results_path: document.getElementById('ped-json-results').value.trim(),
    title_path: document.getElementById('ped-json-title').value.trim(),
    hash_path: document.getElementById('ped-json-hash').value.trim(),
    magnet_path: document.getElementById('ped-json-magnet').value.trim(),
    size_path: document.getElementById('ped-json-size').value.trim(),
    seeds_path: document.getElementById('ped-json-seeds').value.trim(),
    row_regex: document.getElementById('ped-html-row').value.trim(),
    title_regex: document.getElementById('ped-html-title').value.trim(),
    magnet_regex: document.getElementById('ped-html-magnet').value.trim(),
  };

  if (!activeConfig.search_providers) {
    activeConfig.search_providers = [];
  }

  if (editingProviderIndex >= 0) {
    activeConfig.search_providers[editingProviderIndex] = prov;
  } else {
    activeConfig.search_providers.push(prov);
  }

  renderProvidersList();
  closeProviderEditModal();
  showToast(`Provider "${name}" updated. Click "Save Preferences" to persist.`, "info", 2500);
}

function deleteProvider(idx) {
  if (!activeConfig || !activeConfig.search_providers) return;
  const name = activeConfig.search_providers[idx]?.name || 'provider';
  activeConfig.search_providers.splice(idx, 1);
  renderProvidersList();
  showToast(`Removed "${name}". Click "Save Preferences" to apply.`, "info", 2000);
}

async function resetProvidersToDefault() {
  try {
    const res = await fetch('/api/providers/reset', { method: 'POST' });
    const data = await res.json();
    if (data.providers) {
      activeConfig.search_providers = data.providers;
      renderProvidersList();
      showToast("Reset all search providers to default FrostWire/DHT sources.", "success", 2500);
    }
  } catch (err) {
    showToast("Failed to reset providers: " + err.message, "error", 3000);
  }
}

async function loadRawYAML() {
  const editor = document.getElementById('cfg-yaml-editor');
  editor.value = "Loading YAML from disk...";
  try {
    const res = await fetch('/api/config/yaml');
    editor.value = await res.text();
  } catch (err) {
    editor.value = "# Error loading YAML: " + err.message;
  }
}

async function saveSettings() {
  if (!activeConfig) return;

  const btn = document.getElementById('btn-save-settings');
  const origText = btn.textContent;
  btn.textContent = 'Saving...';
  btn.disabled = true;

  try {
    if (activeSettingsTab === 'yaml') {
      const yamlContent = document.getElementById('cfg-yaml-editor').value;
      const res = await fetch('/api/config/yaml', {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: yamlContent
      });
      const data = await res.json();
      if (data.error) {
        showToast(data.error, "error", 4000);
        return;
      }
      showToast("YAML configuration saved and reloaded successfully!", "success", 2500);
    } else {
      activeConfig.download_dir = document.getElementById('cfg-download-dir').value.trim();
      activeConfig.download_limit_kb = parseInt(document.getElementById('cfg-dl-limit').value, 10) || 0;
      activeConfig.upload_limit_kb = parseInt(document.getElementById('cfg-ul-limit').value, 10) || 0;
      activeConfig.enable_dht = document.getElementById('cfg-enable-dht').checked;
      activeConfig.enable_upnp = document.getElementById('cfg-enable-upnp').checked;

      const rawDNS = document.getElementById('cfg-fallback-dns').value.trim();
      if (rawDNS) {
        activeConfig.fallback_dns = rawDNS.split(',').map(s => s.trim()).filter(Boolean);
      }

      await fetch('/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(activeConfig)
      });
      showToast("Preferences and search engines saved!", "success", 2500);
    }
    closeSettingsModal();
  } catch (err) {
    showToast("Failed to save: " + err.message, "error", 3000);
  } finally {
    btn.textContent = origText;
    btn.disabled = false;
  }
}

async function triggerPreseedDHT() {
  showToast("Initiating background DHT pre-seeding from TorrentsCSV...", "info", 3500);
  try {
    const res = await fetch('/api/dht/preseed', { method: 'POST' });
    const data = await res.json();
    if (data.status) {
      showToast(`⚡ TorrentsCSV pre-seed started! Current local cache: ${data.current_size || 0} torrents.`, "success", 4000);
    }
  } catch (err) {
    showToast("Pre-seeding error: " + err.message, "error", 3000);
  }
}

// File & Folder Picker integration
let browserCurrentPath = '';
let browserParentPath = '';
let browserSelectedPath = '';
let browserPickerType = 'file'; // 'file' or 'folder'

async function openFilePicker(type = 'file') {
  browserPickerType = type;
  try {
    const res = await fetch(`/api/system/pick-path?type=${type}`);
    const data = await res.json();
    if (data.path) {
      document.getElementById('send-path-input').value = data.path;
      showToast("Selected: " + data.path, "info", 2000);
      return;
    }
    if (data.cancelled) {
      return; // User cancelled
    }
    openInAppFileBrowser(type);
  } catch (err) {
    openInAppFileBrowser(type);
  }
}

async function openInAppFileBrowser(type = 'file', initialPath = '') {
  browserPickerType = type;
  document.getElementById('browser-modal-title').textContent = type === 'folder' ? 'Choose Folder to Seed' : 'Choose File to Seed';
  document.getElementById('modal-file-browser').classList.add('open');
  await loadBrowserDir(initialPath);
}

function closeFileBrowser() {
  document.getElementById('modal-file-browser').classList.remove('open');
}

async function loadBrowserDir(dirPath = '') {
  const listEl = document.getElementById('browser-items-list');
  listEl.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 20px;">Loading directory...</div>';
  
  try {
    const res = await fetch(`/api/system/browse-dir?path=${encodeURIComponent(dirPath)}`);
    const data = await res.json();
    if (data.error) {
      listEl.innerHTML = `<div style="color: var(--adw-error); padding: 15px;">Error: ${data.error}</div>`;
      return;
    }

    browserCurrentPath = data.current;
    browserParentPath = data.parent;
    browserSelectedPath = browserPickerType === 'folder' ? data.current : '';

    document.getElementById('browser-current-path').value = data.current;
    document.getElementById('browser-selected-hint').textContent = browserSelectedPath ? `Selected: ${browserSelectedPath}` : 'Select an item below';

    if (!data.items || data.items.length === 0) {
      listEl.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 20px;">(Empty folder)</div>';
      return;
    }

    // Sort folders first, then alphabetical
    data.items.sort((a, b) => {
      if (a.is_dir && !b.is_dir) return -1;
      if (!a.is_dir && b.is_dir) return 1;
      return a.name.localeCompare(b.name);
    });

    listEl.innerHTML = data.items.map(item => `
      <div class="browser-item" onclick="onBrowserItemClick('${encodeURIComponent(item.path)}', ${item.is_dir}, this)" ondblclick="onBrowserItemDblClick('${encodeURIComponent(item.path)}', ${item.is_dir})" style="display: flex; align-items: center; justify-content: space-between; padding: 6px 10px; border-radius: 6px; cursor: pointer; user-select: none;">
        <div style="display: flex; align-items: center; gap: 8px;">
          ${item.is_dir ? `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="color: var(--adw-accent);"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>` : `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="color: var(--adw-dim-label);"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>`}
          <span style="font-size: 12.5px; font-weight: ${item.is_dir ? '600' : 'normal'};">${item.name}</span>
        </div>
        ${!item.is_dir ? `<span style="font-size: 11px; color: var(--adw-dim-label);">${formatBytes(item.size)}</span>` : ''}
      </div>
    `).join('');
  } catch (err) {
    listEl.innerHTML = `<div style="color: var(--adw-error); padding: 15px;">Failed to load: ${err.message}</div>`;
  }
}

function onBrowserItemClick(encodedPath, isDir, element) {
  const path = decodeURIComponent(encodedPath);
  browserSelectedPath = path;
  document.querySelectorAll('.browser-item').forEach(el => el.style.background = 'transparent');
  if (element) {
    element.style.background = 'rgba(53, 132, 228, 0.25)';
  }
  document.getElementById('browser-selected-hint').textContent = `Selected: ${path}`;
}

function onBrowserItemDblClick(encodedPath, isDir) {
  const path = decodeURIComponent(encodedPath);
  if (isDir) {
    loadBrowserDir(path);
  } else {
    browserSelectedPath = path;
    confirmFileBrowserSelection();
  }
}

function browserNavigateParent() {
  if (browserParentPath && browserParentPath !== browserCurrentPath) {
    loadBrowserDir(browserParentPath);
  }
}

function confirmFileBrowserSelection() {
  const selected = browserSelectedPath || browserCurrentPath;
  if (selected) {
    document.getElementById('send-path-input').value = selected;
    showToast("Selected: " + selected, "info", 2000);
    closeFileBrowser();
  }
}

// Native Window Actions & State Management
function sendNativeWindowAction(action) {
  try {
    if (window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.windowAction) {
      window.webkit.messageHandlers.windowAction.postMessage(action);
      return true;
    }
  } catch (e) {}
  return false;
}

let isWindowMaximized = false;

function windowMinimize() {
  if (!sendNativeWindowAction('minimize')) {
    showToast("Minimize is handled by your system window manager (Super+H / Alt+F9)", "info", 2000);
  }
}

function windowToggleMaximize() {
  const btn = document.getElementById('btn-window-maximize');
  if (sendNativeWindowAction('toggle_maximize')) {
    isWindowMaximized = !isWindowMaximized;
    if (btn) {
      btn.innerHTML = isWindowMaximized ?
        `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"></path></svg>` :
        `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect></svg>`;
    }
    return;
  }

  // Fallback to HTML5 Fullscreen
  if (!document.fullscreenElement) {
    document.documentElement.requestFullscreen().catch(() => {});
    if (btn) {
      btn.innerHTML = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"></path></svg>`;
    }
  } else {
    document.exitFullscreen().catch(() => {});
    if (btn) {
      btn.innerHTML = `<svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect></svg>`;
    }
  }
}

function windowClose() {
  if (!sendNativeWindowAction('close')) {
    window.close();
  }
}

function windowHeaderDblClick(e) {
  if (e.target.closest('button, input, select, a, .view-btn, .window-control-btn')) return;
  windowToggleMaximize();
}

window.addEventListener('keydown', (e) => {
  if (e.key === 'F11') {
    e.preventDefault();
    windowToggleMaximize();
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'q') {
    e.preventDefault();
    windowClose();
  }
});

// Initialize on page load
window.addEventListener('DOMContentLoaded', () => {
  initEventStream();
});

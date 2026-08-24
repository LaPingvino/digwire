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
  verify: `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="23 4 23 10 17 10"></polyline><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path></svg>`,
  copy: `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>`,
  download: `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 2v14m0 0l-4-4m4 4l4-4M4 20h16"></path></svg>`
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
  document.getElementById('stat-dht-nodes').textContent = `DHT: ${stats.dht_nodes} nodes`;
}

// Torrent Rendering
function renderTorrents() {
  const container = document.getElementById('torrent-list-container');
  const emptyState = document.getElementById('torrents-empty');

  let filtered = torrentsData;
  if (currentFilter === 'downloading') {
    filtered = torrentsData.filter(t => t.state === 'downloading' || t.state === 'metadata');
  } else if (currentFilter === 'completed') {
    filtered = torrentsData.filter(t => t.state === 'seeding' || t.state === 'completed' || t.progress >= 100);
  }

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
      metaString += ` • <span style="color: #57e389; font-weight: 600;">🌐 ${t.webseeds.length} WebSeed${t.webseeds.length > 1 ? 's' : ''}</span>`;
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
            🔄 Force Recheck & Verify Pieces
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

    controls.style.display = 'flex';
    renderSearchResults();
  } catch (err) {
    spinner.style.display = 'none';
    alert("Search failed: " + err.message);
  }
}

function setSourceFilter(source) {
  currentSourceFilter = source;
  const chips = document.querySelectorAll('#source-filter-chips .source-chip');
  chips.forEach(c => {
    const isThis = (source === 'all' && c.textContent.includes('All')) ||
                   (source !== 'all' && c.getAttribute('onclick').includes(`'${source}'`));
    c.classList.toggle('active', isThis);
  });
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
      return (b.seeders || 0) - (a.seeders || 0);
    } else if (currentSortBy === 'size') {
      return (b.size_bytes || 0) - (a.size_bytes || 0);
    }
    return 0;
  });

  container.innerHTML = sorted.map(r => {
    const tagClass = `tag-${r.provider_type || 'torrentscsv'}`;
    const scoreText = r.score > 0 ? `<span class="score-badge">Relevance: ${r.score.toFixed(0)}</span>` : '';

    return `
      <div class="search-card">
        <div class="search-info">
          <div class="search-title" title="${r.title}">${r.title}</div>
          <div class="search-sub">
            <span>📦 ${formatBytes(r.size_bytes)}</span>
            <span style="color: var(--adw-success); font-weight: 600;">▲ ${r.seeders} seeds</span>
            <span>▼ ${r.leechers} peers</span>
            <span class="provider-badge ${tagClass}">${r.provider}</span>
            ${scoreText}
          </div>
        </div>
        <div style="display: flex; gap: 6px;">
          <button class="btn btn-icon" title="Copy Magnet" onclick="copyToClipboard('${encodeURI(r.magnet_uri)}', this)">${ICONS.magnet}</button>
          <button class="btn btn-primary" onclick="downloadFromSearch('${encodeURIComponent(r.magnet_uri)}')">
            ${ICONS.download}
            <span>Download</span>
          </button>
        </div>
      </div>
    `;
  }).join('');
}

async function downloadFromSearch(encodedURI) {
  const uri = decodeURIComponent(encodedURI);
  try {
    const res = await fetch('/api/torrents/add', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: uri })
    });
    const data = await res.json();
    if (data.status === 'ok') {
      switchMainView('torrents');
    } else {
      alert("Failed to add download: " + (data.error || 'Unknown error'));
    }
  } catch (err) {
    alert("Download request failed: " + err.message);
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
    const formData = new FormData();
    formData.append('torrent_file', fileInput.files[0]);
    const res = await fetch('/api/torrents/add', { method: 'POST', body: formData });
    const data = await res.json();
    if (data.status === 'ok') {
      closeAddModal();
      switchMainView('torrents');
    } else {
      alert("Failed: " + (data.error || 'Unknown error'));
    }
    return;
  }

  if (inputVal) {
    const res = await fetch('/api/torrents/add', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: inputVal })
    });
    const data = await res.json();
    if (data.status === 'ok') {
      closeAddModal();
      switchMainView('torrents');
    } else {
      alert("Failed: " + (data.error || 'Unknown error'));
    }
    return;
  }

  alert("Please enter a magnet link, HTTP direct URL, infohash, or choose a .torrent file.");
}

// Settings Modal
async function openSettingsModal() {
  const res = await fetch('/api/config');
  activeConfig = await res.json();

  document.getElementById('cfg-download-dir').value = activeConfig.download_dir;
  const provList = document.getElementById('cfg-providers-list');
  
  provList.innerHTML = activeConfig.search_providers.map((p, idx) => `
    <div style="display: flex; align-items: center; justify-content: space-between; background: rgba(0,0,0,0.15); padding: 10px 14px; border-radius: 8px; gap: 10px;">
      <div style="flex: 1;">
        <div style="font-size: 13px; font-weight: 600;">${p.name}</div>
        <div style="font-size: 11px; color: var(--adw-dim-label);">${p.url || p.type}</div>
      </div>
      <div style="display: flex; align-items: center; gap: 12px;">
        <label style="font-size: 11px; color: var(--adw-dim-label); display: flex; align-items: center; gap: 4px;">
          Bias:
          <select id="cfg-prov-weight-${idx}" class="sort-select" style="padding: 2px 6px;">
            <option value="1.5" ${p.weight >= 1.4 ? 'selected' : ''}>High (1.5x)</option>
            <option value="1.0" ${p.weight > 0.7 && p.weight < 1.4 ? 'selected' : ''}>Normal (1.0x)</option>
            <option value="0.5" ${p.weight <= 0.7 ? 'selected' : ''}>Low (0.5x)</option>
          </select>
        </label>
        <label style="display: flex; align-items: center; gap: 6px; font-size: 12px; cursor: pointer;">
          <input type="checkbox" id="cfg-prov-enabled-${idx}" ${p.enabled ? 'checked' : ''}> Enabled
        </label>
      </div>
    </div>
  `).join('');

  document.getElementById('modal-settings').classList.add('open');
}

function closeSettingsModal() {
  document.getElementById('modal-settings').classList.remove('open');
}

async function saveSettings() {
  if (!activeConfig) return;

  activeConfig.download_dir = document.getElementById('cfg-download-dir').value.trim();
  activeConfig.search_providers.forEach((p, idx) => {
    const cb = document.getElementById(`cfg-prov-enabled-${idx}`);
    if (cb) p.enabled = cb.checked;
    const wt = document.getElementById(`cfg-prov-weight-${idx}`);
    if (wt) p.weight = parseFloat(wt.value) || 1.0;
  });

  await fetch('/api/config', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(activeConfig)
  });

  closeSettingsModal();
}

// Initialize on page load
window.addEventListener('DOMContentLoaded', () => {
  initEventStream();
});

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
let lastActiveFocusElement = null;

// Inspect & Selective Download state
let currentInspectData = null;
let currentInspectFiles = [];
let inspectSelectedIndices = new Set();
let inspectCurrentFilter = '';

// Accessibility: Screen reader live announcement helper
function announceA11y(message) {
  if (!message) return;
  const el = document.getElementById('a11y-announcer');
  if (el) {
    el.textContent = '';
    setTimeout(() => {
      el.textContent = message;
    }, 50);
  }
}

// Accessibility: Safe focus management for modals
function saveFocusAndOpen(modalId, defaultFocusSelector) {
  lastActiveFocusElement = document.activeElement;
  const modal = document.getElementById(modalId);
  if (modal) {
    modal.classList.add('open');
    setTimeout(() => {
      const focusTarget = (defaultFocusSelector ? modal.querySelector(defaultFocusSelector) : null) || 
                          modal.querySelector('input:not([type=hidden]), button:not(.btn-icon), select, textarea, [tabindex="0"]');
      if (focusTarget) focusTarget.focus();
    }, 80);
    const title = modal.querySelector('.modal-title');
    if (title) announceA11y(title.textContent + " dialog opened");
  }
}

function restoreFocusAndClose(modalId) {
  const modal = document.getElementById(modalId);
  if (modal) {
    modal.classList.remove('open');
  }
  if (lastActiveFocusElement && typeof lastActiveFocusElement.focus === 'function') {
    lastActiveFocusElement.focus();
  }
  announceA11y("Dialog closed");
}

// Colorful Native Emojis (Rendered crisply via embedded Noto Color Emoji font)
const ICONS = {
  magnet: '🧲',
  info: 'ℹ️',
  play: '▶️',
  pause: '⏸️',
  trash: '🗑️',
  verify: '🔄',
  copy: '📋',
  download: '📥',
  globe: '🌐',
  package: '📦',
  arrowUp: '⬆️',
  arrowDown: '⬇️',
  zap: '⚡',
  folder: '📂',
  external: '🔗',
  clock: '⏱️',
  search: '🔍',
  check: '✔️',
  x: '✖️',
  edit: '✏️',
  lightbulb: '💡',
  alert: '⚠️',
  dot: '•',

  // Swarm Archetype icons
  rocket: '🚀',
  disc: '💿',
  compass: '🧭',
  archive: '🏛️',
  ghost: '👻',

  // File Types
  file: '📄',
  fileVideo: '🎬',
  fileAudio: '🎵',
  fileArchive: '📦',
  fileImage: '🖼️',
  fileCode: '💻'
};

function getQualifierBadge(q) {
  if (!q) return '';
  let emoji = '⚡';
  switch (q.class) {
    case 'blockbuster': emoji = '🚀'; break;
    case 'mainstream': emoji = '⚡'; break;
    case 'cult': emoji = '💿'; break;
    case 'long_tail': emoji = '🧭'; break;
    case 'deep_archive': emoji = '🏛️'; break;
    case 'ghost_ship': emoji = '👻'; break;
    case 'discovering': emoji = '🔍'; break;
    default: emoji = '⚡'; break;
  }
  const title = escapeHtml(q.description + (q.easter_egg ? '\n\n' + q.easter_egg : ''));
  return `<span class="torrent-badge badge-qualifier badge-qualifier-${q.class}" title="${title}">
    <span class="badge-icon" style="margin-right: 3px;">${emoji}</span><span>${escapeHtml(q.label || q.badge)}</span>
  </span>`;
}

function getPlatformBadge(p) {
  if (!p) return '';
  p = p.toLowerCase();
  if (p === 'folder') {
    return `<span class="torrent-badge badge-platform badge-folder" style="background: rgba(53, 132, 228, 0.2); color: #3584e4; border: 1px solid rgba(53, 132, 228, 0.4);" title="Unified Folder-in-Folder Swarm">📁 Folder Swarm</span>`;
  }
  return `<span class="torrent-badge badge-platform badge-${p}">${escapeHtml(p)}</span>`;
}

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
      btnElement.innerHTML = `${ICONS.check} Copied!`;
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
  const tabTorrents = document.getElementById('tab-torrents');
  const tabSearch = document.getElementById('tab-search');
  
  if (tabTorrents) {
    tabTorrents.classList.toggle('active', view === 'torrents');
    tabTorrents.setAttribute('aria-selected', view === 'torrents' ? 'true' : 'false');
  }
  if (tabSearch) {
    tabSearch.classList.toggle('active', view === 'search');
    tabSearch.setAttribute('aria-selected', view === 'search' ? 'true' : 'false');
  }
  
  const viewTorrents = document.getElementById('view-torrents');
  const viewSearch = document.getElementById('view-search');
  const torrentFilters = document.getElementById('torrent-filters');
  
  if (viewTorrents) viewTorrents.style.display = view === 'torrents' ? 'block' : 'none';
  if (viewSearch) viewSearch.style.display = view === 'search' ? 'block' : 'none';
  if (torrentFilters) torrentFilters.style.display = view === 'torrents' ? 'flex' : 'none';
  
  announceA11y(`Switched to ${view === 'torrents' ? 'Torrents transfer' : 'Indexer Search'} view`);
  if (view === 'search') {
    const sInput = document.getElementById('search-input');
    if (sInput) sInput.focus();
  }
}

function setFilter(filter) {
  currentFilter = filter;
  const filterBtns = document.getElementById('torrent-filters').querySelectorAll('.view-btn');
  filterBtns.forEach(btn => {
    const isActive = btn.textContent.toLowerCase().includes(filter);
    btn.classList.toggle('active', isActive);
  });
  renderTorrents();
  announceA11y(`Showing ${filter} transfers`);
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
  document.getElementById('stat-download-rate').innerHTML = `${ICONS.arrowDown} ${formatSpeed(stats.download_rate)}`;
  document.getElementById('stat-upload-rate').innerHTML = `${ICONS.arrowUp} ${formatSpeed(stats.upload_rate)}`;
  document.getElementById('stat-active-count').textContent = `${stats.active_count} active / ${stats.total_count} total`;
  let dhtText = `DHT: ${stats.dht_nodes} nodes`;
  if (stats.dht_indexed_count !== undefined && stats.dht_indexed_count > 0) {
    dhtText += ` • ${stats.dht_indexed_count} indexed`;
  }
  document.getElementById('stat-dht-nodes').textContent = dhtText;

  const germanyBadge = document.getElementById('germany-mode-badge');
  if (germanyBadge) {
    germanyBadge.style.display = stats.germany_mode ? 'inline-flex' : 'none';
  }
}

// Torrent Keyboard Navigation
function handleTorrentCardKeydown(e, hash, name) {
  const card = e.currentTarget;
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    const next = card.nextElementSibling;
    if (next && next.classList.contains('torrent-card')) {
      next.focus();
    }
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    const prev = card.previousElementSibling;
    if (prev && prev.classList.contains('torrent-card')) {
      prev.focus();
    }
  } else if (e.key === 'Home') {
    e.preventDefault();
    const first = card.parentElement ? card.parentElement.querySelector('.torrent-card') : null;
    if (first) first.focus();
  } else if (e.key === 'End') {
    e.preventDefault();
    const cards = card.parentElement ? card.parentElement.querySelectorAll('.torrent-card') : [];
    if (cards.length > 0) cards[cards.length - 1].focus();
  } else if (e.key === 'Enter' || e.key === ' ') {
    if (e.target === card) {
      e.preventDefault();
      openDetailsModal(hash);
    }
  } else if (e.key === 'Delete') {
    if (e.target === card) {
      e.preventDefault();
      promptDeleteTorrent(hash, name);
    }
  }
}

function getTorrentMetaString(t) {
  const isPaused = t.state === 'paused';
  const isSeeding = t.state === 'seeding' || t.state === 'completed';
  const isMeta = t.state === 'metadata';
  const isInspecting = t.state === 'inspecting';
  const isProcessing = t.state === 'processing';
  const isCreatingSwarm = t.state === 'creating_swarm';
  const isVerifying = t.is_verifying || t.state === 'verifying' || (t.verify_progress !== undefined && t.verify_progress > 0);
  const isWebDownload = t.magnet_uri && (t.magnet_uri.startsWith('http://') || t.magnet_uri.startsWith('https://'));

  let metaString = '';
  if (t.total_bytes > 0 && t.completed_bytes > 0) {
    metaString = `${formatBytes(t.completed_bytes)} of ${formatBytes(t.total_bytes)} (${t.progress.toFixed(1)}%)`;
  } else if (t.completed_bytes > 0) {
    metaString = `${formatBytes(t.completed_bytes)} (${t.progress.toFixed(1)}%)`;
  } else if (t.total_bytes > 0) {
    metaString = `${formatBytes(t.total_bytes)} (${t.progress.toFixed(1)}%)`;
  } else if (t.progress > 0) {
    metaString = `${t.progress.toFixed(1)}%`;
  } else {
    metaString = '0.0%';
  }

  if (isInspecting) {
    metaString = `<span style="color: #62a0ea; font-weight: 500;">${ICONS.search || ''}Inspecting stream / document metadata...</span>`;
  } else if (isProcessing) {
    metaString = `<span style="color: #e5a50a; font-weight: 500;">${ICONS.clock || ''}Processing / merging media container...</span>`;
  } else if (isCreatingSwarm) {
    metaString = `<span style="color: #33d17a; font-weight: 500;">${ICONS.zap || ''}Packaging BitTorrent swarm for DHT...</span>`;
  } else if (isMeta) {
    metaString = 'Downloading metadata from peers...';
  } else {
    if (isVerifying) {
      const vProg = (t.verify_progress !== undefined && t.verify_progress > 0) ? ` (${t.verify_progress.toFixed(0)}% checked)` : '';
      metaString += ` • <span style="color: #c061cb; font-weight: 500;">${ICONS.verify || ''}Verifying integrity${vProg}</span>`;
    }
    if (t.download_rate > 0) {
      metaString += ` • ${ICONS.arrowDown}${formatSpeed(t.download_rate)}`;
      const eta = formatETA(t.eta_seconds);
      if (eta) metaString += ` • ETA: ${eta}`;
    } else if (!isSeeding && !isPaused && t.progress < 100 && t.availability_eta) {
      const qTooltip = t.qualifier ? 
        (t.qualifier.description + (t.qualifier.easter_egg ? '\n\n' + t.qualifier.easter_egg : '')) : 
        'Projected completion based on seeder duty cycle';
      metaString += ` • <span style="color: #62a0ea; font-weight: 500;" title="${escapeHtml(qTooltip)}">${ICONS.clock}Proj. ETA: ${escapeHtml(t.availability_eta)}</span>`;
    }
    if (t.upload_rate > 0) {
      metaString += ` • ${ICONS.arrowUp}${formatSpeed(t.upload_rate)}`;
    }
    if (t.is_media || t.platform) {
      if (isSeeding) {
        metaString += ` • Seeding swarm`;
      } else if (t.download_rate > 0) {
        metaString += ` • Streaming`;
      }
    } else if (isWebDownload) {
      const mirrorCount = t.webseeds && t.webseeds.length > 0 ? t.webseeds.length : (t.peers || 1);
      metaString += ` • ${mirrorCount} mirror${mirrorCount !== 1 ? 's' : ''}`;
    } else {
      const seeds = t.seeders || 0;
      const leechers = t.leechers !== undefined ? t.leechers : Math.max(0, (t.peers || 0) - seeds);
      if (isSeeding) {
        metaString += ` • ${leechers} peer${leechers !== 1 ? 's' : ''}`;
      } else {
        metaString += ` • ${seeds} seed${seeds !== 1 ? 's' : ''}, ${leechers} peer${leechers !== 1 ? 's' : ''}`;
      }
      if (t.webseeds && t.webseeds.length > 0) {
        metaString += ` • <span style="color: #57e389; font-weight: 600;">${ICONS.globe}${t.webseeds.length} WebSeed${t.webseeds.length > 1 ? 's' : ''}</span>`;
      }
    }
  }
  return metaString;
}

function getCardActionsHtml(t) {
  const isPaused = t.state === 'paused';
  const isWebDownload = t.magnet_uri && (t.magnet_uri.startsWith('http://') || t.magnet_uri.startsWith('https://'));
  return `
    ${(t.progress >= 100 || t.state === 'seeding' || t.state === 'completed') ? 
      `<button class="btn btn-icon" title="Open Downloaded File or Folder" aria-label="Open downloaded file for ${escapeHtml(t.name)}" onclick="openTorrentTarget('${t.info_hash}')">${ICONS.play}</button>` : ''
    }
    <button class="btn btn-icon" title="Show in File Manager" aria-label="Show ${escapeHtml(t.name)} in file manager" onclick="showTorrentInFolder('${t.info_hash}')">${ICONS.folder}</button>
    ${!isWebDownload ? 
      `<button class="btn btn-icon" title="Verify Local Data (Recheck)" aria-label="Verify local data for ${escapeHtml(t.name)}" onclick="verifyTorrent('${t.info_hash}', this)">${ICONS.verify}</button>` : ''
    }
    <button class="btn btn-icon" title="Copy Magnet / URL" aria-label="Copy Magnet link for ${escapeHtml(t.name)}" onclick="copyToClipboard('${encodeURI(t.magnet_uri || '')}', this)">${ICONS.magnet}</button>
    <button class="btn btn-icon" title="Inspect Details & Peers" aria-label="Inspect details and peers for ${escapeHtml(t.name)}" onclick="openDetailsModal('${t.info_hash}')">${ICONS.info}</button>
    ${isPaused ? 
      `<button class="btn btn-icon" title="Resume" aria-label="Resume download for ${escapeHtml(t.name)}" onclick="resumeTorrent('${t.info_hash}')">${ICONS.play}</button>` :
      `<button class="btn btn-icon" title="Pause" aria-label="Pause download for ${escapeHtml(t.name)}" onclick="pauseTorrent('${t.info_hash}')">${ICONS.pause}</button>`
    }
    <button class="btn btn-icon" style="color: var(--adw-error);" title="Delete" aria-label="Delete ${escapeHtml(t.name)}" onclick="promptDeleteTorrent('${t.info_hash}', '${t.name.replace(/'/g, "\\'")}')">${ICONS.trash}</button>
  `;
}

function createTorrentCardElement(t) {
  const isSeeding = t.state === 'seeding' || t.state === 'completed';
  const metaString = getTorrentMetaString(t);
  const cardAriaLabel = `Torrent: ${escapeHtml(t.name)}, state: ${t.state}, ${t.progress.toFixed(1)} percent, size: ${formatBytes(t.total_bytes)}`;

  let swarmBanner = '';
  if (t.suggested_swarm) {
    const matchText = t.suggested_swarm.is_partial ?
      `${ICONS.zap}<strong>Partial Match in Pack!</strong> "${escapeHtml(t.suggested_swarm.name)}" (${t.suggested_swarm.seeders} seeds). Upgrade to swarm?` :
      `${ICONS.zap}<strong>Equivalent Swarm Found!</strong> Verified with ${t.suggested_swarm.seeders} seeds. Upgrade to hybrid swarm?`;
    swarmBanner = `
      <div class="swarm-suggestion-banner">
        <div>${matchText}</div>
        <button class="btn btn-primary" style="padding: 3px 10px; font-size: 11px; white-space: nowrap;" onclick="upgradeToSwarm('${t.info_hash}')">
          Upgrade to Swarm
        </button>
      </div>
    `;
  }

  const div = document.createElement('div');
  div.className = 'torrent-card';
  div.dataset.hash = t.info_hash;
  div.tabIndex = 0;
  div.setAttribute('role', 'region');
  div.setAttribute('aria-label', cardAriaLabel);
  div.title = "Double-click or press Enter to inspect";
  div.ondblclick = () => openTorrentTarget(t.info_hash);
  div.onkeydown = (e) => handleTorrentCardKeydown(e, t.info_hash, t.name);

  const platformBadge = getPlatformBadge(t.platform);
  const thumbHtml = t.thumbnail ? `<img src="${escapeHtml(t.thumbnail)}" class="card-thumbnail" alt="" onerror="this.style.display='none'">` : '';

  div.innerHTML = `
    <div class="card-header" style="display: flex; gap: 10px; align-items: center;">
      ${thumbHtml}
      <div style="flex: 1; min-width: 0;">
        <div class="torrent-title" title="${escapeHtml(t.name)}" style="cursor: pointer;" onclick="openDetailsModal('${t.info_hash}')">${escapeHtml(t.name)}</div>
      </div>
      <div style="display: flex; gap: 6px; align-items: center; flex-shrink: 0;">
        ${platformBadge}
        ${getQualifierBadge(t.qualifier)}
        <span class="torrent-badge badge-${t.state}" aria-label="Status: ${t.state}">${t.state}</span>
      </div>
    </div>

    <div class="progress-bar-container" role="progressbar" aria-valuenow="${t.progress.toFixed(1)}" aria-valuemin="0" aria-valuemax="100" aria-valuetext="${t.progress.toFixed(1)} percent complete" style="cursor: pointer;" onclick="openDetailsModal('${t.info_hash}')" title="Progress: ${t.progress.toFixed(1)}%">
      <div class="progress-bar-fill ${isSeeding ? 'seeding' : ''}" style="width: ${Math.min(100, Math.max(0, t.progress))}%;"></div>
    </div>

    ${swarmBanner}

    <div class="card-footer">
      <div class="torrent-meta">${metaString}</div>
      <div class="card-actions">
        ${getCardActionsHtml(t)}
      </div>
    </div>
  `;
  return div;
}

function updateTorrentCardElement(cardEl, t) {
  const isSeeding = t.state === 'seeding' || t.state === 'completed';
  const metaString = getTorrentMetaString(t);
  const cardAriaLabel = `Torrent: ${escapeHtml(t.name)}, state: ${t.state}, ${t.progress.toFixed(1)} percent, size: ${formatBytes(t.total_bytes)}`;

  if (cardEl.getAttribute('aria-label') !== cardAriaLabel) {
    cardEl.setAttribute('aria-label', cardAriaLabel);
  }

  // Update title
  const titleEl = cardEl.querySelector('.torrent-title');
  if (titleEl && titleEl.textContent !== t.name) {
    titleEl.textContent = t.name;
    titleEl.title = t.name;
  }

  // Update badges
  const badgeContainer = cardEl.querySelector('.card-header > div:last-child');
  const platformBadge = getPlatformBadge(t.platform);
  const qualifierHtml = getQualifierBadge(t.qualifier);
  const stateBadgeHtml = `<span class="torrent-badge badge-${t.state}" aria-label="Status: ${t.state}">${t.state}</span>`;
  const fullBadgeHtml = platformBadge + qualifierHtml + stateBadgeHtml;
  if (badgeContainer && badgeContainer.innerHTML !== fullBadgeHtml) {
    badgeContainer.innerHTML = fullBadgeHtml;
  }

  // Update progress bar
  const progressContainer = cardEl.querySelector('.progress-bar-container');
  if (progressContainer) {
    progressContainer.setAttribute('aria-valuenow', t.progress.toFixed(1));
    progressContainer.setAttribute('aria-valuetext', `${t.progress.toFixed(1)} percent complete`);
    progressContainer.title = `Progress: ${t.progress.toFixed(1)}%`;
  }
  const progressFill = cardEl.querySelector('.progress-bar-fill');
  if (progressFill) {
    progressFill.style.width = `${Math.min(100, Math.max(0, t.progress))}%`;
    progressFill.className = `progress-bar-fill ${isSeeding ? 'seeding' : ''}`;
  }

  // Update swarm banner
  let bannerEl = cardEl.querySelector('.swarm-suggestion-banner');
  if (t.suggested_swarm) {
    const matchText = t.suggested_swarm.is_partial ?
      `${ICONS.zap}<strong>Partial Match in Pack!</strong> "${escapeHtml(t.suggested_swarm.name)}" (${t.suggested_swarm.seeders} seeds). Upgrade to swarm?` :
      `${ICONS.zap}<strong>Equivalent Swarm Found!</strong> Verified with ${t.suggested_swarm.seeders} seeds. Upgrade to hybrid swarm?`;
    const newBannerHtml = `
      <div>${matchText}</div>
      <button class="btn btn-primary" style="padding: 3px 10px; font-size: 11px; white-space: nowrap;" onclick="upgradeToSwarm('${t.info_hash}')">
        Upgrade to Swarm
      </button>
    `;
    if (!bannerEl) {
      const div = document.createElement('div');
      div.className = 'swarm-suggestion-banner';
      div.innerHTML = newBannerHtml;
      const pBar = cardEl.querySelector('.progress-bar-container');
      if (pBar) pBar.after(div);
    } else if (bannerEl.innerHTML !== newBannerHtml) {
      bannerEl.innerHTML = newBannerHtml;
    }
  } else if (bannerEl) {
    bannerEl.remove();
  }

  // Update meta string
  const metaEl = cardEl.querySelector('.torrent-meta');
  if (metaEl && metaEl.innerHTML !== metaString) {
    metaEl.innerHTML = metaString;
  }

  // Update card actions
  const actionsEl = cardEl.querySelector('.card-actions');
  const newActionsHtml = getCardActionsHtml(t);
  if (actionsEl && actionsEl.innerHTML.replace(/\s+/g, ' ') !== newActionsHtml.replace(/\s+/g, ' ')) {
    const activeEl = document.activeElement;
    let focusedBtnIdx = -1;
    if (activeEl && actionsEl.contains(activeEl)) {
      const btns = Array.from(actionsEl.querySelectorAll('button'));
      focusedBtnIdx = btns.indexOf(activeEl);
    }
    actionsEl.innerHTML = newActionsHtml;
    if (focusedBtnIdx >= 0) {
      const newBtns = Array.from(actionsEl.querySelectorAll('button'));
      const targetBtn = newBtns[focusedBtnIdx] || newBtns[0];
      if (targetBtn) targetBtn.focus();
    }
  }
}

// Torrent Rendering with Keyed In-Place DOM Diffing & Focus Persistence
function renderTorrents() {
  const container = document.getElementById('torrent-list-container');
  const emptyState = document.getElementById('torrents-empty');
  if (!container || !emptyState) return;

  let filtered = [...torrentsData];
  if (currentFilter === 'downloading') {
    filtered = filtered.filter(t => 
      t.state === 'downloading' || 
      t.state === 'metadata' || 
      t.state === 'inspecting' || 
      t.state === 'processing' || 
      t.state === 'creating_swarm'
    );
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

  // Save current active element inside container if any
  let focusedCardHash = null;
  let focusedSubIndex = -1;
  const activeEl = document.activeElement;
  if (activeEl && container.contains(activeEl)) {
    const card = activeEl.closest('.torrent-card');
    if (card && card.dataset.hash) {
      focusedCardHash = card.dataset.hash;
      if (activeEl !== card) {
        const btns = Array.from(card.querySelectorAll('button'));
        focusedSubIndex = btns.indexOf(activeEl);
      }
    }
  }

  // Keyed DOM reconciliation
  const existingMap = new Map();
  Array.from(container.children).forEach(child => {
    if (child.dataset && child.dataset.hash) {
      existingMap.set(child.dataset.hash, child);
    }
  });

  filtered.forEach((t, index) => {
    let cardEl = existingMap.get(t.info_hash);
    if (cardEl) {
      updateTorrentCardElement(cardEl, t);
      existingMap.delete(t.info_hash);
    } else {
      cardEl = createTorrentCardElement(t);
    }

    const currentAtIndex = container.children[index];
    if (currentAtIndex !== cardEl) {
      container.insertBefore(cardEl, currentAtIndex || null);
    }
  });

  // Remove leftover cards
  existingMap.forEach(cardEl => {
    cardEl.remove();
  });

  // Restore focus if it was lost during reconciliation
  if (focusedCardHash) {
    const currentActive = document.activeElement;
    if (!currentActive || !container.contains(currentActive) || currentActive === document.body) {
      const targetCard = container.querySelector(`[data-hash="${focusedCardHash}"]`);
      if (targetCard) {
        if (focusedSubIndex === -1) {
          targetCard.focus();
        } else {
          const btns = Array.from(targetCard.querySelectorAll('button'));
          const btn = btns[focusedSubIndex] || targetCard;
          btn.focus();
        }
      }
    }
  }
}

async function verifyTorrent(hash, btn) {
  showToast("Rechecking and verifying local piece hashes...", "info", 3000);
  if (btn) btn.disabled = true;
  try {
    const res = await fetch(`/api/torrents/${hash}/verify`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("Hash verification triggered! Rechecking data...", "accent", 3500);
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

  // Announce to screen readers (strip HTML tags)
  const plainText = String(message).replace(/<[^>]*>/g, '').trim();
  announceA11y(plainText);

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
    btn.innerHTML = ICONS.search;
  }

  showToast("Scanning indexers & official host mirrors in background...", "info", 3000);

  try {
    const res = await fetch(`/api/torrents/${hash}/find-swarm`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      if (btn) btn.innerHTML = ICONS.check;
      showToast("Equivalent BitTorrent swarm verified! Click 'Upgrade to Swarm' on card.", "accent", 5000);
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
      showToast("Upgraded to hybrid P2P swarm with WebSeed acceleration!", "accent", 4000);
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

async function setFolderFilesPriority(hash, dirPath, priority) {
  if (!currentDetailData || !currentDetailData.files) return;
  const filesToUpdate = currentDetailData.files.filter(f => {
    const rawPath = f.path || '';
    const lastSlash = rawPath.lastIndexOf('/');
    const lastBackslash = rawPath.lastIndexOf('\\');
    const splitIdx = Math.max(lastSlash, lastBackslash);
    const dir = splitIdx !== -1 ? rawPath.substring(0, splitIdx) : '';
    return dir === dirPath;
  });
  for (const f of filesToUpdate) {
    const idx = f.index !== undefined ? f.index : 0;
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
  saveFocusAndOpen('modal-delete');
}

function closeDeleteModal() {
  restoreFocusAndClose('modal-delete');
  pendingDeleteHash = null;
}

async function confirmDelete(deleteFiles) {
  if (!pendingDeleteHash) return;
  const hash = pendingDeleteHash;
  closeDeleteModal();
  try {
    const res = await fetch(`/api/torrents/${encodeURIComponent(hash)}?delete_files=${deleteFiles}`, { method: 'DELETE' });
    let data;
    try {
      data = await res.json();
    } catch {
      data = {};
    }
    if (res.ok && data.status === 'ok') {
      showToast("Transfer removed.", "info", 2500);
      torrentsData = torrentsData.filter(t => t.info_hash !== hash && t.magnet_uri !== hash && (!t.id || t.id !== hash));
      renderTorrents();
      setTimeout(fetchTorrents, 300);
    } else {
      showToast("Delete failed: " + (data.error || `HTTP ${res.status}`), "error", 4000);
    }
  } catch (err) {
    showToast("Error deleting: " + err.message, "error", 4000);
  }
}

async function openDownloadFolder() {
  await fetch('/api/open-folder', { method: 'POST' });
}

async function openTorrentTarget(hash) {
  try {
    const res = await fetch(`/api/torrents/${hash}/open`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      const name = data.path ? data.path.split('/').pop() : 'item';
      showToast(`Opening ${escapeHtml(name)}...`, "info", 2500);
    } else {
      showToast("Could not open: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error opening: " + err.message, "error", 4000);
  }
}

async function showTorrentInFolder(hash) {
  try {
    const res = await fetch(`/api/torrents/${hash}/show-in-folder`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("Revealed in File Manager", "info", 2000);
    } else {
      showToast("Could not show in folder: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error showing in folder: " + err.message, "error", 4000);
  }
}

async function openTorrentFile(hash, fileIndex) {
  try {
    const res = await fetch(`/api/torrents/${hash}/files/${fileIndex}/open`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      const name = data.path ? data.path.split('/').pop() : 'file';
      showToast(`Opening ${escapeHtml(name)}...`, "info", 2500);
    } else {
      showToast("Could not open file: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error opening file: " + err.message, "error", 4000);
  }
}

async function showTorrentFileInFolder(hash, fileIndex) {
  try {
    const res = await fetch(`/api/torrents/${hash}/files/${fileIndex}/show-in-folder`, { method: 'POST' });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast("Revealed file in File Manager", "info", 2000);
    } else {
      showToast("Could not show in folder: " + (data.error || 'Unknown error'), "error", 4000);
    }
  } catch (err) {
    showToast("Error showing in folder: " + err.message, "error", 4000);
  }
}

// Torrent Details Inspector
async function openDetailsModal(hash) {
  try {
    const res = await fetch(`/api/torrents/${hash}/details`);
    if (!res.ok) throw new Error("Could not load details");
    currentDetailData = await res.json();

    document.getElementById('detail-modal-title').textContent = currentDetailData.name || currentDetailData.info_hash;
    switchDetailTab('overview');
    saveFocusAndOpen('modal-details');
  } catch (err) {
    showToast("Error loading torrent details: " + err.message, "error");
  }
}

function closeDetailsModal() {
  restoreFocusAndClose('modal-details');
  currentDetailData = null;
}

function switchDetailTab(tab) {
  currentDetailTab = tab;
  ['overview', 'files', 'subtitles', 'peers', 'webseeds', 'trackers'].forEach(t => {
    const btn = document.getElementById(`dtab-${t}`);
    if (btn) btn.classList.toggle('active', t === tab);
  });

  const content = document.getElementById('detail-tab-content');
  if (!currentDetailData) return;

  if (tab === 'overview') {
    const suggHtml = currentDetailData.suggested_swarm ? `
      <div class="swarm-suggestion-banner" style="grid-column: 1 / -1; margin-bottom: 8px;">
        <div>
          ${ICONS.zap} <strong>${currentDetailData.suggested_swarm.is_partial ? 'Partial Match in Collection!' : 'Equivalent BitTorrent Swarm Verified!'}</strong> (${currentDetailData.suggested_swarm.seeders} seeds).
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
        <span class="detail-val">${formatBytes(currentDetailData.completed_bytes)} (${currentDetailData.progress.toFixed(1)}%)${(currentDetailData.is_verifying || currentDetailData.state === 'verifying' || (currentDetailData.verify_progress !== undefined && currentDetailData.verify_progress > 0)) ? ` • <span style="color: #c061cb; font-weight: 500;">Verifying integrity${(currentDetailData.verify_progress !== undefined && currentDetailData.verify_progress > 0) ? ` (${currentDetailData.verify_progress.toFixed(0)}% checked)` : ''}</span>` : ''}</span>

        <span class="detail-label">Pieces:</span>
        <span class="detail-val">${currentDetailData.num_pieces || '1'} pieces (${formatBytes(currentDetailData.piece_length || currentDetailData.total_bytes)} per piece)</span>

        <span class="detail-label">Storage Location:</span>
        <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px; flex-wrap: wrap;">
          <span class="detail-val" style="font-family: monospace; font-size: 11.5px; word-break: break-all;">${escapeHtml(currentDetailData.save_path || currentDetailData.download_dir)}</span>
          <div style="display: flex; gap: 6px;">
            <button class="btn" style="padding: 2px 8px; font-size: 11px;" title="Open file or folder with system default app" onclick="openTorrentTarget('${currentDetailData.info_hash}')">
              ${ICONS.play} Open
            </button>
            <button class="btn" style="padding: 2px 8px; font-size: 11px;" title="Reveal in system file manager" onclick="showTorrentInFolder('${currentDetailData.info_hash}')">
              ${ICONS.folder} Show in Folder
            </button>
          </div>
        </div>

        <span class="detail-label">Connected Swarm:</span>
        <span class="detail-val">
          ${currentDetailData.seeders !== undefined ? 
            `${currentDetailData.seeders} seed${currentDetailData.seeders !== 1 ? 's' : ''}, ${currentDetailData.leechers !== undefined ? currentDetailData.leechers : 0} peer${currentDetailData.leechers !== 1 ? 's' : ''} (${currentDetailData.total_peers || (currentDetailData.peers ? currentDetailData.peers.length : 0)} total connected)` : 
            `${currentDetailData.peers ? currentDetailData.peers.length : 0} peers connected`}
        </span>

        <span class="detail-label">Swarm Archetype:</span>
        <div class="detail-val">
          ${currentDetailData.qualifier ? `
            <div style="display: flex; flex-direction: column; gap: 4px;">
              <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap;">
                ${getQualifierBadge(currentDetailData.qualifier)}
                <span style="font-weight: 500;">${escapeHtml(currentDetailData.qualifier.description)}</span>
              </div>
              ${currentDetailData.qualifier.easter_egg ? `
                <div style="color: var(--adw-dim-label); font-size: 11.5px; margin-top: 2px; display: flex; align-items: center; gap: 4px;">
                  ${ICONS.lightbulb} <span style="font-style: italic;">"${escapeHtml(currentDetailData.qualifier.easter_egg)}"</span>
                </div>
              ` : ''}
            </div>
          ` : 'Standard P2P Swarm'}
        </div>

        ${currentDetailData.availability_eta ? `
          <span class="detail-label">Availability Forecast:</span>
          <div class="detail-val" style="color: #62a0ea; font-weight: 600;">
            ${ICONS.clock} ${escapeHtml(currentDetailData.availability_eta)}
            <span style="font-weight: normal; color: var(--adw-dim-label); font-size: 11.5px; margin-left: 6px;">
              (estimated seeder duty cycle: ${(currentDetailData.qualifier.uptime_ratio * 100).toFixed(0)}%)
            </span>
          </div>
        ` : ''}

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

    // Group files by directory
    const groups = new Map();
    currentDetailData.files.forEach((f, i) => {
      const fileIdx = f.index !== undefined ? f.index : i;
      const rawPath = f.path || '';
      const lastSlash = rawPath.lastIndexOf('/');
      const lastBackslash = rawPath.lastIndexOf('\\');
      const splitIdx = Math.max(lastSlash, lastBackslash);
      let dir = '';
      let name = rawPath;
      if (splitIdx !== -1) {
        dir = rawPath.substring(0, splitIdx);
        name = rawPath.substring(splitIdx + 1);
      }
      if (!groups.has(dir)) {
        groups.set(dir, []);
      }
      groups.get(dir).push({
        ...f,
        origIndex: fileIdx,
        basename: name,
        dir: dir
      });
    });

    const hasMultipleDirs = groups.size > 1 || (!groups.has('') && groups.size > 0);

    let rowsHtml = '';
    groups.forEach((groupFiles, dirPath) => {
      const dirTitle = dirPath || 'Root Directory';
      const allGroupSkipped = groupFiles.every(f => f.priority === 0 && !(f.completed || f.progress >= 100));
      const groupTotalBytes = groupFiles.reduce((acc, f) => acc + (f.length || 0), 0);
      const groupProgress = groupTotalBytes > 0
        ? (groupFiles.reduce((acc, f) => acc + ((f.length || 0) * (f.progress || 0)), 0) / groupTotalBytes)
        : 0;

      if (hasMultipleDirs) {
        rowsHtml += `
          <tr style="background: rgba(255,255,255,0.04); font-weight: 600; border-top: 1px solid rgba(128,128,128,0.2);">
            ${isTorrent ? `
              <td>
                <input type="checkbox" title="Toggle entire directory" ${!allGroupSkipped ? 'checked' : ''} onchange="setFolderFilesPriority('${currentDetailData.info_hash}', '${escapeHtml(dirPath)}', this.checked ? 1 : 0)">
              </td>
            ` : ''}
            <td colspan="${isTorrent ? 4 : 3}" style="padding: 7px 10px;">
              <div style="display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 6px;">
                <div style="display: flex; align-items: center; gap: 6px; min-width: 0;">
                  ${ICONS.folder}
                  <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(dirTitle)}">${escapeHtml(dirTitle)}</span>
                  <span style="font-size: 11px; font-weight: normal; color: var(--adw-dim-label); margin-left: 6px;">
                    (${groupFiles.length} files • ${formatBytes(groupTotalBytes)} • ${groupProgress.toFixed(0)}%)
                  </span>
                </div>
                ${isTorrent ? `
                  <div style="display: flex; gap: 4px;">
                    <button class="btn" style="padding: 1px 6px; font-size: 10.5px;" onclick="setFolderFilesPriority('${currentDetailData.info_hash}', '${escapeHtml(dirPath)}', 1)">Select</button>
                    <button class="btn" style="padding: 1px 6px; font-size: 10.5px;" onclick="setFolderFilesPriority('${currentDetailData.info_hash}', '${escapeHtml(dirPath)}', 0)">Skip</button>
                    <button class="btn" style="padding: 1px 6px; font-size: 10.5px;" onclick="setFolderFilesPriority('${currentDetailData.info_hash}', '${escapeHtml(dirPath)}', 2)">High</button>
                  </div>
                ` : ''}
              </div>
            </td>
            <td style="text-align: right; padding: 7px 10px;"></td>
          </tr>
        `;
      }

      groupFiles.forEach(f => {
        const fileIdx = f.origIndex;
        const isCompleted = f.completed || f.progress >= 100;
        const isSkipped = f.priority === 0 && !isCompleted;
        const icon = getIconForFile(f.path);
        rowsHtml += `
          <tr>
            ${isTorrent ? `
              <td>
                <input type="checkbox" ${!isSkipped ? 'checked' : ''} onchange="toggleFileDownload('${currentDetailData.info_hash}', ${fileIdx}, this.checked)">
              </td>
            ` : ''}
            <td style="word-break: break-all; ${isSkipped ? 'opacity: 0.5; text-decoration: line-through;' : ''}">
              <div style="display: flex; align-items: center; gap: 6px; padding-left: ${hasMultipleDirs ? '16px' : '0'};">
                ${icon}
                <span class="file-name-link" style="${isCompleted ? 'cursor: pointer; font-weight: 500;' : ''}" ${isCompleted ? `title="Click to open with default application" onclick="openTorrentFile('${currentDetailData.info_hash}', ${fileIdx})"` : ''}>
                  ${escapeHtml(hasMultipleDirs ? (f.basename || f.path) : f.path)}
                </span>
              </div>
            </td>
            <td style="white-space: nowrap;">${formatBytes(f.length)}</td>
            <td style="white-space: nowrap;">${f.progress.toFixed(0)}%</td>
            ${isTorrent ? `
              <td>
                <select class="sort-select" style="padding: 2px 4px; font-size: 11px;" onchange="updateFilePriority('${currentDetailData.info_hash}', ${fileIdx}, this.value)">
                  <option value="1" ${!isSkipped && f.priority !== 2 ? 'selected' : ''}>Normal</option>
                  <option value="2" ${f.priority === 2 ? 'selected' : ''}>High</option>
                  <option value="0" ${isSkipped ? 'selected' : ''}>Skip</option>
                </select>
              </td>
            ` : ''}
            <td style="white-space: nowrap; text-align: right;">
              <div style="display: flex; gap: 4px; justify-content: flex-end; align-items: center;">
                ${isCompleted ? `
                  <button class="btn" style="padding: 2px 6px; font-size: 11px;" title="Open with system application" onclick="openTorrentFile('${currentDetailData.info_hash}', ${fileIdx})">
                    ${ICONS.play} Open
                  </button>
                ` : ''}
                <button class="btn btn-icon" style="padding: 2px 6px; font-size: 11px;" title="Show in File Manager" onclick="showTorrentFileInFolder('${currentDetailData.info_hash}', ${fileIdx})">
                  ${ICONS.folder}
                </button>
                <a class="btn btn-icon" style="padding: 2px 6px; font-size: 11px; text-decoration: none;" title="Stream or view in browser" target="_blank" href="/api/torrents/${currentDetailData.info_hash}/files/${fileIdx}/view">
                  ${ICONS.external}
                </a>
              </div>
            </td>
          </tr>
        `;
      });
    });

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
            <th style="width: 85px;">Size</th>
            <th style="width: 75px;">Progress</th>
            ${isTorrent ? '<th style="width: 90px;">Priority</th>' : ''}
            <th style="width: 120px; text-align: right;">Actions</th>
          </tr>
        </thead>
        <tbody>
          ${rowsHtml}
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
  } else if (tab === 'subtitles') {
    const subs = currentDetailData.subtitles || [];
    const hash = currentDetailData.info_hash;
    const defaultQuery = currentDetailData.name ? currentDetailData.name.replace(/\.[^/.]+$/, "").replace(/[._-]/g, " ") : "";

    content.innerHTML = `
      <div style="display: flex; flex-direction: column; gap: 14px;">
        <div>
          <div style="font-weight: 600; font-size: 13px; margin-bottom: 6px;">Attached Subtitles & Audio Tracks (${subs.length}):</div>
          ${subs.length === 0 ? 
            '<div style="color: var(--adw-dim-label); font-size: 12px;">No subtitle tracks attached or detected yet. Search OpenSubtitles below to download and auto-pair.</div>' :
            `<div style="display: flex; flex-direction: column; gap: 6px;">
              ${subs.map(s => `
                <div class="sub-track-card">
                  <div>
                    <span style="font-weight: 600; font-size: 12.5px;">${escapeHtml(s.title || s.language)}</span>
                    <span style="color: var(--adw-dim-label); font-size: 11px; margin-left: 6px;">[${escapeHtml(s.format || 'srt')}] • ${escapeHtml(s.language || 'English')}</span>
                    ${s.is_embedded ? '<span style="background: rgba(53,132,228,0.2); color: #78aeed; font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 4px; margin-left: 6px;">Embedded Stream</span>' : '<span style="background: rgba(46,194,126,0.2); color: #57e389; font-size: 10px; font-weight: 700; padding: 2px 6px; border-radius: 4px; margin-left: 6px;">Attached File</span>'}
                  </div>
                  <div>
                    ${s.is_embedded ? `
                      <button class="btn" style="padding: 2px 8px; font-size: 11px;" onclick="extractSubtitleStream('${hash}', ${s.stream_index || 0}, '${s.language_code || 'en'}', this)">
                        Extract to .srt
                      </button>
                    ` : `
                      <button class="btn" style="padding: 2px 8px; font-size: 11px;" onclick="showTorrentInFolder('${hash}')">
                        ${ICONS.folder} View
                      </button>
                    `}
                  </div>
                </div>
              `).join('')}
            </div>`
          }
        </div>

        <!-- Online Subtitle Search Section -->
        <div style="background: rgba(0,0,0,0.15); border: 1px solid var(--adw-border); border-radius: var(--adw-radius-sm); padding: 12px; display: flex; flex-direction: column; gap: 10px;">
          <div style="font-weight: 600; font-size: 13px; display: flex; align-items: center; gap: 6px;">
            ${ICONS.search}
            <span>Search OpenSubtitles / Global Subtitle Index</span>
          </div>
          <div style="display: flex; gap: 8px; flex-wrap: wrap;">
            <input type="text" id="sub-search-query" class="input-text" style="flex: 1; min-width: 180px;" value="${escapeHtml(defaultQuery)}" placeholder="Movie or Show Title">
            <select id="sub-search-lang" class="sort-select" style="min-width: 110px;">
              <option value="en">English (en)</option>
              <option value="es">Spanish (es)</option>
              <option value="fr">French (fr)</option>
              <option value="de">German (de)</option>
              <option value="it">Italian (it)</option>
              <option value="pt">Portuguese (pt)</option>
              <option value="ru">Russian (ru)</option>
              <option value="zh">Chinese (zh)</option>
              <option value="ja">Japanese (ja)</option>
              <option value="ko">Korean (ko)</option>
              <option value="eo">Esperanto (eo)</option>
              <option value="nl">Dutch (nl)</option>
              <option value="pl">Polish (pl)</option>
              <option value="all">All Languages</option>
            </select>
            <button class="btn btn-primary" onclick="performSubtitleSearch('${hash}')">
              ${ICONS.search} Search
            </button>
          </div>

          <div id="sub-search-results" style="display: none; margin-top: 6px;">
            <!-- Subtitle search results rendered dynamically -->
          </div>
        </div>
      </div>
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

async function performSubtitleSearch(hash) {
  const qInput = document.getElementById('sub-search-query');
  const langSelect = document.getElementById('sub-search-lang');
  const resultsBox = document.getElementById('sub-search-results');
  if (!qInput || !resultsBox) return;

  const query = qInput.value.trim();
  const lang = langSelect ? langSelect.value : 'en';
  if (!query) {
    showToast("Please enter a title to search subtitles", "warning");
    return;
  }

  resultsBox.style.display = 'block';
  resultsBox.innerHTML = '<div style="color: var(--adw-dim-label); font-size: 12px; padding: 10px; text-align: center;">Searching OpenSubtitles & global indices...</div>';

  try {
    const res = await fetch('/api/subtitles/search', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hash, query, lang })
    });
    if (!res.ok) {
      throw new Error(`Server returned ${res.status}`);
    }
    const tracks = await res.json();
    if (!tracks || tracks.length === 0) {
      resultsBox.innerHTML = '<div style="color: var(--adw-dim-label); font-size: 12px; padding: 10px; text-align: center;">No matching subtitle tracks found. Try modifying the search query.</div>';
      return;
    }

    resultsBox.innerHTML = `
      <div style="font-weight: 600; font-size: 12px; margin-bottom: 6px; color: var(--adw-fg-color);">Search Results (${tracks.length}):</div>
      <div style="display: flex; flex-direction: column; gap: 6px; max-height: 220px; overflow-y: auto;">
        ${tracks.map(tr => `
          <div class="sub-track-card">
            <div style="min-width: 0; flex: 1;">
              <div style="font-weight: 600; font-size: 12px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">${escapeHtml(tr.title || tr.language)}</div>
              <div style="font-size: 11px; color: var(--adw-dim-label); display: flex; gap: 8px; margin-top: 2px;">
                <span>${escapeHtml(tr.language || 'English')}</span>
                <span>• ${escapeHtml(tr.provider || 'OpenSubtitles')}</span>
                ${tr.downloads ? `<span>• ↓ ${tr.downloads} downloads</span>` : ''}
                ${tr.hearing_impaired ? '<span style="color: #f6d32d;">• [HI]</span>' : ''}
              </div>
            </div>
            <div>
              <button class="btn btn-primary" style="padding: 3px 8px; font-size: 11px;" onclick="downloadSubtitleTrack('${hash}', '${encodeURI(tr.download_url)}', '${tr.language_code || 'en'}', '${escapeHtml(tr.title || '')}', this)">
                ${ICONS.download} Download & Pair
              </button>
            </div>
          </div>
        `).join('')}
      </div>
    `;
  } catch (err) {
    resultsBox.innerHTML = `<div style="color: var(--adw-error); font-size: 12px; padding: 10px; text-align: center;">Failed to search subtitles: ${escapeHtml(err.message)}</div>`;
  }
}

async function downloadSubtitleTrack(hash, downloadUrl, lang, filename, btn) {
  if (btn) {
    btn.disabled = true;
    btn.textContent = "Downloading...";
  }

  try {
    const res = await fetch('/api/subtitles/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        hash,
        download_url: decodeURI(downloadUrl),
        lang,
        file_name: filename
      })
    });
    if (!res.ok) {
      throw new Error(`Server returned ${res.status}`);
    }
    const data = await res.json();
    showToast("Subtitle attached successfully!", "success");

    // Refresh details modal
    if (currentDetailData && currentDetailData.info_hash === hash) {
      const detailRes = await fetch(`/api/torrents/${hash}/details`);
      if (detailRes.ok) {
        currentDetailData = await detailRes.json();
        switchDetailTab('subtitles');
      }
    }
  } catch (err) {
    showToast(`Download failed: ${err.message}`, "error");
    if (btn) {
      btn.disabled = false;
      btn.textContent = "Retry";
    }
  }
}

async function extractSubtitleStream(hash, streamIndex, lang, btn) {
  if (btn) {
    btn.disabled = true;
    btn.textContent = "Extracting...";
  }

  try {
    const res = await fetch('/api/subtitles/extract', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hash, stream_index: streamIndex, lang })
    });
    if (!res.ok) {
      throw new Error(`Server returned ${res.status}`);
    }
    showToast("Embedded subtitle extracted to .srt file!", "success");

    // Refresh details modal
    if (currentDetailData && currentDetailData.info_hash === hash) {
      const detailRes = await fetch(`/api/torrents/${hash}/details`);
      if (detailRes.ok) {
        currentDetailData = await detailRes.json();
        switchDetailTab('subtitles');
      }
    }
  } catch (err) {
    showToast(`Extraction failed: ${err.message}`, "error");
    if (btn) {
      btn.disabled = false;
      btn.textContent = "Retry";
    }
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
  saveFocusAndOpen('modal-send', '#send-path-input');
}

function closeSendModal() {
  restoreFocusAndClose('modal-send');
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
        showToast("Torrent created and now seeding to DHT network!", "accent");
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
        showToast("Web mirror bridged to BitTorrent swarm with WebSeed acceleration!", "accent");
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
async function executeSearch(query) {
  if (!query) return;
  const input = document.getElementById('search-input');
  if (input) input.value = query;

  currentPathFilter = null;

  const spinner = document.getElementById('search-spinner');
  const container = document.getElementById('search-results-container');
  const empty = document.getElementById('search-empty');
  const controls = document.getElementById('search-controls');

  if (spinner) spinner.style.display = 'block';
  if (container) container.innerHTML = '';
  if (empty) empty.style.display = 'none';
  if (controls) controls.style.display = 'none';

  try {
    const res = await fetch(`/api/search?q=${encodeURIComponent(query)}`);
    rawSearchResults = await res.json() || [];

    if (spinner) spinner.style.display = 'none';

    if (rawSearchResults.length === 0) {
      if (empty) empty.style.display = 'block';
      return;
    }

    currentSourceFilter = 'all';
    if (controls) controls.style.display = 'flex';
    renderSourceFilterChips();
    renderSearchResults();
  } catch (err) {
    if (spinner) spinner.style.display = 'none';
    showToast("Search failed: " + err.message, "error", 3500);
  }
}

async function handleSearchSubmit(e) {
  if (e) e.preventDefault();
  const input = document.getElementById('search-input');
  const query = input ? input.value.trim() : '';
  await executeSearch(query);
}

async function rereadSearchForUser(user, event) {
  if (event) {
    event.stopPropagation();
    event.preventDefault();
  }
  if (!user) return;
  searchGroupByFolder = true;
  const btn = document.getElementById('search-group-toggle-btn');
  const label = document.getElementById('search-group-toggle-label');
  if (btn) {
    btn.classList.add('btn-primary');
    if (label) label.textContent = 'Folder Grouping (On)';
  }
  showToast(`Searching all shared files from user "${user}"...`, "info", 2500);
  await executeSearch(`user:${user}`);
}

async function rereadSearchForArtist(artist, event, userContext) {
  if (event) {
    event.stopPropagation();
    event.preventDefault();
  }
  if (!artist) return;
  searchGroupByFolder = true;
  const btn = document.getElementById('search-group-toggle-btn');
  const label = document.getElementById('search-group-toggle-label');
  if (btn) {
    btn.classList.add('btn-primary');
    if (label) label.textContent = 'Folder Grouping (On)';
  }
  showToast(`Searching all albums & tracks for artist "${artist}"...`, "info", 2500);
  await executeSearch(`artist:${artist}`);
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

let searchGroupByFolder = false;
let currentPathFilter = null; // { type: 'artist'|'album'|'folder'|'user', value: string, label: string }
let currentFilteredResults = [];
let currentFolderGroups = [];

function escapeJs(str) {
  return (str || '').replace(/\\/g, '\\\\').replace(/'/g, "\\'").replace(/"/g, '&quot;');
}

function generatePathBreadcrumbsHtml(r, idx) {
  const crumbs = [];
  const userArg = r.user ? `'${escapeJs(r.user)}'` : "''";

  // Pre-calculate counts across rawSearchResults
  let userCount = 0;
  let artistCount = 0;
  let albumCount = 0;

  if (rawSearchResults && rawSearchResults.length > 0) {
    if (r.user) {
      const uLower = r.user.toLowerCase();
      userCount = rawSearchResults.filter(item => (item.user || '').toLowerCase() === uLower).length;
    }
    if (r.artist) {
      const aLower = r.artist.toLowerCase();
      if (r.user) {
        const uLower = r.user.toLowerCase();
        artistCount = rawSearchResults.filter(item => (item.user || '').toLowerCase() === uLower && (
          (item.artist || '').toLowerCase() === aLower ||
          (item.directory || '').toLowerCase().includes(aLower) ||
          (item.path || '').toLowerCase().includes(aLower)
        )).length;
      }
      if (artistCount === 0) {
        artistCount = rawSearchResults.filter(item => (
          (item.artist || '').toLowerCase() === aLower ||
          (item.directory || '').toLowerCase().includes(aLower) ||
          (item.path || '').toLowerCase().includes(aLower)
        )).length;
      }
    }
    if (r.album) {
      const albLower = r.album.toLowerCase();
      if (r.user) {
        const uLower = r.user.toLowerCase();
        albumCount = rawSearchResults.filter(item => (item.user || '').toLowerCase() === uLower && (
          (item.album || '').toLowerCase() === albLower ||
          (item.directory || '').toLowerCase().includes(albLower) ||
          (item.path || '').toLowerCase().includes(albLower)
        )).length;
      }
      if (albumCount === 0) {
        albumCount = rawSearchResults.filter(item => (
          (item.album || '').toLowerCase() === albLower ||
          (item.directory || '').toLowerCase().includes(albLower) ||
          (item.path || '').toLowerCase().includes(albLower)
        )).length;
      }
    }
  }

  // 1. User / Source Peer (rereads search for this user)
  if (r.user) {
    const userBadge = userCount > 0 ? `<span class="crumb-counter" title="${userCount} files from this user in current search">${userCount}</span>` : '';
    crumbs.push(`
      <span class="crumb-folder" title="Source User: ${escapeHtml(r.user)}. Click to search all shared files from this user." onclick="rereadSearchForUser('${escapeJs(r.user)}', event)">
        👤 ${escapeHtml(r.user)}${userBadge}
      </span>
    `);
  }

  // 2. Artist (rereads search for this artist)
  if (r.artist) {
    const artistTitle = r.user ? `Artist: ${escapeHtml(r.artist)} (shared by ${escapeHtml(r.user)}). Click to search all tracks & albums for this artist.` : `Artist: ${escapeHtml(r.artist)}. Click to search all tracks & albums for this artist.`;
    const artistBadge = artistCount > 0 ? `<span class="crumb-counter" title="${artistCount} files from this artist in current search">${artistCount}</span>` : '';
    crumbs.push(`
      <span class="crumb-folder" title="${artistTitle}" onclick="rereadSearchForArtist('${escapeJs(r.artist)}', event, ${userArg})">
        🎤 ${escapeHtml(r.artist)}${artistBadge}
      </span>
    `);
  }

  // 3. Album (group download view)
  if (r.album) {
    const albumBadge = albumCount > 0 ? `<span class="crumb-counter" title="${albumCount} files in this album in current search">${albumCount}</span>` : '';
    crumbs.push(`
      <span class="crumb-folder" style="font-weight: 600;" title="Album: ${escapeHtml(r.album)}. Click to group download." onclick="showGroupedDownload('album', '${escapeJs(r.album)}', event, ${userArg})">
        💿 ${escapeHtml(r.album)}${albumBadge}
      </span>
    `);
  }

  // 4. Directory path segments (subfolders beyond user/artist/album)
  const dirStr = r.directory || r.path;
  if (dirStr) {
    const norm = dirStr.replace(/\\/g, '/');
    const parts = norm.split('/').map(p => p.trim()).filter(Boolean);
    for (const p of parts) {
      const pLower = p.toLowerCase();
      if (r.user && pLower === r.user.toLowerCase()) continue;
      if (r.artist && (pLower === r.artist.toLowerCase() || pLower.includes(r.artist.toLowerCase()))) continue;
      if (r.album && (pLower === r.album.toLowerCase() || pLower.includes(r.album.toLowerCase()))) continue;

      let folderCount = 0;
      if (rawSearchResults && rawSearchResults.length > 0) {
        if (r.user) {
          const uLower = r.user.toLowerCase();
          folderCount = rawSearchResults.filter(item => (item.user || '').toLowerCase() === uLower && (
            (item.directory || '').toLowerCase().includes(pLower) ||
            (item.path || '').toLowerCase().includes(pLower)
          )).length;
        }
        if (folderCount === 0) {
          folderCount = rawSearchResults.filter(item => (
            (item.directory || '').toLowerCase().includes(pLower) ||
            (item.path || '').toLowerCase().includes(pLower)
          )).length;
        }
      }
      const folderBadge = folderCount > 0 ? `<span class="crumb-counter" title="${folderCount} files in this folder in current search">${folderCount}</span>` : '';

      crumbs.push(`
        <span class="crumb-folder" title="Folder: ${escapeHtml(p)}. Click to group download." onclick="showGroupedDownload('folder', '${escapeJs(p)}', event, ${userArg})">
          📁 ${escapeHtml(p)}${folderBadge}
        </span>
      `);
    }
  }

  if (crumbs.length === 0) return '';

  return `
    <div class="search-path-breadcrumb">
      <span class="emoji-face" style="font-size: 12px; margin-right: 2px;">📁</span>
      ${crumbs.join(' <span class="crumb-separator">/</span> ')}
    </div>
  `;
}

function showGroupedDownload(filterType, filterValue, event, userContext) {
  if (event) {
    event.stopPropagation();
    event.preventDefault();
  }
  if (!filterValue) return;

  // Toggle off if already active for this exact filter and user
  if (currentPathFilter && currentPathFilter.type === filterType &&
      currentPathFilter.value.toLowerCase() === filterValue.toLowerCase() &&
      (currentPathFilter.user || '').toLowerCase() === (userContext || '').toLowerCase()) {
    clearPathFilter();
    return;
  }

  let displayLabel = filterValue;
  if (userContext && (filterType === 'artist' || filterType === 'album' || filterType === 'folder')) {
    displayLabel = `${filterValue} (from ${userContext})`;
  }

  currentPathFilter = {
    type: filterType,
    value: filterValue,
    user: userContext || null,
    initialUser: userContext || null,
    label: displayLabel
  };

  // Automatically enable folder grouping for clear album/folder card structure
  searchGroupByFolder = true;
  const btn = document.getElementById('search-group-toggle-btn');
  const label = document.getElementById('search-group-toggle-label');
  if (btn) {
    btn.classList.add('btn-primary');
    if (label) label.textContent = 'Folder Grouping (On)';
  }

  renderSearchResults();

  const container = document.getElementById('search-results-container');
  if (container) {
    container.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  showToast(`Showing grouped download view for ${filterType}: "${displayLabel}"`, "info", 2400);
}

function toggleArtistUserScope() {
  if (!currentPathFilter || currentPathFilter.type !== 'artist') return;
  if (currentPathFilter.user) {
    currentPathFilter.user = null;
    currentPathFilter.label = `${currentPathFilter.value} (all users)`;
  } else if (currentPathFilter.initialUser) {
    currentPathFilter.user = currentPathFilter.initialUser;
    currentPathFilter.label = `${currentPathFilter.value} (from ${currentPathFilter.initialUser})`;
  }
  renderSearchResults();
}

function clearPathFilter() {
  currentPathFilter = null;
  renderSearchResults();
}

function filterSearchByArtist(artist) {
  showGroupedDownload('artist', artist);
}

async function downloadGroupAsFolder(results, btn, groupTitle, userContext) {
  if (!results || results.length === 0) return;
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `<span style="opacity: 0.7;">Organizing folder download...</span>`;
  }

  const effectiveUser = userContext || (currentPathFilter ? currentPathFilter.user : null);
  let folderName = groupTitle || 'Group Download';
  if (effectiveUser && !folderName.toLowerCase().startsWith(effectiveUser.toLowerCase())) {
    folderName = `${effectiveUser} - ${folderName}`;
  }

  showToast(`Organizing ${results.length} files as unified folder download "${folderName}"...`, "info", 3000);

  const payload = {
    name: groupTitle || 'Group Download',
    folder_name: folderName,
    items: results.map(r => {
      const item = r.result || r;
      let path = item.path || item.directory || '';
      if (!path && item.album && item.title) {
        path = `${item.album}/${item.title}`;
      } else if (!path) {
        path = item.title || '';
      }
      return {
        url: item.magnet_uri || item.url || '',
        title: item.title || '',
        artist: item.artist || '',
        album: item.album || '',
        directory: item.directory || '',
        path: path,
        size: item.size_bytes || 0
      };
    })
  };

  try {
    const res = await fetch('/api/torrents/add-group', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (data.status === 'ok') {
      showToast(`✓ Added "${payload.name}" as folder download (${data.num_files || results.length} files)! Swarm will be created afterwards.`, "info", 4500);
      switchTab('transfers');
      fetchTorrents();
    } else {
      showToast(`Failed to add folder download: ${data.error || 'unknown error'}`, "error", 4500);
    }
  } catch (e) {
    showToast(`Error adding folder download: ${e.message}`, "error", 4500);
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `<span class="emoji-face" style="margin-right: 4px;">📥</span><span>Download Folder Swarm</span>`;
    }
  }
}

async function downloadMultipleResults(results, btn, groupTitle) {
  return downloadGroupAsFolder(results, btn, groupTitle);
}

async function downloadAllInCurrentFilter(btn) {
  if (!currentFilteredResults || currentFilteredResults.length === 0) return;
  const label = currentPathFilter ? currentPathFilter.label : 'Group Download';
  const user = currentPathFilter ? currentPathFilter.user : null;
  await downloadGroupAsFolder(currentFilteredResults, btn, label, user);
}

async function downloadFolderGroup(groupIdx, btn) {
  const g = currentFolderGroups[groupIdx];
  if (!g || !g.items || g.items.length === 0) return;
  await downloadGroupAsFolder(g.items, btn, g.album || g.key, g.user);
}

function openGroupInspectModal() {
  if (!currentFilteredResults || currentFilteredResults.length === 0) return;
  const first = currentFilteredResults[0];
  const firstIdx = rawSearchResults.indexOf(first);
  if (firstIdx !== -1) {
    openInspectModal(firstIdx);
  } else {
    openInspectModal(0);
  }
}

function toggleSearchGroupMode() {
  searchGroupByFolder = !searchGroupByFolder;
  const btn = document.getElementById('search-group-toggle-btn');
  const label = document.getElementById('search-group-toggle-label');
  if (btn) {
    if (searchGroupByFolder) {
      btn.classList.add('btn-primary');
      if (label) label.textContent = 'Folder Grouping (On)';
    } else {
      btn.classList.remove('btn-primary');
      if (label) label.textContent = 'Group by Folder';
    }
  }
  renderSearchResults();
}

function handleSortChange(sortType) {
  currentSortBy = sortType;
  renderSearchResults();
}

function handleSearchCardKeydown(e, idx) {
  const card = e.currentTarget;
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    const next = card.nextElementSibling;
    if (next && next.classList.contains('search-card')) {
      next.focus();
    }
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    const prev = card.previousElementSibling;
    if (prev && prev.classList.contains('search-card')) {
      prev.focus();
    }
  } else if (e.key === 'Home') {
    e.preventDefault();
    const first = card.parentElement ? card.parentElement.querySelector('.search-card') : null;
    if (first) first.focus();
  } else if (e.key === 'End') {
    e.preventDefault();
    const cards = card.parentElement ? card.parentElement.querySelectorAll('.search-card') : [];
    if (cards.length > 0) cards[cards.length - 1].focus();
  } else if (e.key === 'Enter' || e.key === ' ') {
    if (e.target === card) {
      e.preventDefault();
      openInspectModal(idx);
    }
  }
}

function renderSearchResults() {
  const container = document.getElementById('search-results-container');
  const empty = document.getElementById('search-empty');

  let filtered = rawSearchResults;
  if (currentSourceFilter !== 'all') {
    filtered = rawSearchResults.filter(r => r.provider_type === currentSourceFilter);
  }

  // Filter in-place by active path element without reloading search
  if (currentPathFilter) {
    const valLower = currentPathFilter.value.toLowerCase();
    const userFilterLower = currentPathFilter.user ? currentPathFilter.user.toLowerCase() : null;
    filtered = filtered.filter(r => {
      // If scoped to a specific sharing user, limit by user
      if (userFilterLower && (r.user || '').toLowerCase() !== userFilterLower) {
        return false;
      }
      if (currentPathFilter.type === 'user') {
        return (r.user || '').toLowerCase() === valLower;
      } else if (currentPathFilter.type === 'artist') {
        return (r.artist || '').toLowerCase() === valLower ||
               (r.directory || '').toLowerCase().includes(valLower) ||
               (r.path || '').toLowerCase().includes(valLower);
      } else if (currentPathFilter.type === 'album') {
        return (r.album || '').toLowerCase() === valLower ||
               (r.directory || '').toLowerCase().includes(valLower) ||
               (r.path || '').toLowerCase().includes(valLower);
      } else if (currentPathFilter.type === 'folder') {
        return (r.directory || '').toLowerCase().includes(valLower) ||
               (r.path || '').toLowerCase().includes(valLower);
      }
      return true;
    });
  }

  currentFilteredResults = filtered;

  if (filtered.length === 0) {
    if (currentPathFilter) {
      container.innerHTML = `
        <div class="active-path-filter-bar">
          <div>
            <span>No results matching <strong>${escapeHtml(currentPathFilter.label)}</strong></span>
          </div>
          <button class="btn btn-sm" onclick="clearPathFilter()">✕ Show All Results</button>
        </div>
      `;
      empty.style.display = 'none';
      return;
    }
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

  // Active path filter banner
  let bannerHtml = '';
  if (currentPathFilter) {
    const totalBytesInFilter = filtered.reduce((acc, r) => acc + (r.size_bytes || 0), 0);
    let userScopeBtn = '';
    if (currentPathFilter.type === 'artist') {
      if (currentPathFilter.user) {
        userScopeBtn = `
          <button class="btn btn-sm" onclick="toggleArtistUserScope()" title="Expand to show this artist from all sharing users">
            <span>👥 All Users</span>
          </button>
        `;
      } else if (currentPathFilter.initialUser) {
        userScopeBtn = `
          <button class="btn btn-sm" onclick="toggleArtistUserScope()" title="Limit this artist only to sharing user ${escapeHtml(currentPathFilter.initialUser)}">
            <span>👤 Only ${escapeHtml(currentPathFilter.initialUser)}</span>
          </button>
        `;
      }
    }

    bannerHtml = `
      <div class="active-path-filter-bar">
        <div style="display: flex; align-items: center; gap: 10px; min-width: 0; flex: 1;">
          <span class="emoji-face" style="font-size: 20px;">📁</span>
          <div style="min-width: 0;">
            <div style="font-size: 13.5px; font-weight: 600; display: flex; align-items: center; gap: 6px; flex-wrap: wrap;">
              <span>Grouped:</span>
              <span style="color: var(--adw-accent-color); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(currentPathFilter.label)}</span>
              <span style="font-size: 11px; opacity: 0.75; font-weight: normal;">(${currentPathFilter.type})</span>
            </div>
            <div style="font-size: 11.5px; color: var(--adw-dim-label); margin-top: 2px;">
              ${filtered.length} ${filtered.length === 1 ? 'item' : 'tracks/files'} &bull; ${formatBytes(totalBytesInFilter)}
            </div>
          </div>
        </div>
        <div style="display: flex; gap: 8px; align-items: center; flex-shrink: 0;">
          ${userScopeBtn}
          <button class="btn btn-primary btn-sm" onclick="downloadAllInCurrentFilter(this)" title="Download all ${filtered.length} items as one organized folder download with automated swarm creation afterwards">
            <span class="emoji-face" style="margin-right: 4px;">📥</span>
            <span>Download Folder Swarm (${filtered.length})</span>
          </button>
          <button class="btn btn-sm" onclick="openGroupInspectModal()" title="Inspect individual files in this group">
            <span class="emoji-face" style="margin-right: 4px;">📦</span>
            <span>Inspect</span>
          </button>
          <button class="btn btn-sm" onclick="clearPathFilter()" title="Show all results from search">
            <span>✕ Show All</span>
          </button>
        </div>
      </div>
    `;
  }

  // Grouped by Folder / Album View
  if (searchGroupByFolder) {
    const folderGroups = new Map();
    sorted.forEach((r, origIdx) => {
      let groupKey = r.directory || r.path;
      if (!groupKey && r.artist && r.album) {
        groupKey = `${r.artist} / ${r.album}`;
      } else if (!groupKey && r.artist) {
        groupKey = r.artist;
      }
      if (!groupKey) {
        groupKey = r.title || 'Ungrouped Collection';
      }
      // Grouping by default is limited to the sharing user
      if (r.user && !groupKey.toLowerCase().startsWith((r.user + ' /').toLowerCase())) {
        groupKey = `${r.user} / ${groupKey}`;
      }

      if (!folderGroups.has(groupKey)) {
        folderGroups.set(groupKey, {
          key: groupKey,
          artist: r.artist || '',
          album: r.album || '',
          user: r.user || '',
          directory: r.directory || r.path || groupKey,
          provider_type: r.provider_type || 'soulseek',
          provider: r.provider || '',
          items: [],
          totalBytes: 0,
          seeders: r.seeders !== undefined ? r.seeders : -1,
          leechers: r.leechers !== undefined ? r.leechers : -1,
          magnet_uri: r.magnet_uri
        });
      }
      const g = folderGroups.get(groupKey);
      g.items.push({ result: r, idx: origIdx });
      g.totalBytes += (r.size_bytes || 0);
      if (r.seeders !== undefined && r.seeders > g.seeders) {
        g.seeders = r.seeders;
      }
    });

    currentFolderGroups = Array.from(folderGroups.values());

    const groupsHtml = currentFolderGroups.map((g, groupIdx) => {
      const tagClass = `tag-${g.provider_type || 'soulseek'}`;
      const itemsCount = g.items.length;
      const firstResultIdx = g.items[0].idx;
      const breadcrumbHtml = generatePathBreadcrumbsHtml(g, firstResultIdx);

      return `
        <div class="folder-group-card" id="folder-group-${groupIdx}">
          <div class="folder-group-header">
            <div style="flex: 1; min-width: 0;">
              <div class="folder-group-title" title="${escapeHtml(g.key)}">
                <span class="emoji-face" style="font-size: 16px;">📁</span>
                <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${escapeHtml(g.key)}</span>
              </div>
              <div style="margin-top: 4px;">
                ${breadcrumbHtml}
              </div>
              <div class="folder-group-stats">
                <span>${ICONS.package} ${formatBytes(g.totalBytes)}</span>
                <span>${ICONS.folder} ${itemsCount} ${itemsCount === 1 ? 'item' : 'tracks/files'}</span>
                <span class="provider-badge ${tagClass}">${escapeHtml(g.provider || g.provider_type)}</span>
              </div>
            </div>
            <div class="folder-group-actions" onclick="event.stopPropagation()">
              <button class="btn btn-sm" title="Inspect files and directory tree" onclick="openInspectModal(${firstResultIdx})">
                <span class="emoji-face" style="margin-right: 3px;">📦</span>
                <span>Files</span>
              </button>
              <button class="btn btn-primary btn-sm" title="Download entire folder as single download with automated swarm creation afterwards" onclick="downloadFolderGroup(${groupIdx}, this)">
                <span class="emoji-face" style="margin-right: 3px;">📥</span>
                <span>Download Folder</span>
              </button>
            </div>
          </div>
          <div class="folder-group-body">
            ${g.items.map(({ result: item, idx: itemIdx }) => {
              const itemSize = item.size_bytes > 0 ? formatBytes(item.size_bytes) : '';
              return `
                <div class="folder-track-row">
                  <div style="display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; padding-right: 12px;">
                    <span class="emoji-face" style="font-size: 13px;">🎵</span>
                    <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500;" title="${escapeHtml(item.title)}">${escapeHtml(item.title)}</span>
                  </div>
                  <div style="display: flex; align-items: center; gap: 10px; flex-shrink: 0;">
                    <span style="font-size: 11.5px; color: var(--adw-dim-label); font-weight: 500;">${itemSize}</span>
                    <button class="btn btn-sm" style="font-size: 11px; padding: 2px 7px;" onclick="openInspectModal(${itemIdx})" title="Inspect file details">
                      <span class="emoji-face" style="margin-right: 2px;">📦</span>
                      <span>Inspect</span>
                    </button>
                    <button class="btn btn-primary btn-sm" style="font-size: 11px; padding: 2px 9px;" onclick="downloadFromSearch('${encodeURIComponent(item.magnet_uri)}', this)" title="Download track">
                      <span class="emoji-face" style="margin-right: 2px;">📥</span>
                      <span>Download</span>
                    </button>
                  </div>
                </div>
              `;
            }).join('')}
          </div>
        </div>
      `;
    }).join('');

    container.innerHTML = bannerHtml + groupsHtml;
    return;
  }

  // Flat Search Card View (with directory breadcrumbs and higher layer actions)
  const cardsHtml = sorted.map((r, idx) => {
    const tagClass = `tag-${r.provider_type || 'torrentscsv'}`;
    const scoreText = r.score > 0 ? `<span class="score-badge">Relevance: ${r.score.toFixed(0)}</span>` : '';
    const fileCountBadge = (r.files && r.files.length > 0) ? `<span style="font-size: 11px; opacity: 0.85; display: inline-flex; align-items: center;">${ICONS.folder} ${r.files.length} ${r.files.length === 1 ? 'file' : 'files'}</span>` : '';

    let seedersHtml = '';
    let leechersHtml = '';
    if (r.seeders !== undefined && r.seeders >= 0) {
      const seedColor = r.seeders > 0 ? 'var(--adw-success)' : 'var(--adw-dim-label)';
      seedersHtml = `<span style="color: ${seedColor}; font-weight: 600;">${ICONS.arrowUp}${r.seeders} seeds</span>`;
      if (r.leechers !== undefined && r.leechers >= 0) {
        leechersHtml = `<span>${ICONS.arrowDown}${r.leechers} peers</span>`;
      }
    } else {
      if (r.provider_type === 'soulseek') {
        seedersHtml = `<span class="health-badge" style="background: rgba(255, 71, 87, 0.15); color: #ff6b81; border: 1px solid rgba(255, 71, 87, 0.35); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500;" title="Lossless P2P Music Audio">${ICONS.dot}P2P Audio</span><span class="seed-probe-btn" style="color: var(--adw-dim-label); opacity: 0.85; cursor: pointer; text-decoration: underline dotted; margin-left: 4px;" title="Probe live BitTorrent swarm & WebSeeds" onclick="scrapeSwarmCard(${idx}, event)">${ICONS.arrowUp}? seeds</span>`;
      } else if (r.provider_type === 'documents') {
        seedersHtml = `<span class="health-badge" style="background: rgba(46, 213, 115, 0.15); color: #2ed573; border: 1px solid rgba(46, 213, 115, 0.35); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500;" title="Digital Library & Document Archive">${ICONS.dot}Library</span><span class="seed-probe-btn" style="color: var(--adw-dim-label); opacity: 0.85; cursor: pointer; text-decoration: underline dotted; margin-left: 4px;" title="Probe live swarm peers" onclick="scrapeSwarmCard(${idx}, event)">${ICONS.arrowUp}? seeds</span>`;
      } else if (r.provider_type === 'archiveorg') {
        seedersHtml = `<span class="health-badge" style="background: rgba(229, 165, 10, 0.15); color: var(--adw-warning); border: 1px solid rgba(229, 165, 10, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500;" title="Archive.org Direct WebSeed & Swarm">${ICONS.dot}WebSeed</span><span class="seed-probe-btn" style="color: var(--adw-dim-label); opacity: 0.85; cursor: pointer; text-decoration: underline dotted; margin-left: 4px;" title="Probe live swarm peers" onclick="scrapeSwarmCard(${idx}, event)">${ICONS.arrowUp}? seeds</span>`;
      } else {
        seedersHtml = `<span class="seed-probe-btn" style="color: var(--adw-dim-label); opacity: 0.85; cursor: pointer; text-decoration: underline dotted;" title="Swarm health unknown. Click to probe live swarm." onclick="scrapeSwarmCard(${idx}, event)">${ICONS.arrowUp}? seeds</span>`;
      }
    }

    let healthBadge = '';
    if (r.health) {
      if (r.health.status === 'periodic') {
        const titleTooltip = escapeHtml(`Historical Swarm Pattern: ${r.health.description || ''}`);
        const peakText = r.health.peak_window ? `~${r.health.peak_window}` : 'Periodic';
        healthBadge = `<span class="health-badge health-periodic" style="background: rgba(230, 162, 60, 0.15); color: #e6a23c; border: 1px solid rgba(230, 162, 60, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500; display: inline-flex; align-items: center;" title="${titleTooltip}">${ICONS.dot}${escapeHtml(peakText)}</span>`;
      } else if (r.health.status === 'dormant') {
        const titleTooltip = escapeHtml(`Swarm History: ${r.health.description || 'No active seeders recorded in checks'}`);
        healthBadge = `<span class="health-badge health-dormant" style="background: rgba(245, 108, 108, 0.15); color: #f56c6c; border: 1px solid rgba(245, 108, 108, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500; display: inline-flex; align-items: center;" title="${titleTooltip}">${ICONS.dot}Dormant</span>`;
      } else if (r.health.status === 'active') {
        healthBadge = `<span class="health-badge health-active" style="background: rgba(103, 194, 58, 0.15); color: #67c23a; border: 1px solid rgba(103, 194, 58, 0.3); padding: 2px 7px; border-radius: 10px; font-size: 11px; font-weight: 500; display: inline-flex; align-items: center;" title="Live seeders active right now">${ICONS.dot}Active</span>`;
      }
    }

    // Directory path breadcrumbs
    const pathBreadcrumbHtml = generatePathBreadcrumbsHtml(r, idx);

    const cardAria = `Search result: ${escapeHtml(r.title)}, ${formatBytes(r.size_bytes)}, ${r.provider || ''}`;
    return `
      <div class="search-card" tabindex="0" role="region" aria-label="${cardAria}" onkeydown="handleSearchCardKeydown(event, ${idx})">
        <div class="search-info">
          <div class="search-title" title="${escapeHtml(r.title)}">${escapeHtml(r.title)}</div>
          ${pathBreadcrumbHtml}
          <div class="search-sub" style="margin-top: 4px;">
            <span>${ICONS.package}${formatBytes(r.size_bytes)}</span>
            ${seedersHtml}
            ${leechersHtml}
            <span class="provider-badge ${tagClass}">${escapeHtml(r.provider)}</span>
            ${healthBadge}
            ${fileCountBadge}
            ${scoreText}
          </div>
        </div>
        <div style="display: flex; gap: 6px; align-items: center; flex-shrink: 0;">
          <button class="btn" title="Inspect files inside this entry" aria-label="Inspect files for ${escapeHtml(r.title)}" onclick="openInspectModal(${idx})">
            <span class="emoji-face" style="margin-right: 4px;">📦</span>
            <span>Files</span>
          </button>
          <button class="btn btn-icon" title="Copy Magnet / Link" aria-label="Copy link for ${escapeHtml(r.title)}" onclick="copyToClipboard('${encodeURI(r.magnet_uri)}', this)">${ICONS.magnet}</button>
          <button class="btn btn-primary" aria-label="Download ${escapeHtml(r.title)}" onclick="downloadFromSearch('${encodeURIComponent(r.magnet_uri)}', this)">
            ${ICONS.download}
            <span>Download</span>
          </button>
        </div>
      </div>
    `;
  }).join('');

  container.innerHTML = bannerHtml + cardsHtml;
}

async function scrapeSwarmCard(idx, event) {
  if (event) {
    event.stopPropagation();
  }
  const result = rawSearchResults[idx];
  if (!result) return;

  const targetEl = event ? event.currentTarget : null;
  if (targetEl) {
    targetEl.innerHTML = `${ICONS.clock} probing...`;
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
    }
  }
  renderSearchResults();
}

async function openInspectModal(idx) {
  const result = rawSearchResults[idx];
  if (!result) return;

  const modal = document.getElementById('modal-inspect');
  const titleEl = document.getElementById('inspect-title');
  const sizeEl = document.getElementById('inspect-size');
  const countEl = document.getElementById('inspect-file-count');
  const hashEl = document.getElementById('inspect-hash');
  const filesContainer = document.getElementById('inspect-files-container');
  const filterInput = document.getElementById('inspect-file-filter');
  if (filterInput) filterInput.value = '';
  inspectCurrentFilter = '';
  saveFocusAndOpen('modal-inspect', '#inspect-file-filter');

  titleEl.textContent = result.title || 'Unknown Torrent';
  sizeEl.textContent = `Total Size: ${formatBytes(result.size_bytes)}`;
  countEl.textContent = 'Files: ...';
  hashEl.textContent = `Hash: ${result.info_hash || '—'}`;
  hashEl.dataset.hash = result.info_hash || '';

  currentInspectData = result;

  // If the search result already contains files list
  if (result.files && result.files.length > 0) {
    currentInspectFiles = result.files.map((f, i) => ({
      index: f.index !== undefined ? f.index : i,
      path: f.path || f,
      length: f.size_bytes !== undefined ? f.size_bytes : (f.length || 0)
    }));
    inspectSelectedIndices = new Set(currentInspectFiles.map(f => f.index));
    renderInspectFiles(currentInspectFiles);
    countEl.textContent = `Files: ${currentInspectFiles.length}`;
    updateInspectSelectionSummary();
    return;
  }

  // Otherwise, query live BEP 9 metadata / DHT inspect endpoint
  filesContainer.innerHTML = `
    <div style="text-align: center; padding: 40px; color: var(--adw-dim-label);">
      <div style="margin-bottom: 8px;">${ICONS.clock}</div>
      <div style="font-weight: 600; margin-bottom: 4px; color: var(--adw-fg-color);">Resolving torrent metadata...</div>
      <div style="font-size: 11.5px;">Fetching file directory structure from DHT swarm peers & WebSeeds</div>
    </div>
  `;

  try {
    const res = await fetch(`/api/torrents/inspect?hash=${encodeURIComponent(result.info_hash)}&magnet=${encodeURIComponent(result.magnet_uri)}`);
    if (!res.ok) {
      throw new Error("Metadata resolution timed out. You can still start the download directly.");
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

    inspectSelectedIndices = new Set(currentInspectFiles.map(f => f.index));

    // Cache files on the search result item so subsequent inspect clicks are instant!
    result.files = currentInspectFiles;

    renderInspectFiles(currentInspectFiles);
    updateInspectSelectionSummary();
    // Refresh search results list to reflect resolved title and file count
    renderSearchResults();
  } catch (err) {
    currentInspectFiles = [{
      index: 0,
      path: result.title,
      length: result.size_bytes || 0
    }];
    inspectSelectedIndices = new Set([0]);
    filesContainer.innerHTML = `
      <div style="text-align: center; padding: 35px 20px; color: var(--adw-dim-label);">
        <div style="margin-bottom: 6px; color: var(--adw-warning);">${ICONS.alert}</div>
        <div style="font-size: 13px; font-weight: 600; color: var(--adw-fg-color); margin-bottom: 4px;">Direct Swarm / Metadata Lookup</div>
        <div style="font-size: 12px; margin-bottom: 12px;">${escapeHtml(err.message)}</div>
        <div style="font-size: 11.5px; opacity: 0.8;">Single package payload: <strong style="color: var(--adw-fg-color);">${escapeHtml(result.title)}</strong> (${formatBytes(result.size_bytes)})</div>
      </div>
    `;
    countEl.textContent = 'Files: 1';
    updateInspectSelectionSummary();
  }
}

function getIconForFile(path) {
  const ext = (path || '').split('.').pop().toLowerCase();
  if (['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'ts'].includes(ext)) return ICONS.fileVideo;
  if (['mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'opus', 'wma', 'alac', 'aiff'].includes(ext)) return ICONS.fileAudio;
  if (['zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'iso'].includes(ext)) return ICONS.fileArchive;
  if (['pdf', 'epub', 'mobi', 'doc', 'docx', 'txt', 'rtf', 'cbr', 'cbz'].includes(ext)) return ICONS.file;
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp'].includes(ext)) return ICONS.fileImage;
  if (['exe', 'msi', 'deb', 'rpm', 'apk', 'dmg', 'AppImage'].includes(ext)) return ICONS.fileCode;
  return ICONS.file;
}

function selectOnlyInspectFolder(dirPath, event) {
  if (event) event.stopPropagation();
  inspectSelectedIndices.clear();
  currentInspectFiles.forEach(f => {
    const rawPath = f.path || '';
    const lastSlash = Math.max(rawPath.lastIndexOf('/'), rawPath.lastIndexOf('\\'));
    const dir = lastSlash !== -1 ? rawPath.substring(0, lastSlash) : '';
    if (dir === dirPath) {
      inspectSelectedIndices.add(f.index);
    }
  });
  renderInspectFiles(currentInspectFiles);
  updateInspectSelectionSummary();
}

async function downloadInspectFolder(dirPath, event) {
  if (event) event.stopPropagation();
  selectOnlyInspectFolder(dirPath);
  const btn = document.getElementById('inspect-download-btn');
  if (btn) {
    startDownloadFromInspect(btn, true);
  }
}

function renderInspectFiles(files) {
  const container = document.getElementById('inspect-files-container');
  if (!container) return;

  if (!files || files.length === 0) {
    container.innerHTML = '<div style="text-align: center; color: var(--adw-dim-label); padding: 30px;">No files found.</div>';
    return;
  }

  // Group files by directory
  const groups = new Map();
  files.forEach(f => {
    const rawPath = f.path || '';
    const lastSlash = rawPath.lastIndexOf('/');
    const lastBackslash = rawPath.lastIndexOf('\\');
    const splitIdx = Math.max(lastSlash, lastBackslash);
    let dir = '';
    let name = rawPath;
    if (splitIdx !== -1) {
      dir = rawPath.substring(0, splitIdx);
      name = rawPath.substring(splitIdx + 1);
    }
    if (!groups.has(dir)) {
      groups.set(dir, []);
    }
    groups.get(dir).push({
      ...f,
      basename: name,
      dir: dir
    });
  });

  const hasMultipleDirs = groups.size > 1 || (!groups.has('') && groups.size > 0);

  let html = '';
  groups.forEach((groupFiles, dirPath) => {
    const dirTitle = dirPath || 'Root Directory';
    const allGroupChecked = groupFiles.every(f => inspectSelectedIndices.has(f.index));
    const groupTotalBytes = groupFiles.reduce((acc, f) => acc + (f.length || 0), 0);

    html += `
      <div class="inspect-dir-group" style="border-bottom: 1px solid rgba(128,128,128,0.12);">
        ${hasMultipleDirs ? `
          <div style="display: flex; align-items: center; justify-content: space-between; padding: 8px 12px; background: rgba(255,255,255,0.03); border-bottom: 1px solid rgba(128,128,128,0.08); font-size: 12px; font-weight: 600; flex-wrap: wrap; gap: 6px;">
            <label style="display: flex; align-items: center; gap: 8px; cursor: pointer; min-width: 0; flex: 1;">
              <input type="checkbox" class="inspect-dir-checkbox" data-dir="${escapeHtml(dirPath)}" ${allGroupChecked ? 'checked' : ''} onchange="toggleInspectDirGroup('${escapeHtml(dirPath)}', this.checked)">
              ${ICONS.folder}
              <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap;" title="${escapeHtml(dirTitle)}">${escapeHtml(dirTitle)}</span>
            </label>
            <div style="display: flex; align-items: center; gap: 6px;">
              <span style="font-size: 11px; color: var(--adw-dim-label); font-weight: normal; margin-right: 4px; white-space: nowrap;">
                ${groupFiles.length} file${groupFiles.length !== 1 ? 's' : ''} • ${formatBytes(groupTotalBytes)}
              </span>
              <button type="button" class="btn btn-sm" style="font-size: 10.5px; padding: 2px 7px;" onclick="selectOnlyInspectFolder('${escapeHtml(dirPath)}', event)" title="Select only files in this directory">✔️ Select</button>
              <button type="button" class="btn btn-sm btn-primary" style="font-size: 10.5px; padding: 2px 8px;" onclick="downloadInspectFolder('${escapeHtml(dirPath)}', event)" title="Download all files in this directory immediately">📥 Download Folder</button>
            </div>
          </div>
        ` : ''}
        <div class="inspect-dir-files">
          ${groupFiles.map(f => {
            const isChecked = inspectSelectedIndices.has(f.index);
            const sizeStr = f.length > 0 ? formatBytes(f.length) : '';
            const icon = getIconForFile(f.path);
            return `
              <div style="display: flex; justify-content: space-between; align-items: center; padding: 6px 12px ${hasMultipleDirs ? '6px 28px' : '6px 12px'}; border-bottom: 1px solid rgba(128,128,128,0.06); font-size: 12px;">
                <label style="display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; cursor: pointer; padding-right: 12px;">
                  <input type="checkbox" class="inspect-file-checkbox" data-index="${f.index}" ${isChecked ? 'checked' : ''} onchange="toggleInspectFile(${f.index}, this.checked)">
                  ${icon}
                  <span style="white-space: nowrap; overflow: hidden; text-overflow: ellipsis;" title="${escapeHtml(f.path)}">${escapeHtml(f.basename || f.path)}</span>
                </label>
                <div style="color: var(--adw-dim-label); font-weight: 500; font-size: 11px; white-space: nowrap;">${sizeStr}</div>
              </div>
            `;
          }).join('')}
        </div>
      </div>
    `;
  });

  container.innerHTML = html;
}

function filterInspectFiles(query) {
  inspectCurrentFilter = (query || '').toLowerCase().trim();
  if (!inspectCurrentFilter) {
    renderInspectFiles(currentInspectFiles);
    return;
  }
  const filtered = currentInspectFiles.filter(f => f.path.toLowerCase().includes(inspectCurrentFilter));
  renderInspectFiles(filtered);
}

function toggleInspectFile(index, checked) {
  if (checked) {
    inspectSelectedIndices.add(index);
  } else {
    inspectSelectedIndices.delete(index);
  }
  updateInspectSelectionSummary();
  // Refresh directory header checkboxes
  document.querySelectorAll('.inspect-dir-group').forEach(groupEl => {
    const dirCheckbox = groupEl.querySelector('.inspect-dir-checkbox');
    if (!dirCheckbox) return;
    const fileCheckboxes = Array.from(groupEl.querySelectorAll('.inspect-file-checkbox'));
    if (fileCheckboxes.length > 0) {
      dirCheckbox.checked = fileCheckboxes.every(cb => cb.checked);
      dirCheckbox.indeterminate = !dirCheckbox.checked && fileCheckboxes.some(cb => cb.checked);
    }
  });
}

function toggleInspectDirGroup(dirPath, checked) {
  currentInspectFiles.forEach(f => {
    const rawPath = f.path || '';
    const lastSlash = rawPath.lastIndexOf('/');
    const lastBackslash = rawPath.lastIndexOf('\\');
    const splitIdx = Math.max(lastSlash, lastBackslash);
    const dir = splitIdx !== -1 ? rawPath.substring(0, splitIdx) : '';
    if (dir === dirPath) {
      if (checked) {
        inspectSelectedIndices.add(f.index);
      } else {
        inspectSelectedIndices.delete(f.index);
      }
    }
  });
  if (inspectCurrentFilter) {
    const filtered = currentInspectFiles.filter(f => f.path.toLowerCase().includes(inspectCurrentFilter));
    renderInspectFiles(filtered);
  } else {
    renderInspectFiles(currentInspectFiles);
  }
  updateInspectSelectionSummary();
}

function inspectSelectAll(select) {
  if (select) {
    inspectSelectedIndices = new Set(currentInspectFiles.map(f => f.index));
  } else {
    inspectSelectedIndices.clear();
  }
  if (inspectCurrentFilter) {
    const filtered = currentInspectFiles.filter(f => f.path.toLowerCase().includes(inspectCurrentFilter));
    renderInspectFiles(filtered);
  } else {
    renderInspectFiles(currentInspectFiles);
  }
  updateInspectSelectionSummary();
}

function inspectSelectByType(type) {
  inspectSelectedIndices.clear();
  const audioExts = ['mp3', 'flac', 'wav', 'aac', 'ogg', 'm4a', 'opus', 'wma', 'alac', 'aiff'];
  const videoExts = ['mp4', 'mkv', 'avi', 'mov', 'wmv', 'flv', 'webm', 'm4v', 'ts'];
  const docExts = ['pdf', 'epub', 'mobi', 'doc', 'docx', 'txt', 'rtf', 'cbr', 'cbz'];

  currentInspectFiles.forEach(f => {
    const ext = (f.path || '').split('.').pop().toLowerCase();
    if (type === 'audio' && audioExts.includes(ext)) {
      inspectSelectedIndices.add(f.index);
    } else if (type === 'video' && videoExts.includes(ext)) {
      inspectSelectedIndices.add(f.index);
    } else if (type === 'docs' && docExts.includes(ext)) {
      inspectSelectedIndices.add(f.index);
    }
  });

  if (inspectCurrentFilter) {
    const filtered = currentInspectFiles.filter(f => f.path.toLowerCase().includes(inspectCurrentFilter));
    renderInspectFiles(filtered);
  } else {
    renderInspectFiles(currentInspectFiles);
  }
  updateInspectSelectionSummary();
}

function updateInspectSelectionSummary() {
  const summaryEl = document.getElementById('inspect-selection-summary');
  const downloadBtnText = document.getElementById('inspect-download-btn-text');
  let totalSelectedBytes = 0;
  let totalSelectedCount = 0;

  currentInspectFiles.forEach(f => {
    if (inspectSelectedIndices.has(f.index)) {
      totalSelectedBytes += f.length || 0;
      totalSelectedCount++;
    }
  });

  if (summaryEl) {
    summaryEl.textContent = `Selected: ${totalSelectedCount} / ${currentInspectFiles.length} (${formatBytes(totalSelectedBytes)})`;
  }
  if (downloadBtnText) {
    downloadBtnText.textContent = (totalSelectedCount === currentInspectFiles.length || totalSelectedCount === 0)
      ? 'Download All'
      : `Download Selected (${totalSelectedCount})`;
  }
}

function closeInspectModal() {
  restoreFocusAndClose('modal-inspect');
  currentInspectData = null;
  currentInspectFiles = [];
  inspectSelectedIndices.clear();
  inspectCurrentFilter = '';
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

async function startDownloadFromInspect(btn, onlySelected) {
  if (!currentInspectData || !currentInspectData.magnet_uri) return;
  const uri = currentInspectData.magnet_uri;

  let selectedFiles = null;
  if (onlySelected && inspectSelectedIndices.size > 0 && inspectSelectedIndices.size < currentInspectFiles.length) {
    selectedFiles = Array.from(inspectSelectedIndices);
  }

  closeInspectModal();
  await downloadFromSearchWithSelection(uri, selectedFiles, btn);
}

async function downloadFromSearchWithSelection(uri, selectedFiles, btn) {
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `<span style="opacity: 0.7;">Adding...</span>`;
  }

  const payload = { url: uri };
  if (selectedFiles && selectedFiles.length > 0) {
    payload.selected_files = selectedFiles;
  }

  const label = (selectedFiles && selectedFiles.length > 0)
    ? `Adding ${selectedFiles.length} selected files...`
    : "Adding download...";
  showToast(label, "info", 2000);

  try {
    const res = await fetch('/api/torrents/add', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const data = await res.json();
    if (data.status === 'ok') {
      const successMsg = (selectedFiles && selectedFiles.length > 0)
        ? `Added ${selectedFiles.length} tracks/files to transfers queue!`
        : "Transfer started!";
      showToast(successMsg, "info", 3000);
      // Stacking: refresh transfers in background without forcing a tab switch
      fetchTorrents();
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

async function downloadFromSearch(encodedURI, btn) {
  const uri = decodeURIComponent(encodedURI);
  await downloadFromSearchWithSelection(uri, null, btn);
}

// Add Modal
function openAddModal() {
  saveFocusAndOpen('modal-add', '#add-magnet-input');
}

function closeAddModal() {
  restoreFocusAndClose('modal-add');
  document.getElementById('add-magnet-input').value = '';
  document.getElementById('add-file-input').value = '';
}

async function submitAddTorrent() {
  const inputVal = document.getElementById('add-magnet-input').value.trim();
  const fileInput = document.getElementById('add-file-input');

  if (fileInput.files.length > 0) {
    const file = fileInput.files[0];
    closeAddModal();
    showToast("Adding .torrent file...", "info", 1800);
    try {
      const buffer = await file.arrayBuffer();
      const res = await fetch('/api/torrents/add', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/x-bittorrent',
          'X-Torrent-Name': encodeURIComponent(file.name || 'upload.torrent')
        },
        body: buffer
      });
      const data = await res.json();
      if (data.status === 'ok') {
        showToast("Transfer added!", "info", 2500);
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
        showToast("Transfer started!", "info", 2500);
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

    const gerEl = document.getElementById('cfg-germany-mode');
    if (gerEl) gerEl.checked = activeConfig.germany_mode === true;

    const dnsEl = document.getElementById('cfg-fallback-dns');
    if (dnsEl) dnsEl.value = (activeConfig.fallback_dns || ['8.8.8.8:53', '1.1.1.1:53', '8.8.4.4:53', '9.9.9.9:53']).join(', ');

    renderProvidersList();
    switchSettingsTab('providers');
    saveFocusAndOpen('modal-settings', '#stab-providers');
  } catch (err) {
    console.error('Error opening settings modal:', err);
    showToast('Failed to open settings: ' + err.message, 'error', 3500);
  }
}

function closeSettingsModal() {
  restoreFocusAndClose('modal-settings');
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
          <button class="btn" style="padding: 3px 8px; font-size: 11px; display: inline-flex; align-items: center;" id="btn-test-prov-${idx}" onclick="testSingleProvider(${idx}, this)" title="Test live latency and results">${ICONS.zap} Test</button>
          <button class="btn btn-icon" style="padding: 3px 6px; font-size: 11px;" onclick="openEditProviderModal(${idx})" title="Edit provider details">${ICONS.edit}</button>
          <button class="btn btn-icon" style="padding: 3px 6px; font-size: 11px; color: var(--adw-error);" onclick="deleteProvider(${idx})" title="Remove provider">${ICONS.trash}</button>
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
      showToast(`${p.name}: ${data.count} hits (${data.duration_ms}ms)`, "success", 3000);
      btnEl.innerHTML = `${ICONS.check} ${data.count} (${data.duration_ms}ms)`;
    } else {
      showToast(`${p.name} failed: ${data.error || 'Timeout'}`, "warning", 3500);
      btnEl.innerHTML = `${ICONS.x} Fail`;
    }
  } catch (err) {
    showToast(`Error testing ${p.name}: ${err.message}`, "error", 3000);
    btnEl.innerHTML = `${ICONS.x} Error`;
  } finally {
    setTimeout(() => {
      btnEl.innerHTML = originalText;
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
  saveFocusAndOpen('modal-provider-edit', '#ped-name');
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
  saveFocusAndOpen('modal-provider-edit', '#ped-name');
}

function closeProviderEditModal() {
  restoreFocusAndClose('modal-provider-edit');
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
      feedback.textContent = `Success! Retrieved ${data.count} torrents in ${data.duration_ms}ms.`;
    } else {
      feedback.style.background = 'rgba(224, 27, 36, 0.2)';
      feedback.style.color = '#ff7b63';
      feedback.textContent = `Test Failed: ${data.error || 'Could not parse response'}`;
    }
  } catch (err) {
    feedback.style.background = 'rgba(224, 27, 36, 0.2)';
    feedback.style.color = '#ff7b63';
    feedback.textContent = `Network Error: ${err.message}`;
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
      activeConfig.germany_mode = document.getElementById('cfg-germany-mode').checked;

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
      showToast(`TorrentsCSV pre-seed started! Current local cache: ${data.current_size || 0} torrents.`, "success", 4000);
    }
  } catch (err) {
    showToast("Pre-seeding error: " + err.message, "error", 3000);
  }
}

async function toggleGermanyMode(enabled) {
  try {
    const res = await fetch('/api/config/germany-mode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: enabled })
    });
    const data = await res.json();
    if (activeConfig) {
      activeConfig.germany_mode = data.germany_mode;
    }
    const gerEl = document.getElementById('cfg-germany-mode');
    if (gerEl) gerEl.checked = data.germany_mode;
    const gBadge = document.getElementById('germany-mode-badge');
    if (gBadge) gBadge.style.display = data.germany_mode ? 'inline-flex' : 'none';
    showToast(data.germany_mode ? "🇩🇪 Germany Safe Mode: Uploads strictly disabled (0 B/s)" : "Germany Mode Disabled: Normal P2P seeding enabled", "info", 3000);
  } catch (err) {
    showToast("Failed to toggle Germany mode: " + err.message, "error", 3000);
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
  saveFocusAndOpen('modal-file-browser', '#btn-browser-select');
  await loadBrowserDir(initialPath);
}

function closeFileBrowser() {
  restoreFocusAndClose('modal-file-browser');
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
          <span class="emoji-face" style="font-size: 14px;">${item.is_dir ? '📁' : '📄'}</span>
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
    return;
  }
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'q') {
    e.preventDefault();
    windowClose();
    return;
  }

  // Escape: Close topmost open modal and return focus
  if (e.key === 'Escape') {
    const openModals = [
      'modal-file-browser', 'modal-provider-edit', 'modal-inspect',
      'modal-details', 'modal-delete', 'modal-send', 'modal-settings', 'modal-add'
    ];
    for (const mId of openModals) {
      const el = document.getElementById(mId);
      if (el && el.classList.contains('open')) {
        e.preventDefault();
        if (mId === 'modal-file-browser') closeFileBrowser();
        else if (mId === 'modal-provider-edit') closeProviderEditModal();
        else if (mId === 'modal-inspect') closeInspectModal();
        else if (mId === 'modal-details') closeDetailsModal();
        else if (mId === 'modal-delete') closeDeleteModal();
        else if (mId === 'modal-send') closeSendModal();
        else if (mId === 'modal-settings') closeSettingsModal();
        else if (mId === 'modal-add') closeAddModal();
        return;
      }
    }
  }

  const isInput = ['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement ? document.activeElement.tagName : '');

  // Shortcuts when not typing in text fields
  if (!isInput) {
    if (e.key === '1' || (e.altKey && e.key === '1') || ((e.ctrlKey || e.metaKey) && e.key === '1')) {
      e.preventDefault();
      switchMainView('torrents');
    } else if (e.key === '2' || (e.altKey && e.key === '2') || ((e.ctrlKey || e.metaKey) && e.key === '2')) {
      e.preventDefault();
      switchMainView('search');
    } else if (e.key === '/') {
      e.preventDefault();
      switchMainView('search');
      const sIn = document.getElementById('search-input');
      if (sIn) sIn.focus();
    }
  }

  // Ctrl+N / Ctrl+O: Open Add Modal
  if ((e.ctrlKey || e.metaKey) && (e.key.toLowerCase() === 'n' || e.key.toLowerCase() === 'o')) {
    e.preventDefault();
    openAddModal();
  }
  // Ctrl+S: Open Send/Create Torrent Modal
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
    e.preventDefault();
    openSendModal();
  }
  // Ctrl+,: Open Preferences / Settings Modal
  if ((e.ctrlKey || e.metaKey) && e.key === ',') {
    e.preventDefault();
    openSettingsModal();
  }
  // Ctrl+F: Focus search
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f') {
    e.preventDefault();
    switchMainView('search');
    const sIn = document.getElementById('search-input');
    if (sIn) sIn.focus();
  }
});

// Initialize on page load
window.addEventListener('DOMContentLoaded', () => {
  initEventStream();
  // Immediate initial load so UI populates instantly without waiting for SSE tick
  fetch('/api/torrents').then(r => r.json()).then(data => {
    if (data && Array.isArray(data)) {
      torrentsData = data;
      if (currentView === 'torrents') renderTorrents();
    }
  }).catch(() => {});
  fetch('/api/stats').then(r => r.json()).then(stats => {
    if (stats) renderGlobalStats(stats);
  }).catch(() => {});
});

// Drag and Drop support
let dragCounter = 0;

window.addEventListener('dragenter', (e) => {
  e.preventDefault();
  dragCounter++;
  const overlay = document.getElementById('drag-drop-overlay');
  if (overlay) overlay.classList.add('active');
});

window.addEventListener('dragleave', (e) => {
  e.preventDefault();
  dragCounter--;
  if (dragCounter <= 0) {
    dragCounter = 0;
    const overlay = document.getElementById('drag-drop-overlay');
    if (overlay) overlay.classList.remove('active');
  }
});

window.addEventListener('dragover', (e) => {
  e.preventDefault();
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy';
});

window.addEventListener('drop', async (e) => {
  e.preventDefault();
  dragCounter = 0;
  const overlay = document.getElementById('drag-drop-overlay');
  if (overlay) overlay.classList.remove('active');

  const files = e.dataTransfer ? e.dataTransfer.files : null;
  if (files && files.length > 0) {
    for (const file of files) {
      if (file.name.endsWith('.torrent') || file.type === 'application/x-bittorrent' || file.size > 0) {
        showToast(`Adding ${file.name}...`, "info", 1800);
        try {
          const buffer = await file.arrayBuffer();
          const res = await fetch('/api/torrents/add', {
            method: 'POST',
            headers: {
              'Content-Type': 'application/x-bittorrent',
              'X-Torrent-Name': encodeURIComponent(file.name)
            },
            body: buffer
          });
          const data = await res.json();
          if (data.status === 'ok') {
            showToast(`Added: ${file.name}`, "info", 2500);
            switchMainView('torrents');
          } else {
            showToast(`Failed to add ${file.name}: ` + (data.error || 'Unknown error'), "error", 4000);
          }
        } catch (err) {
          showToast(`Error adding ${file.name}: ` + err.message, "error", 4000);
        }
      }
    }
    return;
  }

  // Check for dropped URL / magnet link
  const text = e.dataTransfer ? (e.dataTransfer.getData('text/uri-list') || e.dataTransfer.getData('text/plain') || '').trim() : '';
  if (text) {
    const lines = text.split('\n').map(l => l.trim()).filter(Boolean);
    for (const line of lines) {
      if (line.startsWith('#')) continue;
      showToast("Adding dropped link...", "info", 1800);
      try {
        const res = await fetch('/api/torrents/add', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: line })
        });
        const data = await res.json();
        if (data.status === 'ok') {
          showToast("Transfer added!", "info", 2500);
          switchMainView('torrents');
        } else {
          showToast("Failed: " + (data.error || 'Unknown error'), "error", 4000);
        }
      } catch (err) {
        showToast("Request error: " + err.message, "error", 4000);
      }
    }
  }
});

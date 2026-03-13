(function () {
    'use strict';
    // Chat UI runs in an IIFE so state (serverUrl, aguiPasswordForSession, etc.) is not on the global object.
    // Only the handlers required by chat.html and dynamic onclick attributes are exposed on window.

    // ── IndexedDB Persistence Layer ──
    class GenieDB {
        constructor() {
            this.dbName = 'genie-chat';
            this.version = 1;
            this.db = null;
        }

        open() {
            return new Promise((resolve, reject) => {
                const req = indexedDB.open(this.dbName, this.version);
                req.onupgradeneeded = (e) => {
                    const db = e.target.result;
                    if (!db.objectStoreNames.contains('conversations')) {
                        const store = db.createObjectStore('conversations', { keyPath: 'threadId' });
                        store.createIndex('serverUrl', 'serverUrl', { unique: false });
                        store.createIndex('updatedAt', 'updatedAt', { unique: false });
                    }
                };
                req.onsuccess = (e) => { this.db = e.target.result; resolve(this.db); };
                req.onerror = (e) => reject(e.target.error);
            });
        }

        async saveConversation(conv) {
            if (!this.db) await this.open();
            return new Promise((resolve, reject) => {
                const tx = this.db.transaction('conversations', 'readwrite');
                tx.objectStore('conversations').put(conv);
                tx.oncomplete = () => resolve();
                tx.onerror = (e) => reject(e.target.error);
            });
        }

        async getConversations(serverUrlFilter) {
            if (!this.db) await this.open();
            return new Promise((resolve, reject) => {
                const tx = this.db.transaction('conversations', 'readonly');
                const store = tx.objectStore('conversations');
                const req = store.getAll();
                req.onsuccess = () => {
                    let results = req.result || [];
                    if (serverUrlFilter) {
                        results = results.filter(c => c.serverUrl === serverUrlFilter);
                    }
                    results.sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0));
                    resolve(results);
                };
                req.onerror = (e) => reject(e.target.error);
            });
        }

        async getAllConversations() {
            if (!this.db) await this.open();
            return new Promise((resolve, reject) => {
                const tx = this.db.transaction('conversations', 'readonly');
                const req = tx.objectStore('conversations').getAll();
                req.onsuccess = () => {
                    const results = req.result || [];
                    results.sort((a, b) => (b.updatedAt || 0) - (a.updatedAt || 0));
                    resolve(results);
                };
                req.onerror = (e) => reject(e.target.error);
            });
        }

        async getConversation(threadId) {
            if (!this.db) await this.open();
            return new Promise((resolve, reject) => {
                const tx = this.db.transaction('conversations', 'readonly');
                const req = tx.objectStore('conversations').get(threadId);
                req.onsuccess = () => resolve(req.result || null);
                req.onerror = (e) => reject(e.target.error);
            });
        }

        async deleteConversation(threadId) {
            if (!this.db) await this.open();
            return new Promise((resolve, reject) => {
                const tx = this.db.transaction('conversations', 'readwrite');
                tx.objectStore('conversations').delete(threadId);
                tx.oncomplete = () => resolve();
                tx.onerror = (e) => reject(e.target.error);
            });
        }
    }

    const genieDB = new GenieDB();
    genieDB.open().catch(err => console.warn('IndexedDB init failed:', err));

    // Current conversation being built
    let currentConversation = null;

    // ── Segmentation: platform + userId + chatId (threadId) ──
    // Platform identifier for this UI channel.
    const PLATFORM_ID = 'agui:http';

    // Stable user ID persisted across sessions so the backend can
    // correlate the same human across multiple chats.
    function getOrCreateUserId() {
        const KEY = 'genie-user-id';
        let uid = localStorage.getItem(KEY);
        if (!uid) {
            uid = crypto.randomUUID();
            localStorage.setItem(KEY, uid);
        }
        return uid;
    }
    const userId = getOrCreateUserId();

    // ── State ──
    let serverUrl = '';
    let isConnected = false;
    // Agent display name from /health (defaults to "Genie" until connected).
    let agentDisplayName = 'Genie';
    // When the server requires AG-UI password, this is set after successful connect and sent with every request.
    let aguiPasswordForSession = '';
    // When the server requires JWT/OIDC auth, this is set after successful connect and sent as Authorization: Bearer.
    let aguiTokenForSession = '';
    let activeAuthTab = 'password';
    // Whether the server supports OAuth login (detected from 401 response).
    let serverSupportsOAuth = false;
    // The OAuth login URL on the server.
    let serverOAuthLoginUrl = '';

    function aguiAuthHeaders() {
        const h = {};
        if (aguiTokenForSession) {
            h['Authorization'] = 'Bearer ' + aguiTokenForSession;
        } else if (aguiPasswordForSession) {
            h['X-AGUI-Password'] = aguiPasswordForSession;
        }
        return h;
    }

    let isStreaming = false;
    // Reconnection state: when stream drops we try to reconnect with backoff.
    let isReconnecting = false;
    let reconnectAttempts = 0;
    const RECONNECT_MAX_ATTEMPTS = 5;
    const RECONNECT_BASE_MS = 1000;
    // Every chat session gets a fresh threadId so the LLM never
    // sees memory/context from a previous chat.
    let threadId = crypto.randomUUID();
    let runCounter = 0;
    let currentAssistantBubble = null;
    let currentAssistantContent = '';
    let currentMessageId = null;  // track active message ID
    let abortController = null;   // AbortController for cancelling streaming

    // Typing effect state
    let typingInterval = null;
    let displayedContentLength = 0;
    const TYPING_CHUNK_SIZE = 3;
    const TYPING_TICK_RATE = 10;

    // HITL nudge: show hint after several approvals in this run
    let hitlApprovalCount = 0;
    let hitlNudgeShown = false;

    // Batch-approve: track pending approvals and offer "Approve All" when
    // a tool sits unapproved for longer than BATCH_APPROVE_DELAY_MS.
    const BATCH_APPROVE_DELAY_MS = 8000;
    // Map<approvalId, { toolName: string, addedAt: number }>
    const pendingApprovals = new Map();
    let batchApproveBannerEl = null;

    // ── File Attachment State ──
    // pendingFiles holds File objects selected by the user before sending.
    let pendingFiles = [];
    // Tracks blob: URLs created for image previews so we can revoke them.
    let activeBlobURLs = [];
    // Tracks blob: URLs created to display user attachments in the chat history.
    let chatHistoryBlobURLs = [];
    const MAX_FILE_SIZE = 18 * 1024 * 1024; // 18 MB per file (base64 expands ~33%, keeping under typical 25 MB server limit)
    const ACCEPTED_TYPES = {
        image: ['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml', 'image/bmp'],
        audio: ['audio/mpeg', 'audio/wav', 'audio/ogg', 'audio/webm', 'audio/mp4', 'audio/aac', 'audio/flac'],
        video: ['video/mp4', 'video/webm', 'video/ogg', 'video/quicktime', 'video/x-msvideo'],
        document: ['application/pdf', 'application/msword', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'text/plain', 'text/csv', 'application/json', 'text/markdown', 'text/xml', 'application/xml', 'application/octet-stream'],
    };
    const ALL_ACCEPTED_MIMES = [].concat(ACCEPTED_TYPES.image, ACCEPTED_TYPES.audio, ACCEPTED_TYPES.video, ACCEPTED_TYPES.document);

    // ── DOM Refs ──
    const messagesEl = document.getElementById('chat-messages');
    const approvalsColumnEl = document.getElementById('approvals-column');
    const approvalsColumnListEl = document.getElementById('approvals-column-list');
    const inputEl = document.getElementById('chat-input');
    const sendBtn = document.getElementById('send-btn');
    const micBtn = document.getElementById('mic-btn');
    const connectionBadge = document.getElementById('connection-badge');
    const connectionLabel = document.getElementById('connection-label');
    const emptyState = document.getElementById('empty-state');
    const notificationPromptEl = document.getElementById('notification-prompt');
    const attachBtn = document.getElementById('attach-btn');
    const fileInputEl = document.getElementById('file-input');
    const attachmentPreviewEl = document.getElementById('attachment-preview');
    const dropOverlayEl = document.getElementById('drop-overlay');

    // ── File Attachment Helper Functions ──

    function handleFileSelect(event) {
        const files = event.target.files;
        if (!files || files.length === 0) return;
        addFiles(Array.from(files));
        // Reset input so the same file can be re-selected
        if (fileInputEl) fileInputEl.value = '';
    }

    function addFiles(files) {
        for (const file of files) {
            // Size check
            if (file.size > MAX_FILE_SIZE) {
                addErrorMessage(file.name + ' exceeds 18 MB limit. Please use a smaller file.');
                continue;
            }
            // MIME type allowlist: reject unsupported file types.
            // The <input accept=...> only covers file-picker; drag-and-drop
            // and paste bypass it, so we enforce here for all paths.
            if (file.type && ALL_ACCEPTED_MIMES.indexOf(file.type) === -1) {
                addErrorMessage(file.name + ' has unsupported type (' + (file.type || 'unknown') + '). Supported: images, audio, video, and documents.');
                continue;
            }
            // Reject files with no MIME type (unknown) as a safety measure.
            if (!file.type) {
                addErrorMessage(file.name + ' has no recognized file type. Please use a supported image, audio, video, or document file.');
                continue;
            }
            // Skip duplicate names
            if (pendingFiles.some(f => f.name === file.name && f.size === file.size)) continue;
            pendingFiles.push(file);
        }
        renderAttachmentPreview();
    }

    function removeAttachment(index) {
        pendingFiles.splice(index, 1);
        renderAttachmentPreview();
    }

    function clearPendingFiles() {
        // Revoke all tracked blob URLs to prevent memory leaks.
        for (var i = 0; i < activeBlobURLs.length; i++) {
            URL.revokeObjectURL(activeBlobURLs[i]);
        }
        activeBlobURLs = [];
        pendingFiles = [];
        renderAttachmentPreview();
    }

    function renderAttachmentPreview() {
        if (!attachmentPreviewEl) return;
        // Revoke previous blob URLs before re-rendering to prevent leaks.
        for (var i = 0; i < activeBlobURLs.length; i++) {
            URL.revokeObjectURL(activeBlobURLs[i]);
        }
        activeBlobURLs = [];
        attachmentPreviewEl.innerHTML = '';
        if (pendingFiles.length === 0) {
            attachmentPreviewEl.style.display = 'none';
            if (attachBtn) attachBtn.classList.remove('has-files');
            return;
        }
        attachmentPreviewEl.style.display = 'flex';
        if (attachBtn) attachBtn.classList.add('has-files');

        pendingFiles.forEach((file, idx) => {
            const chip = document.createElement('div');
            chip.className = 'attachment-chip';

            // Preview element (thumbnail for images, icon for others)
            if (file.type.startsWith('image/')) {
                const img = document.createElement('img');
                img.className = 'chip-preview';
                const blobUrl = URL.createObjectURL(file);
                activeBlobURLs.push(blobUrl);
                img.src = blobUrl;
                img.alt = file.name;
                chip.appendChild(img);
            } else {
                const iconDiv = document.createElement('div');
                iconDiv.className = 'chip-icon';
                if (file.type.startsWith('audio/')) {
                    iconDiv.classList.add('audio');
                    iconDiv.textContent = '🎵';
                } else if (file.type.startsWith('video/')) {
                    iconDiv.classList.add('video');
                    iconDiv.textContent = '🎬';
                } else {
                    iconDiv.classList.add('document');
                    iconDiv.textContent = '📄';
                }
                chip.appendChild(iconDiv);
            }

            // File info
            const info = document.createElement('div');
            info.className = 'chip-info';
            const nameSpan = document.createElement('span');
            nameSpan.className = 'chip-name';
            nameSpan.textContent = file.name;
            nameSpan.title = file.name;
            info.appendChild(nameSpan);
            const sizeSpan = document.createElement('span');
            sizeSpan.className = 'chip-size';
            sizeSpan.textContent = formatFileSize(file.size);
            info.appendChild(sizeSpan);
            chip.appendChild(info);

            // Remove button
            const removeBtn = document.createElement('button');
            removeBtn.className = 'chip-remove';
            removeBtn.textContent = '×';
            removeBtn.title = 'Remove ' + file.name;
            removeBtn.setAttribute('aria-label', 'Remove ' + file.name);
            removeBtn.onclick = () => genie.removeAttachment(idx);
            chip.appendChild(removeBtn);

            attachmentPreviewEl.appendChild(chip);
        });
    }

    function formatFileSize(bytes) {
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
    }

    function fileToDataUrl(file) {
        return new Promise((resolve, reject) => {
            const reader = new FileReader();
            reader.onload = () => resolve(reader.result);
            reader.onerror = () => reject(new Error('Failed to read ' + file.name));
            reader.readAsDataURL(file);
        });
    }

    // ── Drag & Drop ──
    let dragCounter = 0;

    document.addEventListener('dragenter', (e) => {
        e.preventDefault();
        if (!isConnected) return;
        dragCounter++;
        if (dragCounter === 1 && dropOverlayEl) {
            dropOverlayEl.style.display = 'flex';
        }
    });

    document.addEventListener('dragleave', (e) => {
        e.preventDefault();
        dragCounter--;
        if (dragCounter <= 0) {
            dragCounter = 0;
            if (dropOverlayEl) dropOverlayEl.style.display = 'none';
        }
    });

    document.addEventListener('dragover', (e) => {
        e.preventDefault();
    });

    document.addEventListener('drop', (e) => {
        e.preventDefault();
        dragCounter = 0;
        if (dropOverlayEl) dropOverlayEl.style.display = 'none';
        if (!isConnected) return;
        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length > 0) {
            addFiles(Array.from(files));
            inputEl.focus();
        }
    });

    // Also allow pasting images from clipboard
    document.addEventListener('paste', (e) => {
        if (!isConnected) return;
        const items = e.clipboardData && e.clipboardData.items;
        if (!items) return;
        const files = [];
        for (let i = 0; i < items.length; i++) {
            if (items[i].kind === 'file') {
                const file = items[i].getAsFile();
                if (file) files.push(file);
            }
        }
        if (files.length > 0) {
            addFiles(files);
        }
    });

    // ── Browser notifications (approval + updates) ──
    const NOTIFICATION_PROMPT_DISMISSED_KEY = 'genie-notification-prompt-dismissed';

    if ('serviceWorker' in navigator) {
        window.addEventListener('load', function () {
            navigator.serviceWorker.register('./sw.js').catch(function (err) {
                console.warn('Service worker registration failed:', err);
            });
        });

        navigator.serviceWorker.addEventListener('message', function (event) {
            const data = event.data;
            if (data && data.category === 'genie-action') {
                if (data.action === 'approve') {
                    if (window.genie && window.genie.resolveApproval) window.genie.resolveApproval(data.approvalId, 'approved');
                } else if (data.action === 'reject') {
                    if (window.genie && window.genie.resolveApproval) window.genie.resolveApproval(data.approvalId, 'rejected');
                } else if (data.action === 'recheck') {
                    if (window.genie && window.genie.revisitApproval) window.genie.revisitApproval(data.approvalId);
                }
            }
        });
    }

    function notificationsSupported() {
        return typeof Notification !== 'undefined';
    }

    function showNotification(title, body, tag, extraData) {
        if (!notificationsSupported() || Notification.permission !== 'granted') return;

        if ('serviceWorker' in navigator) {
            navigator.serviceWorker.ready.then(function (reg) {
                const options = {
                    body: body || '',
                    tag: tag || 'genie',
                    icon: '/favicon.ico',
                    data: extraData || {}
                };
                if (extraData && extraData.type === 'APPROVAL_REQUEST') {
                    options.requireInteraction = true;
                    // Max actions supported differs by OS/Browser, typically 2 or 3.
                    options.actions = [
                        { action: 'approve', title: '✅ Approve' },
                        { action: 'reject', title: '❌ Reject' },
                        { action: 'recheck', title: '🔄 Recheck' }
                    ];
                }
                reg.showNotification(title, options).then(function () {
                    setTimeout(function () {
                        reg.getNotifications({ tag: options.tag }).then(function (notifications) {
                            notifications.forEach(function (notification) {
                                notification.close();
                            });
                        }).catch(function (e) {
                            if (console && typeof console.debug === 'function') {
                                console.debug('getNotifications failed', e);
                            }
                        });
                    }, 60000);
                }).catch(function (e) {
                    fallbackNotification(title, body, tag);
                });
            }).catch(e => {
                fallbackNotification(title, body, tag);
            });
        } else {
            fallbackNotification(title, body, tag);
        }
    }

    function fallbackNotification(title, body, tag) {
        try {
            const n = new Notification(title, {
                body: body || '',
                tag: tag || 'genie',
                icon: '/favicon.ico'
            });
            n.onclick = () => { window.focus(); n.close(); };
            setTimeout(() => {
                try { n.close(); } catch (e) { /* ignore */ }
            }, 60000);
        } catch (e) {
            console.warn('Notification failed:', e);
        }
    }

    async function requestNotificationPermission() {
        if (!notificationsSupported()) return false;
        if (Notification.permission === 'granted') return true;
        if (Notification.permission === 'denied') return false;
        const perm = await Notification.requestPermission();
        return perm === 'granted';
    }

    function showNotificationPrompt() {
        if (!notificationPromptEl || !notificationsSupported()) return;
        if (Notification.permission !== 'default') return;
        if (localStorage.getItem(NOTIFICATION_PROMPT_DISMISSED_KEY) === '1') return;
        notificationPromptEl.style.display = 'flex';
    }

    function hideNotificationPrompt() {
        if (notificationPromptEl) notificationPromptEl.style.display = 'none';
    }

    async function enableNotifications() {
        const granted = await requestNotificationPermission();
        hideNotificationPrompt();
        if (granted) {
            showNotification('Notifications enabled', 'You\'ll be notified when ' + agentDisplayName + ' needs your approval or has a reply.', 'genie-setup');
        }
    }

    function dismissNotificationPrompt() {
        localStorage.setItem(NOTIFICATION_PROMPT_DISMISSED_KEY, '1');
        hideNotificationPrompt();
    }

    document.addEventListener('visibilitychange', function () {
        if (document.visibilityState === 'visible') {
            if (typeof navigator.clearAppBadge === 'function') {
                try { navigator.clearAppBadge(); } catch (e) { /* ignore */ }
            }
            if (isConnected && serverUrl && !isReconnecting) {
                tryReconnectOnce().then(function (ok) {
                    if (!ok) {
                        setConnected(false);
                        tryReconnectWithBackoff();
                    }
                });
            }
        }
    });

    function vibrateBrief() {
        if (localStorage.getItem('genie-vibrate') === '0') return;
        if (typeof navigator.vibrate !== 'function') return;
        try { navigator.vibrate(50); } catch (e) { /* ignore */ }
    }

    function toggleVibratePreference() {
        const current = localStorage.getItem('genie-vibrate');
        const next = current === '0' ? '1' : '0';
        localStorage.setItem('genie-vibrate', next);
        updateVibrateToggleLabel();
    }

    function updateVibrateToggleLabel() {
        const btn = document.getElementById('vibrate-toggle');
        if (!btn) return;
        const parent = btn.closest('p');
        if (typeof navigator.vibrate !== 'function' && parent) {
            parent.style.display = 'none';
            return;
        }
        if (parent) parent.style.display = '';
        const off = localStorage.getItem('genie-vibrate') === '0';
        btn.textContent = 'Vibrate on alerts: ' + (off ? 'Off' : 'On');
        btn.setAttribute('aria-label', off ? 'Vibrate on alerts is off; click to turn on' : 'Vibrate on alerts is on; click to turn off');
    }

    // ── Microphone / Speech Recognition ──
    const SpeechRecognitionAPI = window.SpeechRecognition || window.webkitSpeechRecognition;
    let speechRecognition = null;
    let isListening = false;
    let speechPrefix = '';
    let speechFinal = '';

    function toggleMic() {
        if (!micBtn || !SpeechRecognitionAPI || isStreaming || !isConnected) return;

        if (isListening) {
            stopListening();
            return;
        }

        try {
            speechPrefix = (inputEl.value || '').replace(/\s*\[listening…\]\s*$/, '').trim();
            speechFinal = '';
            speechRecognition = new SpeechRecognitionAPI();
            speechRecognition.continuous = true;
            speechRecognition.interimResults = true;
            speechRecognition.lang = document.documentElement.lang || 'en-US';

            speechRecognition.onresult = function (event) {
                let interim = '';
                for (let i = event.resultIndex; i < event.results.length; i++) {
                    const transcript = event.results[i][0].transcript;
                    if (event.results[i].isFinal) {
                        speechFinal += transcript;
                    } else {
                        interim = transcript;
                    }
                }
                const combined = [speechPrefix, speechFinal, interim].filter(Boolean).join(' ');
                inputEl.value = combined + (interim ? ' [listening…]' : '');
                autoResize(inputEl);
            };

            speechRecognition.onerror = function (event) {
                if (event.error === 'not-allowed' || event.error === 'service-not-allowed') {
                    addErrorMessage('Microphone access denied. Allow microphone in your browser to use voice input.');
                } else if (event.error !== 'aborted') {
                    console.warn('Speech recognition error:', event.error);
                }
                stopListening();
            };

            speechRecognition.onend = function () {
                if (isListening) {
                    inputEl.value = (inputEl.value || '').replace(/\s*\[listening…\]\s*$/, '').trim();
                    autoResize(inputEl);
                }
                stopListening();
            };

            speechRecognition.start();
            isListening = true;
            micBtn.classList.add('recording');
            micBtn.title = 'Stop listening';
        } catch (err) {
            console.warn('Speech recognition not available:', err);
            addErrorMessage('Voice input is not supported in this browser. Try Chrome or Edge.');
        }
    }

    function stopListening() {
        if (speechRecognition && isListening) {
            try {
                speechRecognition.stop();
            } catch (e) { /* ignore */ }
            speechRecognition = null;
        }
        isListening = false;
        if (micBtn) {
            micBtn.classList.remove('recording');
            micBtn.title = 'Use microphone to speak';
        }
    }

    function isMicSupported() {
        return !!SpeechRecognitionAPI;
    }

    // ── Connect ──

    // showAuthModal opens the unified auth modal. preferredTab can be 'password' or 'token'
    // to pre-select the appropriate method based on the server's 401 error code.
    function showAuthModal(preferredTab) {
        const overlay = document.getElementById('auth-modal-overlay');
        const errEl = document.getElementById('agui-auth-error');
        if (overlay) overlay.style.display = 'flex';
        if (errEl) {
            errEl.textContent = '';
            errEl.style.display = 'none';
        }
        switchAuthTab(preferredTab || 'password');
        const onEscape = function (e) {
            if (e.key === 'Escape') {
                closeAuthModal();
            }
        };
        document.addEventListener('keydown', onEscape);
        if (overlay) overlay._authEscapeHandler = onEscape;
    }

    function closeAuthModal() {
        const overlay = document.getElementById('auth-modal-overlay');
        const passwordInput = document.getElementById('agui-password-input');
        const tokenInput = document.getElementById('agui-token-input');
        const errEl = document.getElementById('agui-auth-error');
        if (overlay) {
            if (overlay._authEscapeHandler) {
                document.removeEventListener('keydown', overlay._authEscapeHandler);
                overlay._authEscapeHandler = null;
            }
            overlay.style.display = 'none';
        }
        if (errEl) {
            errEl.textContent = '';
            errEl.style.display = 'none';
        }
        if (passwordInput) passwordInput.value = '';
        if (tokenInput) tokenInput.value = '';
    }

    // switchAuthTab toggles between 'password', 'token', and 'oauth' panels in the auth modal.
    function switchAuthTab(tab) {
        activeAuthTab = tab;
        var allTabs = ['password', 'token', 'oauth'];
        allTabs.forEach(function (t) {
            var tabBtn = document.getElementById('auth-tab-' + t);
            var panel = document.getElementById('auth-panel-' + t);
            if (tabBtn) {
                tabBtn.classList.toggle('active', t === tab);
                tabBtn.setAttribute('aria-selected', t === tab ? 'true' : 'false');
            }
            if (panel) panel.style.display = (t === tab) ? '' : 'none';
        });
        // Focus the appropriate input.
        if (tab === 'password') {
            var pi = document.getElementById('agui-password-input');
            if (pi) pi.focus();
        } else if (tab === 'token') {
            var ti = document.getElementById('agui-token-input');
            if (ti) ti.focus();
        }
        // Show/hide Connect button (not needed for OAuth).
        var submitBtn = document.getElementById('auth-submit-btn');
        if (submitBtn) submitBtn.style.display = (tab === 'oauth') ? 'none' : '';
    }

    // connectWithActiveAuth dispatches to the correct connect method based on the active tab.
    function connectWithActiveAuth() {
        if (activeAuthTab === 'token') {
            connectWithToken();
        } else {
            connectWithPassword();
        }
    }

    // Legacy alias so old code paths still work.
    function showPasswordModal() { showAuthModal('password'); }
    function closePasswordModal() { closeAuthModal(); }

    function onHealthSuccess(data) {
        const userName = data.user || '';
        // Read agent name from /health and update all UI labels.
        if (data.agent_name) {
            agentDisplayName = data.agent_name;
            updateAgentLabels(agentDisplayName);
        }
        setConnected(true, userName);
        emptyState.style.display = 'none';
        closeAuthModal();
        inputEl.disabled = false;
        sendBtn.disabled = false;
        if (micBtn) {
            micBtn.disabled = !isMicSupported();
            micBtn.title = isMicSupported() ? 'Use microphone to speak' : 'Voice input not supported in this browser';
        }
        if (attachBtn) attachBtn.disabled = false;
        inputEl.focus();
        addSystemMessage(userName
            ? 'Hello, ' + escapeHtml(userName) + '!  ' + escapeHtml(agentDisplayName) + ' is ready at ' + serverUrl
            : escapeHtml(agentDisplayName) + ' is ready at ' + serverUrl);
        fetchAndShowResume();
        fetchAndShowCapabilities();
        showNotificationPrompt();
        currentConversation = {
            threadId: threadId,
            serverUrl: serverUrl,
            userId: userId,
            platform: PLATFORM_ID,
            title: 'New conversation',
            messages: [],
            updatedAt: Date.now(),
        };
        refreshSidebar();
    }

    // Updates all UI labels that reference the agent name.
    function updateAgentLabels(name) {
        document.title = 'Chat — ' + name + ' by Stackgen';
        const headerEl = document.getElementById('header-title');
        if (headerEl) headerEl.textContent = name + ' Chat';
        if (inputEl) inputEl.placeholder = 'Ask ' + name + ' anything...';
        const welcomeEl = document.getElementById('welcome-title');
        if (welcomeEl) welcomeEl.textContent = 'Welcome to ' + name;
        const notifText = document.getElementById('notification-text');
        if (notifText) notifText.textContent = 'Get notified when ' + name + ' needs your approval or has a reply.';
        const resumeLabel = document.getElementById('resume-label');
        if (resumeLabel) resumeLabel.textContent = '\ud83e\uddde ' + name + ' Resume';
        const capLabel = document.getElementById('capabilities-label');
        if (capLabel) capLabel.textContent = '\ud83d\udccb What ' + name + ' can do';
    }

    function isAllowedServerUrl(url) {
        try {
            if (!/^https?:\/\//i.test(url)) return false;
            const u = new URL(url);
            return (u.protocol === 'http:' || u.protocol === 'https:') && u.host && u.host.length > 0;
        } catch (_) {
            return false;
        }
    }

    async function connectToServer() {
        const endpoint = document.getElementById('endpoint-input').value.trim();
        if (!endpoint) return;

        const normalized = endpoint.replace(/\/+$/, '');
        if (!isAllowedServerUrl(normalized)) {
            addConnectionFailureMessage(new Error('URL must use http or https'), false);
            return;
        }
        serverUrl = normalized;
        aguiPasswordForSession = '';
        aguiTokenForSession = '';
        closeAuthModal();

        try {
            const res = await fetch(serverUrl + '/health', { mode: 'cors', headers: aguiAuthHeaders() });
            if (res.status === 401) {
                // Parse the error body to determine which auth method is required.
                let preferredTab = 'password';
                try {
                    const errBody = await res.json();
                    if ((errBody && errBody.auth_method === 'oidc') || (errBody && errBody.oauth_enabled)) {
                        serverSupportsOAuth = true;
                        serverOAuthLoginUrl = errBody.login_url || '/auth/login';
                        preferredTab = 'oauth';
                        // Show the Google tab.
                        const oauthTab = document.getElementById('auth-tab-oauth');
                        if (oauthTab) oauthTab.style.display = '';

                        // NEW: Automatically redirect to login since oauth is the preferred method
                        try {
                            const loginUrl = new URL(serverOAuthLoginUrl, serverUrl);
                            loginUrl.searchParams.set('return_to', window.location.pathname + window.location.search + window.location.hash);
                            window.location.href = loginUrl.toString();
                            return; // Ensure no modal is shown on redirect
                        } catch (e) {
                            // If URL construction fails, log and fall through to showing the auth modal.
                            console.error('Invalid OAuth login URL', e);
                        }
                    } else if ((errBody && errBody.auth_method === 'jwt') || (errBody && errBody.error === 'missing_token') || (errBody && errBody.error === 'invalid_token')) {
                        preferredTab = 'token';
                    } else if (errBody && errBody.auth_method === 'apikey') {
                        preferredTab = 'token'; // fallback token for api key UI
                    }
                } catch (_parseErr) { /* fallback to password tab */ }
                showAuthModal(preferredTab);
                return;
            }
            if (!res.ok) throw new Error('Health check failed');
            const data = await res.json();
            if (data.status === 'ok') {
                onHealthSuccess(data);
                await refreshSidebar();
            }
        } catch (err) {
            const isLikelyCors = (err.name === 'TypeError' && err.message === 'Failed to fetch') ||
                (err.message && String(err.message).toLowerCase().indexOf('cors') !== -1);
            addConnectionFailureMessage(err, isLikelyCors);
        }
    }

    // isPasswordSafeUrl returns true if the URL is HTTPS or localhost (safe to send credentials).
    function isPasswordSafeUrl(urlStr) {
        try {
            const u = new URL(urlStr);
            if (u.protocol === 'https:') return true;
            if (u.protocol === 'http:' && (u.hostname === 'localhost' || u.hostname === '127.0.0.1')) return true;
            return false;
        } catch (_) { return false; }
    }

    async function connectWithPassword() {
        const passwordInput = document.getElementById('agui-password-input');
        const pwd = (passwordInput && passwordInput.value) ? String(passwordInput.value) : '';
        const errEl = document.getElementById('agui-auth-error');
        if (!pwd) {
            if (errEl) {
                errEl.textContent = 'Enter the password.';
                errEl.style.display = 'block';
            }
            return;
        }
        if (!isPasswordSafeUrl(serverUrl)) {
            if (errEl) {
                errEl.textContent = 'Password cannot be sent over an insecure connection. Use HTTPS or connect to localhost.';
                errEl.style.display = 'block';
            }
            return;
        }
        if (errEl) errEl.style.display = 'none';
        try {
            const res = await fetch(serverUrl + '/health', {
                mode: 'cors',
                headers: { 'X-AGUI-Password': pwd }
            });
            if (res.status === 401) {
                if (errEl) {
                    errEl.textContent = 'Wrong password. Try again.';
                    errEl.style.display = 'block';
                }
                return;
            }
            if (!res.ok) throw new Error('Health check failed');
            const data = await res.json();
            if (data.status === 'ok') {
                aguiPasswordForSession = pwd;
                aguiTokenForSession = '';
                closeAuthModal();
                onHealthSuccess(data);
                await refreshSidebar();
            }
        } catch (err) {
            addConnectionFailureMessage(err, false);
        }
    }

    // connectWithToken authenticates using a JWT/OIDC Bearer token.
    async function connectWithToken() {
        const tokenInput = document.getElementById('agui-token-input');
        const token = (tokenInput && tokenInput.value) ? String(tokenInput.value).trim() : '';
        const errEl = document.getElementById('agui-auth-error');
        if (!token) {
            if (errEl) {
                errEl.textContent = 'Paste your JWT token.';
                errEl.style.display = 'block';
            }
            return;
        }
        if (!isPasswordSafeUrl(serverUrl)) {
            if (errEl) {
                errEl.textContent = 'Token cannot be sent over an insecure connection. Use HTTPS or connect to localhost.';
                errEl.style.display = 'block';
            }
            return;
        }
        if (errEl) errEl.style.display = 'none';
        try {
            const res = await fetch(serverUrl + '/health', {
                mode: 'cors',
                headers: { 'Authorization': 'Bearer ' + token }
            });
            if (res.status === 401) {
                if (errEl) {
                    let msg = 'Token rejected. Check token validity and try again.';
                    try {
                        const errBody = await res.json();
                        if (errBody && errBody.message) msg = errBody.message;
                    } catch (_) { /* use default msg */ }
                    errEl.textContent = msg;
                    errEl.style.display = 'block';
                }
                return;
            }
            if (!res.ok) throw new Error('Health check failed');
            const data = await res.json();
            if (data.status === 'ok') {
                aguiTokenForSession = token;
                aguiPasswordForSession = '';
                closeAuthModal();
                onHealthSuccess(data);
                await refreshSidebar();
            }
        } catch (err) {
            addConnectionFailureMessage(err, false);
        }
    }

    // loginWithGoogle redirects the browser to the Genie server's OAuth login endpoint.
    function loginWithGoogle() {
        if (!serverUrl) return;
        try {
            const loginUrl = new URL(serverOAuthLoginUrl, serverUrl);
            loginUrl.searchParams.set('return_to', window.location.pathname + window.location.search + window.location.hash);
            window.location.href = loginUrl.toString();
        } catch (e) {
            window.location.href = serverUrl + serverOAuthLoginUrl;
        }
    }

    // Connection failure UI: error + optional CORS hint, auto-removed after 30 seconds.
    function addConnectionFailureMessage(err, isLikelyCors) {
        const corsUrl = 'https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/CORS';
        const wrapper = document.createElement('div');
        wrapper.className = 'connection-failure-dismissible';

        const errorDiv = document.createElement('div');
        errorDiv.className = 'flex justify-center mb-4';
        errorDiv.innerHTML = isLikelyCors
            ? '<div class="chat-bubble error">⚠️ Connection blocked by CORS. The browser blocked the request because the server did not allow your origin.</div>'
            : '<div class="chat-bubble error">⚠️ Failed to connect: ' + escapeHtml(err.message) + '. Ensure genie is running and accessible.</div>';
        wrapper.appendChild(errorDiv);

        if (isLikelyCors) {
            const hintDiv = document.createElement('div');
            hintDiv.className = 'flex justify-center mb-4';
            hintDiv.innerHTML = '<div class="chat-bubble system">' +
                'To allow this page to connect, add your origin to Genie\'s CORS settings: ' +
                '<strong>Config → Messenger → AGUI → CORS Origins</strong> (comma-separated list). ' +
                'If you opened this page as a file (<code>file://</code>), the origin is <code>null</code>; ' +
                'serve the chat from a local HTTP server instead so you get a real origin (e.g. <code>http://localhost:…</code>) to add. ' +
                'Learn more: <a href="' + escapeHtml(corsUrl) + '" target="_blank" rel="noopener noreferrer">CORS (MDN)</a>.' +
                '</div>';
            wrapper.appendChild(hintDiv);
        }

        messagesEl.appendChild(wrapper);
        scrollToBottom();
        setTimeout(function () { wrapper.remove(); }, 10000);
    }

    function setConnected(connected, userName) {
        isConnected = connected;
        connectionBadge.className = 'connection-badge ' + (connected ? 'connected' : (isReconnecting ? 'reconnecting' : 'disconnected'));
        if (isReconnecting) {
            connectionLabel.textContent = 'Reconnecting…';
            connectionBadge.title = 'Reconnecting…';
        } else if (connected && userName) {
            connectionLabel.textContent = 'Connected · ' + userName;
            connectionBadge.title = 'Connected';
        } else if (connected) {
            connectionLabel.textContent = 'Connected';
            connectionBadge.title = 'Connected';
        } else {
            connectionLabel.textContent = 'Not connected';
            connectionBadge.title = serverUrl ? 'Click to reconnect' : 'Not connected';
        }
        updateButtonState();
        // Hide resume section when disconnected
        if (!connected) {
            document.getElementById('resume-container').classList.remove('visible');
            const capEl = document.getElementById('capabilities-container');
            if (capEl) capEl.classList.remove('visible');
        }
    }

    // User- or UI-triggered reconnect (resets attempt count and starts backoff).
    function triggerReconnect() {
        if (isReconnecting || !serverUrl) return;
        reconnectAttempts = 0;
        tryReconnectWithBackoff();
    }

    // Attempts a single reconnection (health check) using stored serverUrl and session password.
    async function tryReconnectOnce() {
        if (!serverUrl) return false;
        try {
            const res = await fetch(serverUrl + '/health', { mode: 'cors', headers: aguiAuthHeaders() });
            if (res.status === 401) return false; // would need password again
            if (!res.ok) return false;
            const data = await res.json();
            return data.status === 'ok';
        } catch (_) {
            return false;
        }
    }

    // Called after a successful reconnection (stream was lost, we re-established).
    function onReconnectSuccess(data) {
        const userName = data.user || '';
        isReconnecting = false;
        reconnectAttempts = 0;
        setConnected(true, userName);
        inputEl.disabled = false;
        sendBtn.disabled = false;
        if (micBtn) {
            micBtn.disabled = !isMicSupported();
        }
        addSystemMessage('Reconnected. You can continue the conversation.');
        fetchAndShowResume();
        fetchAndShowCapabilities();
        updateButtonState();
    }

    function addReconnectPrompt() {
        const div = document.createElement('div');
        div.className = 'flex justify-center mb-4';
        div.innerHTML = '<div class="chat-bubble system">Connection lost. <button type="button" class="reconnect-inline-btn" onclick="genie.triggerReconnect()">Reconnect</button></div>';
        messagesEl.appendChild(div);
        scrollToBottom();
    }

    // Tries to reconnect with exponential backoff; on success resumes the chat (same thread/conversation).
    function tryReconnectWithBackoff() {
        if (isReconnecting || !serverUrl) return;
        isReconnecting = true;
        connectionLabel.textContent = 'Reconnecting…';
        connectionBadge.className = 'connection-badge reconnecting';

        function attempt() {
            tryReconnectOnce().then(function (ok) {
                if (!ok) {
                    reconnectAttempts += 1;
                    if (reconnectAttempts < RECONNECT_MAX_ATTEMPTS) {
                        const delay = RECONNECT_BASE_MS * Math.pow(2, reconnectAttempts - 1);
                        setTimeout(attempt, delay);
                        return;
                    }
                    isReconnecting = false;
                    setConnected(false);
                    addReconnectPrompt();
                    return;
                }
                fetch(serverUrl + '/health', { mode: 'cors', headers: aguiAuthHeaders() })
                    .then(function (r) { return r.ok ? r.json() : null; })
                    .then(function (data) {
                        if (data && data.status === 'ok') {
                            onReconnectSuccess(data);
                        } else {
                            isReconnecting = false;
                            setConnected(false);
                        }
                    })
                    .catch(function () {
                        isReconnecting = false;
                        setConnected(false);
                    });
            });
        }
        attempt();
    }

    // ── Fetch & Display Genie Resume ──
    async function fetchAndShowResume() {
        try {
            const res = await fetch(serverUrl + '/api/v1/resume', {
                method: 'GET',
                mode: 'cors',
                headers: aguiAuthHeaders(),
            });
            if (!res.ok) return; // silently skip if not available
            const text = await res.text();
            if (!text || !text.trim()) return;
            document.getElementById('resume-body').textContent = text;
            document.getElementById('resume-container').classList.add('visible');
        } catch (err) {
            console.warn('Resume fetch failed:', err);
        }
    }

    // ── Fetch & Display Capabilities (AI stance: tools, always_allowed, denied_tools) ──
    async function fetchAndShowCapabilities() {
        const container = document.getElementById('capabilities-container');
        const body = document.getElementById('capabilities-body');
        if (!container || !body) return;
        try {
            const res = await fetch(serverUrl + '/api/v1/capabilities', {
                method: 'GET',
                mode: 'cors',
                headers: aguiAuthHeaders(),
            });
            if (!res.ok) return;
            const data = await res.json();
            if (!data || (!data.tool_names && !data.always_allowed && !data.denied_tools)) return;

            const tools = data.tool_names || [];
            const allowed = data.always_allowed || [];
            const denied = data.denied_tools || [];

            let html = '';
            html += '<p class="text-sm text-gray-700 mb-3"><strong>' + tools.length + ' tools</strong> available. ';
            if (allowed.length > 0) {
                html += '<span class="text-green-700">' + allowed.length + ' auto-approved</span> (no prompt). ';
            }
            if (denied.length > 0) {
                html += '<span class="text-amber-700">' + denied.length + ' blocked</span>.</p>';
            } else {
                html += '</p>';
            }

            if (tools.length > 0) {
                html += '<p class="text-xs font-semibold text-gray-600 mt-2 mb-1">Available tools</p>';
                html += '<ul class="text-xs text-gray-600 list-disc pl-4 mb-2 max-h-32 overflow-y-auto">';
                tools.slice(0, 50).forEach(function (name) {
                    html += '<li><code class="bg-gray-100 px-1 rounded">' + escapeHtml(name) + '</code></li>';
                });
                if (tools.length > 50) {
                    html += '<li class="text-gray-400">… and ' + (tools.length - 50) + ' more</li>';
                }
                html += '</ul>';
            }
            if (allowed.length > 0) {
                html += '<p class="text-xs font-semibold text-green-800 mt-2 mb-1">Auto-approved (no prompt)</p>';
                html += '<p class="text-xs text-gray-600">' + allowed.map(function (a) { return '<code class="bg-green-50 px-1 rounded">' + escapeHtml(a) + '</code>'; }).join(', ') + '</p>';
            }
            if (denied.length > 0) {
                html += '<p class="text-xs font-semibold text-amber-800 mt-2 mb-1">Blocked</p>';
                html += '<p class="text-xs text-gray-600">' + denied.map(function (d) { return '<code class="bg-amber-50 px-1 rounded">' + escapeHtml(d) + '</code>'; }).join(', ') + '</p>';
            }

            body.innerHTML = html;
            container.classList.add('visible');
        } catch (err) {
            console.warn('Capabilities fetch failed:', err);
        }
    }

    // ── Send Message ──
    async function sendMessage() {
        const message = inputEl.value.replace(/\s*\[listening…\]\s*$/, '').trim();
        if ((!message && pendingFiles.length === 0) || !isConnected) return;

        if (isListening) stopListening();

        // Capture files before clearing
        const filesToSend = [...pendingFiles];
        clearPendingFiles();

        // Build display text for the user bubble (includes attachment names)
        const displayParts = [];
        if (filesToSend.length > 0) {
            displayParts.push(filesToSend.map(f => '[Attached: ' + f.name + ']').join(' '));
        }
        if (message) displayParts.push(message);
        const displayText = displayParts.join('\n');

        // Add user bubble with media previews
        addUserBubble(displayText, filesToSend);
        inputEl.value = '';
        autoResize(inputEl);

        // Persist user message
        if (currentConversation) {
            if (currentConversation.messages.length === 0) {
                currentConversation.title = (message || 'Media attachment').length > 60 ? (message || 'Media attachment').substring(0, 60) + '…' : (message || 'Media attachment');
            }
            currentConversation.messages.push({ role: 'user', content: displayText, timestamp: Date.now() });
            currentConversation.updatedAt = Date.now();
            genieDB.saveConversation(currentConversation).then(() => refreshSidebar()).catch(console.warn);
        }

        // Mid-run feedback injection if agent is already thinking
        if (isStreaming) {
            // Block attachments during mid-run injection: the inject endpoint
            // only supports plain text, so file data would be silently dropped.
            if (filesToSend.length > 0) {
                addErrorMessage('File attachments cannot be sent while the agent is thinking. Please wait for the current response to finish.');
                return;
            }
            try {
                const response = await fetch(serverUrl + '/api/v1/inject', {
                    method: 'POST',
                    headers: Object.assign({ 'Content-Type': 'application/json' }, aguiAuthHeaders()),
                    body: JSON.stringify({
                        threadId: threadId,
                        message: displayText
                    })
                });
                if (!response.ok) {
                    addErrorMessage('Failed to inject feedback: ' + response.statusText);
                }
            } catch (err) {
                console.error('Inject error:', err);
                addErrorMessage('Failed to send feedback mid-run.');
            }
            return;
        }

        // Encode files as base64 for the message payload
        let messageWithFiles = message || '';
        if (filesToSend.length > 0) {
            try {
                const encodedParts = await Promise.all(filesToSend.map(async (file) => {
                    const dataUrl = await fileToDataUrl(file);
                    return '[file:' + file.name + ':' + file.type + ']\n' + dataUrl;
                }));
                messageWithFiles = encodedParts.join('\n\n') + (message ? '\n\n' + message : '');
            } catch (err) {
                console.error('File encoding error:', err);
                addErrorMessage('Failed to encode attached files.');
                return;
            }
        }

        // Prepare request for normal run
        runCounter++;
        const runId = 'run-' + runCounter;
        isStreaming = true;

        // We do NOT disable the input or send button anymore so users can type mid-run feedback.
        document.getElementById('stop-btn').classList.add('visible');
        abortController = new AbortController();

        // Start SSE stream
        try {
            const response = await fetch(serverUrl + '/', {
                method: 'POST',
                signal: abortController.signal,
                headers: Object.assign({ 'Content-Type': 'application/json' }, aguiAuthHeaders()),
                body: JSON.stringify({
                    threadId: threadId,
                    runId: runId,
                    // Segmentation fields: the backend uses these to
                    // scope context/memory per platform+user+chat.
                    userId: userId,
                    platform: PLATFORM_ID,
                    messages: [{ role: 'user', content: messageWithFiles }]
                })
            });

            if (!response.ok) {
                throw new Error('Server returned ' + response.status);
            }

            // Parse SSE stream
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });

                // SSE events are delimited by blank lines (\n\n).
                // Process all complete events, keep any partial trailing data.
                let delimIdx;
                while ((delimIdx = buffer.indexOf('\n\n')) !== -1) {
                    const frame = buffer.substring(0, delimIdx);
                    buffer = buffer.substring(delimIdx + 2);

                    let eventType = '';
                    let dataLines = [];
                    for (const line of frame.split('\n')) {
                        if (line.startsWith('event:')) {
                            eventType = line.slice(6).trim();
                        } else if (line.startsWith('data:')) {
                            dataLines.push(line.slice(5).trimStart());
                        }
                        // Skip comments (: lines)
                    }
                    if (dataLines.length > 0) {
                        const raw = dataLines.join('\n');
                        try {
                            const event = JSON.parse(raw);
                            handleSSEEvent(event, eventType);
                        } catch (e) {
                            console.warn('[SSE] bad JSON:', raw);
                        }
                    }
                }
            }
        } catch (err) {
            if (err.name === 'AbortError') {
                addErrorMessage('Stopped by user');
            } else {
                addErrorMessage('Stream error: ' + err.message);
                setConnected(false);
                tryReconnectWithBackoff();
            }
        } finally {
            finishStreaming();
        }
    }

    // ── SSE Event Handler ──
    function handleSSEEvent(event, eventType) {
        const type = event.type || eventType;

        switch (type) {
            case 'RUN_STARTED':
                hitlApprovalCount = 0;
                hitlNudgeShown = false;
                pendingApprovals.clear();
                removeBatchApproveBanner();
                showThinking();
                break;

            case 'TEXT_MESSAGE_START':
                hideThinking();
                // If a new message starts for a different messageId, finalize the old one.
                if (currentMessageId && event.messageId && event.messageId !== currentMessageId) {
                    finalizeAssistantBubble();
                }
                currentMessageId = event.messageId || null;
                // Only start a new bubble if we don't already have one
                if (!currentAssistantBubble) {
                    startAssistantBubble();
                }
                break;

            case 'TEXT_MESSAGE_CONTENT':
                hideThinking();
                // If messageId changed, finalize old bubble & start new one.
                if (event.messageId && currentMessageId && event.messageId !== currentMessageId) {
                    finalizeAssistantBubble();
                    currentMessageId = event.messageId;
                }
                if (!currentAssistantBubble) {
                    currentMessageId = event.messageId || null;
                    startAssistantBubble();
                }
                appendToAssistantBubble(event.delta || '');
                break;

            case 'TEXT_MESSAGE_END':
                finalizeAssistantBubble();
                currentMessageId = null;
                break;

            case 'REASONING_MESSAGE_CONTENT':
                // Show reasoning as a dimmer text
                if (!currentAssistantBubble) startAssistantBubble();
                appendToAssistantBubble(event.delta || '');
                break;

            case 'TOOL_CALL_START':
                hideThinking();
                // Skip rendering generic tool card for ask_clarifying_question —
                // CLARIFICATION_REQUEST event renders a dedicated input form instead.
                if (event.toolCallName !== 'ask_clarifying_question') {
                    addToolCard(event.toolCallId, event.toolCallName, 'running');
                }
                break;

            case 'TOOL_CALL_ARGS':
                // Accumulate tool arguments for later display
                if (event.toolCallId && toolCallMeta[event.toolCallId]) {
                    toolCallMeta[event.toolCallId].args += (event.delta || '');
                }
                break;

            case 'TOOL_CALL_END':
                updateToolCard(event.toolCallId, 'completed');
                populateAgentDetails(event.toolCallId);
                break;

            case 'TOOL_CALL_RESULT': {
                const isError = event.content && (
                    event.content.startsWith('tool execution error:') ||
                    event.content.startsWith('Error:')
                );
                updateToolCard(event.toolCallId, isError ? 'error' : 'done', event.content);
                break;
            }

            case 'RUN_FINISHED':
                hideThinking();
                finalizeAssistantBubble();
                break;

            case 'RUN_ERROR':
                hideThinking();
                finalizeAssistantBubble();
                addErrorMessage(event.message || 'An error occurred');
                break;

            case 'STEP_STARTED':
                showThinking(event.stepName);
                break;

            case 'CUSTOM':
                // User action events — render native cards for login, confirmation, etc.
                if (event.name === 'user_action_required' && event.value) {
                    hideThinking();
                    addUserActionCard(event.value);
                    showNotification(
                        'Action required',
                        event.value.message || ('Please complete: ' + (event.value.action || 'action')),
                        'user-action-' + (event.value.service || '')
                    );
                    vibrateBrief();
                }
                // Log events — just show as subtle system messages
                if (event.name === 'log' && event.value) {
                    // Skip debug logs
                    if (event.value.level !== 'DEBUG') {
                        console.log('[Genie]', event.value.level, event.value.message);
                    }
                }
                break;

            case 'TOOL_APPROVAL_REQUEST':
                hideThinking();
                if (event.autoApproved) {
                    addAutoApprovedCard(event.toolCallName, event.content, event.justification);
                } else {
                    addApprovalCard(event.approvalId, event.toolCallName, event.content, event.justification);
                    showNotification('Approval required', (event.toolCallName || 'A tool') + ' needs your approval', 'approval-' + (event.approvalId || ''), {
                        type: 'APPROVAL_REQUEST',
                        approvalId: event.approvalId
                    });
                    vibrateBrief();
                }
                break;

            case 'CLARIFICATION_REQUEST':
                hideThinking();
                addClarificationCard(event.approvalId, event.content, event.message);
                showNotification(agentDisplayName + ' needs your input', (event.content || 'Please answer in the chat').substring(0, 80), 'clarify-' + (event.approvalId || ''));
                vibrateBrief();
                break;
        }
    }

    // ── UI Helpers ──
    function addUserBubble(text, files) {
        const div = document.createElement('div');
        div.className = 'flex justify-end mb-4';
        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble user';

        // Show media previews in the bubble
        if (files && files.length > 0) {
            const attachDiv = document.createElement('div');
            attachDiv.className = 'user-attachments';
            files.forEach(file => {
                const url = URL.createObjectURL(file);
                chatHistoryBlobURLs.push(url);
                if (file.type.startsWith('image/')) {
                    const img = document.createElement('img');
                    img.src = url;
                    img.alt = file.name;
                    img.loading = 'lazy';
                    attachDiv.appendChild(img);
                } else if (file.type.startsWith('audio/')) {
                    const audio = document.createElement('audio');
                    audio.src = url;
                    audio.controls = true;
                    audio.preload = 'metadata';
                    attachDiv.appendChild(audio);
                } else if (file.type.startsWith('video/')) {
                    const video = document.createElement('video');
                    video.src = url;
                    video.controls = true;
                    video.preload = 'metadata';
                    video.muted = true;
                    attachDiv.appendChild(video);
                } else {
                    const fileTag = document.createElement('span');
                    fileTag.className = 'user-attachment-file';
                    fileTag.textContent = '📎 ' + file.name;
                    attachDiv.appendChild(fileTag);
                }
            });
            bubble.appendChild(attachDiv);
        }

        // Only show text if there's a message beyond the [Attached: ...] tags
        const textWithoutAttachTags = text.replace(/\[Attached: [^\]]+\]\s*/g, '').trim();
        if (textWithoutAttachTags) {
            const textSpan = document.createElement('span');
            textSpan.textContent = textWithoutAttachTags;
            bubble.appendChild(textSpan);
        }

        div.appendChild(bubble);
        messagesEl.appendChild(div);
        scrollToBottom();
    }

    function startAssistantBubble() {
        if (currentAssistantBubble) return;
        currentAssistantContent = '';
        displayedContentLength = 0;

        const wrapper = document.createElement('div');
        wrapper.className = 'flex justify-start mb-4';

        const bubbleWrap = document.createElement('div');
        bubbleWrap.className = 'bubble-wrapper';

        const bubble = document.createElement('div');
        bubble.className = 'chat-bubble assistant typing-effect';
        bubble.innerHTML = '';

        bubbleWrap.appendChild(bubble);
        wrapper.appendChild(bubbleWrap);
        messagesEl.appendChild(wrapper);
        currentAssistantBubble = bubble;
        scrollToBottom();
    }

    function appendToAssistantBubble(text) {
        if (!currentAssistantBubble) return;
        currentAssistantContent += text;
        ensureTyping();
    }

    function finalizeAssistantBubble() {
        if (typingInterval) {
            clearInterval(typingInterval);
            typingInterval = null;
        }

        if (currentAssistantBubble && currentAssistantContent) {
            currentAssistantBubble.classList.remove('typing-effect');
            currentAssistantBubble.innerHTML = renderMarkdown(currentAssistantContent);
            displayedContentLength = currentAssistantContent.length;

            addCopyButton(currentAssistantBubble, currentAssistantContent);
            addSpeakButton(currentAssistantBubble, currentAssistantContent);

            // Persist assistant response
            if (currentConversation && currentAssistantContent.trim()) {
                currentConversation.messages.push({ role: 'assistant', content: currentAssistantContent, timestamp: Date.now() });
                currentConversation.updatedAt = Date.now();
                genieDB.saveConversation(currentConversation).then(() => refreshSidebar()).catch(console.warn);
                updateButtonState();
            }

            if (document.hidden && notificationsSupported() && Notification.permission === 'granted') {
                const snippet = currentAssistantContent.trim().replace(/\s+/g, ' ').substring(0, 60);
                showNotification('Genie replied', (snippet.length === 60 ? snippet + '…' : snippet) || 'New message', 'genie-reply');
                vibrateBrief();
            }
        }
        currentAssistantBubble = null;
        currentAssistantContent = '';
        displayedContentLength = 0;
    }

    function ensureTyping() {
        if (typingInterval) return;
        typingInterval = setInterval(() => {
            if (!currentAssistantBubble) {
                clearInterval(typingInterval);
                typingInterval = null;
                return;
            }

            if (displayedContentLength < currentAssistantContent.length) {
                displayedContentLength = Math.min(displayedContentLength + TYPING_CHUNK_SIZE, currentAssistantContent.length);
                const visibleText = currentAssistantContent.substring(0, displayedContentLength);
                currentAssistantBubble.innerHTML = renderMarkdown(visibleText);
                scrollToBottom();
            } else {
                clearInterval(typingInterval);
                typingInterval = null;
                if (currentAssistantBubble) currentAssistantBubble.classList.remove('typing-effect');
            }
        }, TYPING_TICK_RATE);
    }

    function showThinking(label) {
        hideThinking(); // Remove existing
        const div = document.createElement('div');
        div.id = 'thinking-indicator';
        div.className = 'thinking-indicator';
        div.innerHTML = `
<div class="thinking-dots"><span></span><span></span><span></span></div>
<span>${label ? escapeHtml(label) + '...' : 'Thinking...'}</span>
      `;
        messagesEl.appendChild(div);
        scrollToBottom();
    }

    function hideThinking() {
        const el = document.getElementById('thinking-indicator');
        if (el) el.remove();
    }

    // Track tool metadata per call for smart result formatting
    const toolCallMeta = {};

    function friendlyToolName(rawName) {
        const map = {
            'web_search': '🔍 Searching the web',
            'create_agent': '🤖 Delegating to specialist',
            'read_file': '📄 Reading a file',
            'read_multiple_files': '📄 Reading files',
            'save_file': '💾 Saving a file',
            'run_shell': '⚙️ Running a command',
            'list_file': '📂 Listing files',
            'search_file': '🔍 Searching files',
            'search_content': '🔍 Searching content',
            'replace_content': '✏️ Editing file',
            'summarize_content': '📝 Summarizing',
            'memory_search': '🧠 Searching memory',
            'memory_store': '🧠 Storing to memory',
            'browser_navigate': '🌐 Opening page',
            'browser_read_text': '🌐 Reading page',
            'browser_read_html': '🌐 Reading page HTML',
            'browser_click': '🖱️ Clicking element',
            'browser_type': '⌨️ Typing text',
            'browser_screenshot': '📸 Taking screenshot',
            'browser_eval_js': '🌐 Running browser script',
            'browser_wait': '⏳ Waiting for page',
            'email_send': '📧 Sending email',
            'email_read': '📧 Reading email',
        };
        return map[rawName] || ('🔧 Using ' + (rawName || 'tool'));
    }

    /**
     * Format a tool result into a human-friendly summary.
     * Avoids showing raw JSON — instead extracts meaningful info
     * based on the tool name and the shape of the response.
     */
    function formatToolResult(toolName, rawContent, toolCallId) {
        if (!rawContent) return null;
        const text = typeof rawContent === 'string' ? rawContent : JSON.stringify(rawContent);

        // Try parsing as JSON for structured extraction
        let parsed = null;
        try { parsed = JSON.parse(text); } catch (_) { /* not JSON */ }

        switch (toolName) {
            case 'web_search': {
                if (parsed && Array.isArray(parsed.results)) {
                    const items = parsed.results.slice(0, 3);
                    return items.map(r => `• ${r.title || r.url || 'Result'}`).join('\n') +
                        (parsed.results.length > 3 ? `\n  … and ${parsed.results.length - 3} more` : '');
                }
                if (parsed && parsed.snippet) return parsed.snippet;
                return _truncText(text, 200);
            }
            case 'create_agent': {
                if (parsed && parsed.result) return _truncText(parsed.result, 300);
                if (parsed && parsed.output) return _truncText(parsed.output, 300);
                return _truncText(text, 300);
            }
            case 'read_file':
            case 'read_multiple_files': {
                const meta = toolCallMeta[toolCallId];
                const fname = _extractArg(meta, 'file_name');
                const preview = _truncText(text, 200);
                return fname ? `📄 ${fname}\n${preview}` : preview;
            }
            case 'save_file': {
                const meta2 = toolCallMeta[toolCallId];
                const fname2 = _extractArg(meta2, 'file_name');
                if (fname2) return `Saved ${fname2}`;
                if (parsed && parsed.message) return parsed.message;
                return _truncText(text, 150);
            }
            case 'list_file':
            case 'search_file':
            case 'search_content': {
                // Often returns a list of file paths
                if (parsed && Array.isArray(parsed)) {
                    const shown = parsed.slice(0, 5).map(f => typeof f === 'string' ? f : (f.path || f.name || JSON.stringify(f)));
                    return shown.map(f => `  ${f}`).join('\n') +
                        (parsed.length > 5 ? `\n  … and ${parsed.length - 5} more` : '');
                }
                return _truncText(text, 200);
            }
            case 'run_shell': {
                if (parsed && parsed.stdout != null) {
                    const out = parsed.stdout || parsed.stderr || '';
                    return _truncText(out, 250);
                }
                return _truncText(text, 250);
            }
            case 'summarize_content':
                return _truncText(text, 300);
            default: {
                // Generic: if it's a JSON object, try to find a 'message', 'result', or 'output' key
                if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
                    const useful = parsed.message || parsed.result || parsed.output || parsed.summary || parsed.content;
                    if (useful) return _truncText(String(useful), 250);
                }
                return _truncText(text, 200);
            }
        }
    }

    function _truncText(s, max) {
        if (!s) return '';
        if (s.length <= max) return s;
        return s.substring(0, max) + '…';
    }

    function _extractArg(meta, key) {
        if (!meta || !meta.args) return null;
        try {
            const a = JSON.parse(meta.args);
            return a[key] || null;
        } catch (_) { return null; }
    }

    function addToolCard(toolCallId, toolName, status) {
        // Track metadata for this tool call
        toolCallMeta[toolCallId] = { name: toolName, args: '' };

        const div = document.createElement('div');
        div.className = 'flex justify-start mb-3';
        const friendly = friendlyToolName(toolName);
        const domId = safeDomId(toolCallId);
        div.innerHTML = `
<details class="tool-card" id="tool-${domId}">
  <summary>
    <span class="tool-label">${escapeHtml(friendly)}</span>
    <span class="tool-status running" id="tool-status-${domId}">Running…</span>
    <span class="tool-chevron">▶</span>
  </summary>
  <div class="tool-body" id="tool-body-${domId}"></div>
</details>
      `;
        messagesEl.appendChild(div);
        scrollToBottom();
    }

    function updateToolCard(toolCallId, status, result) {
        const domId = safeDomId(toolCallId);
        const statusEl = document.getElementById('tool-status-' + domId);
        if (statusEl) {
            if (status === 'error') {
                statusEl.textContent = 'Error ✗';
                statusEl.className = 'tool-status error';
            } else if (status === 'completed' || status === 'done') {
                statusEl.textContent = 'Done ✓';
                statusEl.className = 'tool-status done';
            } else {
                statusEl.textContent = status;
            }
        }

        // Append human-friendly tool result if provided
        if (result) {
            const bodyEl = document.getElementById('tool-body-' + domId);
            const meta = toolCallMeta[toolCallId];
            const toolName = meta ? meta.name : '';
            const friendly = formatToolResult(toolName, result, toolCallId);

            if (bodyEl && friendly) {
                const resultDiv = document.createElement('div');
                resultDiv.className = 'tool-result';
                resultDiv.textContent = friendly;
                bodyEl.appendChild(resultDiv);
            }

            // Persist tool result to conversation
            if (currentConversation && friendly) {
                currentConversation.messages.push({
                    role: 'tool',
                    content: friendly,
                    toolName: toolName,
                    timestamp: Date.now(),
                });
                currentConversation.updatedAt = Date.now();
                genieDB.saveConversation(currentConversation).catch(console.warn);
            }
        }
    }

    function prettyPrintArgs(raw) {
        if (!raw) return '';
        try {
            const parsed = JSON.parse(raw);
            return JSON.stringify(parsed, null, 2);
        } catch (_) {
            return raw;
        }
    }

    function populateAgentDetails(toolCallId) {
        const meta = toolCallMeta[toolCallId];
        if (!meta || meta.name !== 'create_agent') return;
        let args;
        try { args = JSON.parse(meta.args); } catch (_) { return; }

        const domId = safeDomId(toolCallId);
        const bodyEl = document.getElementById('tool-body-' + domId);
        if (!bodyEl) return;

        const labelEl = document.getElementById('tool-' + domId)
            ?.querySelector('.tool-label');
        if (labelEl && args.agent_name) {
            labelEl.textContent = '🤖 Delegating to ' + args.agent_name;
        }

        const toolTags = (args.tool_names || [])
            .map(t => `<span class="agent-tool-tag">${escapeHtml(t)}</span>`)
            .join('');

        const goalText = args.goal
            ? (args.goal.length > 300 ? args.goal.slice(0, 300) + '…' : args.goal)
            : '';

        let html = '<div class="agent-details">';
        if (args.task_type) {
            html += `<div class="agent-detail-row"><span class="agent-detail-label">Type</span><span class="agent-detail-value">${escapeHtml(args.task_type)}</span></div>`;
        }
        if (toolTags) {
            html += `<div class="agent-detail-row"><span class="agent-detail-label">Tools</span><div class="agent-tools">${toolTags}</div></div>`;
        }
        if (Array.isArray(args.steps) && args.steps.length) {
            const stepItems = args.steps.map((s, i) =>
                `<span class="agent-tool-tag">${i + 1}. ${escapeHtml(s.name || s.goal || 'step')}</span>`
            ).join('');
            html += `<div class="agent-detail-row"><span class="agent-detail-label">Steps</span><div class="agent-tools">${stepItems}</div></div>`;
            if (args.flow_type) {
                html += `<div class="agent-detail-row"><span class="agent-detail-label">Flow</span><span class="agent-detail-value">${escapeHtml(args.flow_type)}</span></div>`;
            }
        }
        if (goalText) {
            html += `<div class="agent-detail-row" style="flex-direction:column;gap:0.15rem"><span class="agent-detail-label">Goal</span><div class="agent-goal-text">${escapeHtml(goalText)}</div></div>`;
        }
        html += '</div>';
        bodyEl.innerHTML = html;
    }

    function updateApprovalsColumnVisibility() {
        if (!approvalsColumnEl || !approvalsColumnListEl) return;
        const hasItems = approvalsColumnListEl.children.length > 0;
        approvalsColumnEl.classList.toggle('visible', hasItems);
        updateAppBadge();
    }

    function updateAppBadge() {
        if (typeof navigator.setAppBadge !== 'function' && typeof navigator.clearAppBadge !== 'function') return;
        const count = approvalsColumnListEl ? approvalsColumnListEl.querySelectorAll('.approvals-column-item').length : 0;
        try {
            if (count > 0) navigator.setAppBadge(count);
            else navigator.clearAppBadge();
        } catch (e) { /* ignore */ }
    }

    function toggleFullscreen() {
        const container = document.getElementById('chat-container');
        if (!container) return;
        if (!document.fullscreenElement && !document.webkitFullscreenElement) {
            const req = container.requestFullscreen || container.webkitRequestFullscreen;
            if (req) {
                const p = req.call(container);
                if (p && typeof p.catch === 'function') p.catch(function () { /* fullscreen denied or unsupported */ });
            }
        } else {
            const exit = document.exitFullscreen || document.webkitExitFullscreen;
            if (exit) {
                const p = exit.call(document);
                if (p && typeof p.catch === 'function') p.catch(function () { /* ignore */ });
            }
        }
    }

    document.addEventListener('fullscreenchange', updateFullscreenButton);
    document.addEventListener('webkitfullscreenchange', updateFullscreenButton);
    function updateFullscreenButton() {
        const btn = document.getElementById('fullscreen-btn');
        if (!btn) return;
        const isFs = !!document.fullscreenElement || !!document.webkitFullscreenElement;
        btn.textContent = isFs ? '⛶ Exit fullscreen' : '⛶ Fullscreen';
        btn.title = isFs ? 'Exit fullscreen' : 'Fullscreen';
        btn.setAttribute('aria-label', isFs ? 'Exit fullscreen' : 'Toggle fullscreen');
    }
    (function initFullscreenButton() {
        const container = document.getElementById('chat-container');
        const btn = document.getElementById('fullscreen-btn');
        if (!btn || !container) return;
        var supported = (container.requestFullscreen || container.webkitRequestFullscreen) && (document.exitFullscreen || document.webkitExitFullscreen);
        if (!supported) btn.style.display = 'none';
        else updateFullscreenButton();
    })();

    const POOF_DURATION_MS = 650;

    function dismissWithPoof(itemEl) {
        if (!itemEl || !itemEl.classList) return;
        itemEl.classList.add('poof');
        setTimeout(() => {
            itemEl.remove();
            updateApprovalsColumnVisibility();
        }, POOF_DURATION_MS);
    }

    function addAutoApprovedCard(toolName, args, justification) {
        if (!approvalsColumnListEl) return;
        const domId = 'auto-' + Date.now() + '-' + Math.floor(Math.random() * 1000);

        const div = document.createElement('div');
        div.className = 'flex justify-start approvals-column-item';
        div.innerHTML = `
<div class="approval-card" id="${domId}" style="border-left: 3px solid #10b981; transition: opacity 0.5s ease; opacity: 1;">
  <div class="approval-header" style="color: #065f46;">✅ Auto-approved — ${escapeHtml(toolName || 'tool')}</div>
  <div class="approval-args" style="max-height: 4.5rem; overflow: hidden; font-size: 0.75rem; margin-top: 0.25rem; opacity: 0.8;">${escapeHtml(prettyPrintArgs(args))}</div>
</div>
        `;
        approvalsColumnListEl.appendChild(div);
        updateApprovalsColumnVisibility();

        // fade out and remove after 3.5 seconds
        setTimeout(() => {
            const el = document.getElementById(domId);
            if (el) el.style.opacity = '0';
            setTimeout(() => {
                if (div.parentNode) div.parentNode.removeChild(div);
                updateApprovalsColumnVisibility();
            }, 600); // Wait 0.6s to allow opacity transition
        }, 3500);
    }

    // ── User Action Cards (login, open URL, confirm) ──
    // Tracks which services have already shown an action card to avoid duplicates.
    const shownActionCards = new Set();

    function addUserActionCard(value) {
        const action = value.action || 'open_url';
        const service = value.service || 'service';
        const url = value.url || '';
        const message = value.message || '';

        // De-duplicate by service name to prevent multiple cards for the same auth.
        const dedupeKey = action + ':' + service;
        if (shownActionCards.has(dedupeKey)) return;
        shownActionCards.add(dedupeKey);

        const div = document.createElement('div');
        div.className = 'flex justify-start mb-4';

        let icon = '🔗';
        let title = 'Action Required';
        let btnLabel = 'Open';
        let borderColor = 'rgba(99,102,241,0.4)';
        let bgColor = 'rgba(99,102,241,0.08)';
        let iconBg = 'rgba(99,102,241,0.15)';
        let btnColor = '#4f46e5';

        if (action === 'oauth_login') {
            icon = '🔐';
            title = 'Sign in to ' + escapeHtml(service);
            btnLabel = '🔐 Sign In';
            borderColor = 'rgba(16,185,129,0.5)';
            bgColor = 'rgba(16,185,129,0.06)';
            iconBg = 'rgba(16,185,129,0.15)';
            btnColor = '#059669';
        } else if (action === 'confirm') {
            icon = '✅';
            title = 'Confirmation Required';
            btnLabel = '✅ Confirm';
            borderColor = 'rgba(245,158,11,0.5)';
            bgColor = 'rgba(245,158,11,0.06)';
            iconBg = 'rgba(245,158,11,0.15)';
            btnColor = '#d97706';
        }

        const messageHtml = message
            ? '<p style="font-size:0.85rem;color:#cbd5e1;margin:0.5rem 0 0.75rem;line-height:1.4;">' + escapeHtml(message) + '</p>'
            : '';

        const buttonHtml = url
            ? '<a href="' + escapeAttr(url) + '" target="_blank" rel="noopener" '
              + 'style="display:inline-flex;align-items:center;gap:0.35rem;padding:0.5rem 1.25rem;'
              + 'background:' + btnColor + ';color:#fff;border:none;border-radius:0.5rem;'
              + 'font-size:0.85rem;font-weight:600;text-decoration:none;cursor:pointer;'
              + 'transition:opacity 0.15s ease;" '
              + 'onmouseover="this.style.opacity=0.85" onmouseout="this.style.opacity=1"'
              + '>' + btnLabel + '</a>'
            : '';

        div.innerHTML = '\
<div style="border-left:3px solid ' + borderColor + ';background:' + bgColor + ';border-radius:0.5rem;padding:0.85rem 1rem;max-width:420px;width:100%;">\
  <div style="display:flex;align-items:center;gap:0.5rem;margin-bottom:0.25rem;">\
    <span style="font-size:1.3rem;background:' + iconBg + ';width:2rem;height:2rem;display:flex;align-items:center;justify-content:center;border-radius:0.4rem;">' + icon + '</span>\
    <span style="font-weight:600;font-size:0.95rem;color:#e2e8f0;">' + title + '</span>\
  </div>\
  ' + messageHtml + '\
  ' + buttonHtml + '\
</div>';

        messagesEl.appendChild(div);
        scrollToBottom();
    }

    function addApprovalCard(approvalId, toolName, args, justification) {
        hitlApprovalCount += 1;
        // Track this pending approval for the batch-approve feature.
        pendingApprovals.set(approvalId, { toolName: toolName || 'tool', addedAt: Date.now() });

        const div = document.createElement('div');
        div.className = 'flex justify-start approvals-column-item';
        const justificationHtml = justification
            ? `<div style="font-size:0.75rem;color:#a5b4fc;background:rgba(99,102,241,0.1);border-left:3px solid rgba(99,102,241,0.4);padding:0.35rem 0.5rem;margin-bottom:0.5rem;border-radius:0 0.25rem 0.25rem 0;">💡 ${renderMarkdown(justification)}</div>`
            : '';
        const prettyArgs = prettyPrintArgs(args);
        div.innerHTML = `
<div class="approval-card" id="approval-${escapeAttr(approvalId)}">
  <div class="approval-header">⚠️ Approval Required — ${escapeHtml(toolName || 'tool')}</div>
  ${justificationHtml}
  <details class="approval-args-toggle">
    <summary>📋 Arguments (click to expand)</summary>
    <div class="approval-args">${escapeHtml(prettyArgs)}</div>
  </details>
  <div class="feedback-area">
    <textarea id="feedback-${escapeAttr(approvalId)}" placeholder="Optional feedback — guide the agent on what to change…" rows="1"></textarea>
  </div>
  <div class="approval-allow-row">
    <div style="display:flex;align-items:center;gap:0.5rem;flex-wrap:wrap;">
      <label for="allow-for-${escapeAttr(approvalId)}">Allow future calls for:</label>
      <select id="allow-for-${escapeAttr(approvalId)}">
        <option value="0">One-time only</option>
        <option value="5">5 min</option>
        <option value="10">10 min</option>
        <option value="30">30 min</option>
        <option value="60">60 min</option>
      </select>
    </div>
    <div style="margin-top:0.4rem;">
      <label for="allow-args-${escapeAttr(approvalId)}">Only when args contain (comma-separated, optional):</label>
      <input type="text" id="allow-args-${escapeAttr(approvalId)}" placeholder="e.g. /docs, /tmp" style="width:100%;margin-top:0.25rem;" />
    </div>
  </div>
  <div class="approval-actions" id="approval-actions-${escapeAttr(approvalId)}" style="margin-top:0.75rem;">
    <button class="btn-approve" onclick="genie.resolveApproval('${escapeJsQuoted(approvalId)}', 'approved')">✅ Approve</button>
    <button class="btn-revisit" onclick="genie.revisitApproval('${escapeJsQuoted(approvalId)}')" title="Send back with feedback for the agent to rethink">🔄 Revisit</button>
    <button class="btn-reject" onclick="genie.resolveApproval('${escapeJsQuoted(approvalId)}', 'rejected')">❌ Reject</button>
  </div>
</div>
      `;
        approvalsColumnListEl.appendChild(div);
        updateApprovalsColumnVisibility();

        // Batch-approve: after BATCH_APPROVE_DELAY_MS, if ≥2 pending approvals exist, show the banner.
        setTimeout(() => {
            if (pendingApprovals.has(approvalId) && pendingApprovals.size >= 2) {
                showOrUpdateBatchApproveBanner();
            }
        }, BATCH_APPROVE_DELAY_MS);
        // If the banner is already visible, update its count immediately.
        if (batchApproveBannerEl && pendingApprovals.size >= 2) {
            updateBatchApproveBannerCount();
        }

        if (hitlApprovalCount >= 3 && !hitlNudgeShown) {
            hitlNudgeShown = true;
            const nudgeEl = document.createElement('div');
            nudgeEl.className = 'flex justify-start mb-3';
            const safeTool = (toolName && toolName.trim()) ? toolName.trim() : 'tool_name';
            nudgeEl.innerHTML = `
<div class="approval-card" style="border-color:rgba(34,197,94,0.4);background:rgba(163,230,53,0.15);">
  <div style="font-size:0.8rem;color:#166534;">
    💡 <strong>Tip:</strong> To skip approval for this tool in the future, add it to <code style="background:rgba(0,0,0,0.08);color:#14532d;padding:0.1rem 0.3rem;border-radius:0.2rem;">[hitl]</code> <code style="background:rgba(0,0,0,0.08);color:#14532d;padding:0.1rem 0.3rem;border-radius:0.2rem;">always_allowed</code> in your config. Example: <code style="background:rgba(0,0,0,0.08);color:#14532d;padding:0.1rem 0.3rem;border-radius:0.2rem;font-size:0.75rem;">always_allowed = [\"${escapeHtml(safeTool)}\"]</code> — or use the <a href="https://stackgenhq.github.io/genie/config-builder.html" target="_blank" rel="noopener" style="color:#15803d;font-weight:600;">Config Builder</a>.
  </div>
</div>
      `;
            messagesEl.appendChild(nudgeEl);
        }
        scrollToBottom();
    }

    async function resolveApproval(approvalId, decision) {
        const actionsEl = document.getElementById('approval-actions-' + approvalId);
        if (!actionsEl) return;

        // Grab optional feedback
        const feedbackEl = document.getElementById('feedback-' + approvalId);
        const feedback = feedbackEl ? feedbackEl.value.trim() : '';

        // Allow-for duration and args filter (only when approving)
        let allowForMins = 0;
        let allowWhenArgsContain = [];
        if (decision === 'approved') {
            const allowForEl = document.getElementById('allow-for-' + approvalId);
            if (allowForEl) allowForMins = parseInt(allowForEl.value, 10) || 0;
            const allowArgsEl = document.getElementById('allow-args-' + approvalId);
            if (allowArgsEl && allowArgsEl.value.trim()) {
                allowWhenArgsContain = allowArgsEl.value.split(',').map(s => s.trim()).filter(Boolean);
            }
        }

        // Disable buttons immediately
        actionsEl.querySelectorAll('button').forEach(btn => btn.disabled = true);
        if (feedbackEl) feedbackEl.disabled = true;

        try {
            const body = { approvalId, decision, feedback };
            if (decision === 'approved' && (allowForMins > 0 || allowWhenArgsContain.length > 0)) {
                body.allowForMins = allowForMins;
                if (allowWhenArgsContain.length > 0) body.allowWhenArgsContain = allowWhenArgsContain;
            }
            const resp = await fetch(serverUrl + '/approve', {
                method: 'POST',
                headers: Object.assign({ 'Content-Type': 'application/json' }, aguiAuthHeaders()),
                body: JSON.stringify(body),
            });

            if (!resp.ok) {
                const err = await resp.text();
                throw new Error(err);
            }

            // Replace buttons with resolved status
            let statusHtml;
            if (decision === 'revisit') {
                statusHtml = '<span class="approval-resolved" style="color:#a78bfa">🔄 Sent back for revision</span>';
            } else if (decision === 'approved' && feedback) {
                statusHtml = '<span class="approval-resolved" style="color:#f59e0b">✏️ Approved with feedback</span>';
            } else if (decision === 'approved') {
                statusHtml = '<span class="approval-resolved" style="color:#16a34a">✅ Approved</span>';
            } else {
                statusHtml = '<span class="approval-resolved" style="color:#dc2626">❌ Rejected</span>';
            }
            if (feedback) {
                statusHtml += `<div style="font-size:0.7rem;color:rgba(255,255,255,0.5);margin-top:0.25rem">${escapeHtml(feedback)}</div>`;
            }
            actionsEl.innerHTML = statusHtml;
            // Hide the feedback textarea after resolution
            if (feedbackEl) feedbackEl.parentElement.style.display = 'none';
            // Remove from pending-approvals tracking and update batch-approve banner.
            pendingApprovals.delete(approvalId);
            refreshBatchApproveBanner();
            // Poof-dismiss from right column after 5 seconds
            setTimeout(() => {
                dismissWithPoof(document.getElementById('approval-' + approvalId)?.closest('.approvals-column-item'));
            }, 5000);
        } catch (err) {
            if (err.message && err.message.includes('not found or already resolved')) {
                // Approval was already handled (TTL expired or resolved elsewhere) — remove tile immediately
                const itemEl = document.getElementById('approval-' + approvalId)?.closest('.approvals-column-item');
                if (itemEl) itemEl.remove();
                updateApprovalsColumnVisibility();
            } else {
                actionsEl.innerHTML = '<span class="approval-resolved" style="color:#dc2626">⚠️ Error: ' + escapeHtml(err.message) + '</span>';
            }
        }
    }

    async function revisitApproval(approvalId) {
        const feedbackEl = document.getElementById('feedback-' + approvalId);
        let feedback = feedbackEl ? feedbackEl.value.trim() : '';

        if (!feedback) {
            feedback = prompt('What should the agent do differently?');
            if (!feedback) return; // User cancelled
        }

        // Use the existing resolveApproval with 'rejected' + feedback
        // so the LLM receives the error and re-plans
        const actionsEl = document.getElementById('approval-actions-' + approvalId);
        if (!actionsEl) return;

        actionsEl.querySelectorAll('button').forEach(btn => btn.disabled = true);
        if (feedbackEl) feedbackEl.disabled = true;

        try {
            const resp = await fetch(serverUrl + '/approve', {
                method: 'POST',
                headers: Object.assign({ 'Content-Type': 'application/json' }, aguiAuthHeaders()),
                body: JSON.stringify({ approvalId, decision: 'rejected', feedback }),
            });

            if (!resp.ok) {
                const err = await resp.text();
                throw new Error(err);
            }

            let statusHtml = '<span class="approval-resolved" style="color:#a78bfa">🔄 Sent back for revision</span>';
            statusHtml += `<div style="font-size:0.7rem;color:rgba(255,255,255,0.5);margin-top:0.25rem">${escapeHtml(feedback)}</div>`;
            actionsEl.innerHTML = statusHtml;
            if (feedbackEl) feedbackEl.parentElement.style.display = 'none';
            // Remove from pending-approvals tracking and update batch-approve banner.
            pendingApprovals.delete(approvalId);
            refreshBatchApproveBanner();
            setTimeout(() => {
                dismissWithPoof(document.getElementById('approval-' + approvalId)?.closest('.approvals-column-item'));
            }, 5000);
        } catch (err) {
            if (err.message && err.message.includes('not found or already resolved')) {
                const itemEl = document.getElementById('approval-' + approvalId)?.closest('.approvals-column-item');
                if (itemEl) itemEl.remove();
                updateApprovalsColumnVisibility();
            } else {
                actionsEl.innerHTML = '<span class="approval-resolved" style="color:#dc2626">⚠️ Error: ' + escapeHtml(err.message) + '</span>';
            }
        }
    }

    // ── Batch Approve All ──

    // Build a { toolName → [approvalId, …] } map from pendingApprovals.
    function groupPendingByToolName() {
        const groups = {};
        for (const [id, info] of pendingApprovals) {
            const name = info.toolName || 'tool';
            if (!groups[name]) groups[name] = [];
            groups[name].push(id);
        }
        return groups;
    }

    function showOrUpdateBatchApproveBanner() {
        // Always rebuild the banner content to reflect the latest groups.
        if (batchApproveBannerEl) {
            rebuildBatchBannerContent();
            return;
        }
        const count = pendingApprovals.size;
        if (count < 2) return;

        const banner = document.createElement('div');
        banner.id = 'batch-approve-banner';
        banner.className = 'approvals-column-item';
        banner.style.cssText = 'order:-1;';
        buildBatchBannerContent(banner);
        if (approvalsColumnListEl) {
            approvalsColumnListEl.insertBefore(banner, approvalsColumnListEl.firstChild);
        }
        batchApproveBannerEl = banner;
        updateApprovalsColumnVisibility();
    }

    function buildBatchBannerContent(container) {
        const groups = groupPendingByToolName();
        const total = pendingApprovals.size;
        const toolNames = Object.keys(groups);

        // Per-tool rows (only when there are multiple distinct tool names or ≥2 of any single tool).
        let toolRowsHtml = '';
        for (const name of toolNames) {
            const ids = groups[name];
            const n = ids.length;
            if (n < 1) continue;
            const safeIds = JSON.stringify(ids).replace(/'/g, '&#39;');
            toolRowsHtml += `
      <div style="display:flex;align-items:center;gap:0.5rem;padding:0.25rem 0;">
        <code style="background:rgba(255,255,255,0.08);padding:0.1rem 0.4rem;border-radius:0.25rem;font-size:0.75rem;color:#c4b5fd;">${escapeHtml(name)}</code>
        <span style="font-size:0.75rem;color:rgba(255,255,255,0.5);">×${n}</span>
        <button class="btn-approve" onclick="genie.approveByToolName('${escapeJsQuoted(name)}')" style="padding:0.2rem 0.5rem;font-size:0.7rem;">✅ Approve${n > 1 ? ' All' : ''} (${n})</button>
      </div>`;
        }

        container.innerHTML = `
<div class="approval-card" style="border-left:3px solid #818cf8;background:rgba(99,102,241,0.12);position:relative;">
  <div style="display:flex;align-items:center;justify-content:space-between;gap:0.5rem;">
    <span style="font-size:0.85rem;font-weight:600;color:#a5b4fc;">⚡ ${total} tool${total !== 1 ? 's' : ''} waiting</span>
    <button onclick="genie.dismissBatchBanner()" style="background:none;border:none;color:rgba(255,255,255,0.4);cursor:pointer;font-size:1rem;padding:0 0.25rem;" title="Dismiss" aria-label="Dismiss batch approve banner">×</button>
  </div>
  <div style="margin-top:0.35rem;">${toolRowsHtml}</div>
  <div style="margin-top:0.5rem;display:flex;gap:0.5rem;border-top:1px solid rgba(255,255,255,0.06);padding-top:0.5rem;">
    <button class="btn-approve" onclick="genie.approveAllPending()" style="flex:1;">✅ Approve All (${total})</button>
    <button class="btn-reject" onclick="genie.rejectAllPending()" style="flex:0;white-space:nowrap;">❌ Reject All</button>
  </div>
</div>
        `;
    }

    function rebuildBatchBannerContent() {
        if (!batchApproveBannerEl) return;
        buildBatchBannerContent(batchApproveBannerEl);
    }

    function refreshBatchApproveBanner() {
        if (pendingApprovals.size < 2) {
            removeBatchApproveBanner();
            return;
        }
        if (batchApproveBannerEl) {
            rebuildBatchBannerContent();
        }
    }

    function removeBatchApproveBanner() {
        if (batchApproveBannerEl) {
            batchApproveBannerEl.remove();
            batchApproveBannerEl = null;
            updateApprovalsColumnVisibility();
        }
    }

    function dismissBatchBanner() {
        removeBatchApproveBanner();
    }

    async function approveByToolName(toolName) {
        const ids = [];
        for (const [id, info] of pendingApprovals) {
            if (info.toolName === toolName) ids.push(id);
        }
        if (ids.length === 0) return;
        // Disable buttons on the matching row to prevent double-clicks.
        if (batchApproveBannerEl) {
            batchApproveBannerEl.querySelectorAll('button').forEach(btn => { btn.disabled = true; });
        }
        const results = await Promise.allSettled(ids.map(id => resolveApproval(id, 'approved')));
        results.forEach((r, i) => {
            if (r.status === 'rejected') {
                console.warn('Batch approve by tool failed for', ids[i], r.reason);
            }
        });
        refreshBatchApproveBanner();
    }

    async function approveAllPending() {
        const ids = Array.from(pendingApprovals.keys());
        if (ids.length === 0) return;
        if (batchApproveBannerEl) {
            batchApproveBannerEl.querySelectorAll('button').forEach(btn => { btn.disabled = true; });
        }
        const results = await Promise.allSettled(ids.map(id => resolveApproval(id, 'approved')));
        results.forEach((r, i) => {
            if (r.status === 'rejected') {
                console.warn('Batch approve failed for', ids[i], r.reason);
            }
        });
        removeBatchApproveBanner();
    }

    async function rejectAllPending() {
        const ids = Array.from(pendingApprovals.keys());
        if (ids.length === 0) return;
        if (batchApproveBannerEl) {
            batchApproveBannerEl.querySelectorAll('button').forEach(btn => { btn.disabled = true; });
        }
        const results = await Promise.allSettled(ids.map(id => resolveApproval(id, 'rejected')));
        results.forEach((r, i) => {
            if (r.status === 'rejected') {
                console.warn('Batch reject failed for', ids[i], r.reason);
            }
        });
        removeBatchApproveBanner();
    }

    // ── Clarification Card ──
    function addClarificationCard(requestId, question, context) {
        const div = document.createElement('div');
        div.className = 'flex justify-start approvals-column-item';
        const contextHtml = context
            ? `<div class="clarify-context">💡 ${renderMarkdown(context)}</div>`
            : '';
        div.innerHTML = `
<div class="clarify-card" id="clarify-${escapeAttr(requestId)}">
  <div class="clarify-header">❓ ${escapeHtml(agentDisplayName)} needs your input</div>
  <div class="clarify-question">${renderMarkdown(question || 'Please provide more information.')}</div>
  ${contextHtml}
  <textarea class="clarify-input" id="clarify-input-${escapeAttr(requestId)}" placeholder="Type your answer…" rows="2"
      onkeydown="if(event.key==='Enter'&&!event.shiftKey){event.preventDefault();genie.submitClarification('${escapeJsQuoted(requestId)}')}"></textarea>
  <div id="clarify-actions-${escapeAttr(requestId)}">
    <button type="button" class="btn-clarify-thumb" onclick="genie.submitClarificationWithAnswer('${escapeJsQuoted(requestId)}', 'yes')" title="Yes">👍</button>
    <button type="button" class="btn-clarify-thumb" onclick="genie.submitClarificationWithAnswer('${escapeJsQuoted(requestId)}', 'no')" title="No">👎</button>
    <button class="btn-submit-clarify" onclick="genie.submitClarification('${escapeJsQuoted(requestId)}')">📨 Submit Answer</button>
  </div>
</div>
      `;
        approvalsColumnListEl.appendChild(div);
        updateApprovalsColumnVisibility();
        scrollToBottom();
        // Auto-focus the input
        setTimeout(() => {
            const inp = document.getElementById('clarify-input-' + requestId);
            if (inp) inp.focus();
        }, 100);
    }

    async function submitClarificationWithAnswer(requestId, answer) {
        const inputEl = document.getElementById('clarify-input-' + requestId);
        const actionsEl = document.getElementById('clarify-actions-' + requestId);
        if (!actionsEl) return;

        inputEl && (inputEl.disabled = true);
        actionsEl.querySelectorAll('button').forEach(btn => btn.disabled = true);

        try {
            const resp = await fetch(serverUrl + '/api/v1/clarify', {
                method: 'POST',
                headers: Object.assign({ 'Content-Type': 'application/json' }, aguiAuthHeaders()),
                body: JSON.stringify({ requestId, answer }),
            });

            if (!resp.ok) {
                const err = await resp.text();
                throw new Error(err);
            }

            actionsEl.innerHTML = '<span class="clarify-resolved">✅ Answer submitted</span>';
            if (inputEl) inputEl.style.display = 'none';
            setTimeout(() => {
                dismissWithPoof(document.getElementById('clarify-' + requestId)?.closest('.approvals-column-item'));
            }, 5000);
        } catch (err) {
            actionsEl.innerHTML = '<span style="color:#dc2626;font-size:0.75rem;font-weight:600">⚠️ Error: ' + escapeHtml(err.message) + '</span>';
            if (inputEl) inputEl.disabled = false;
            actionsEl.querySelectorAll('button').forEach(btn => btn.disabled = false);
        }
    }

    async function submitClarification(requestId) {
        const inputEl = document.getElementById('clarify-input-' + requestId);
        const actionsEl = document.getElementById('clarify-actions-' + requestId);
        if (!inputEl || !actionsEl) return;

        const answer = inputEl.value.trim();
        if (!answer) {
            inputEl.style.borderColor = '#ef4444';
            inputEl.placeholder = 'Please type an answer before submitting…';
            return;
        }

        await submitClarificationWithAnswer(requestId, answer);
    }

    function addSystemMessage(text) {
        const div = document.createElement('div');
        div.className = 'flex justify-center mb-4';
        div.innerHTML = `<div class="chat-bubble system">${escapeHtml(text)}</div>`;
        messagesEl.appendChild(div);
        scrollToBottom();
    }

    function addErrorMessage(text) {
        const div = document.createElement('div');
        div.className = 'flex justify-center mb-4';
        div.innerHTML = `<div class="chat-bubble error">⚠️ ${escapeHtml(text)}</div>`;
        messagesEl.appendChild(div);
        scrollToBottom();
    }

    function finishStreaming() {
        isStreaming = false;
        abortController = null;
        document.getElementById('stop-btn').classList.remove('visible');
        inputEl.focus();
        hideThinking();
        finalizeAssistantBubble();
        updateButtonState();
    }

    function stopStreaming() {
        if (abortController) {
            abortController.abort();
        }
    }

    function scrollToBottom() {
        requestAnimationFrame(() => {
            messagesEl.scrollTop = messagesEl.scrollHeight;
        });
    }

    if (typeof ResizeObserver !== 'undefined' && messagesEl) {
        let lastHeight = messagesEl.clientHeight;
        const ro = new ResizeObserver(function () {
            const h = messagesEl.clientHeight;
            if (h < lastHeight) scrollToBottom();
            lastHeight = h;
        });
        ro.observe(messagesEl);
    }

    // ── Markdown Rendering (lightweight) ──
    // Renders markdown from assistant/user content. Escapes HTML first to prevent XSS, then applies safe substitutions.
    function renderMarkdown(text) {
        if (text == null || typeof text !== 'string') return '';
        // Escape entire string first so no raw < > & can become HTML/script; then apply markdown.
        text = escapeHtml(text);
        // Code blocks (content already escaped above)
        text = text.replace(/```(\w*)\n([\s\S]*?)```/g, function (_, lang, code) {
            return '<pre><code>' + code.trim() + '</code></pre>';
        });
        // Inline code
        text = text.replace(/`([^`]+)`/g, '<code>$1</code>');
        // Bold
        text = text.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
        // Italic
        text = text.replace(/\*([^*]+)\*/g, '<em>$1</em>');
        // Headers (h1–h3)
        text = text.replace(/^### (.+)$/gm, '<h3 style="font-size:1rem;font-weight:700;margin:0.75rem 0 0.25rem">$1</h3>');
        text = text.replace(/^## (.+)$/gm, '<h2 style="font-size:1.1rem;font-weight:700;margin:0.75rem 0 0.25rem">$1</h2>');
        text = text.replace(/^# (.+)$/gm, '<h1 style="font-size:1.25rem;font-weight:800;margin:0.75rem 0 0.25rem">$1</h1>');
        // Horizontal rule
        text = text.replace(/^---$/gm, '<hr style="border:none;border-top:1px solid rgba(0,0,0,0.15);margin:0.75rem 0">');
        // Lists: convert consecutive list lines into <ul>/<ol> blocks before converting remaining newlines to <br>.
        text = renderLists(text);
        // Line breaks (for remaining newlines not consumed by lists/headers)
        text = text.replace(/\n/g, '<br>');
        return text;
    }

    // Convert consecutive lines starting with "- ", "* ", or "N. " into proper HTML lists.
    function renderLists(text) {
        var lines = text.split('\n');
        var result = [];
        var i = 0;
        while (i < lines.length) {
            var line = lines[i];
            // Ordered list: line starts with "1. ", "2. ", etc.
            if (/^\d+\.\s/.test(line)) {
                var items = [];
                while (i < lines.length && /^\d+\.\s/.test(lines[i])) {
                    items.push(lines[i].replace(/^\d+\.\s/, ''));
                    i++;
                }
                result.push('<ol style="margin:0.25rem 0 0.25rem 1.25rem;padding:0;list-style:decimal">' +
                    items.map(function (it) { return '<li style="margin:0.15rem 0">' + it + '</li>'; }).join('') +
                    '</ol>');
                continue;
            }
            // Unordered list: line starts with "- " or "* " (but not bold **)
            if (/^[-*]\s/.test(line) && !/^\*\*/.test(line)) {
                var items = [];
                while (i < lines.length && /^[-*]\s/.test(lines[i]) && !/^\*\*/.test(lines[i])) {
                    items.push(lines[i].replace(/^[-*]\s/, ''));
                    i++;
                }
                result.push('<ul style="margin:0.25rem 0 0.25rem 1.25rem;padding:0;list-style:disc">' +
                    items.map(function (it) { return '<li style="margin:0.15rem 0">' + it + '</li>'; }).join('') +
                    '</ul>');
                continue;
            }
            result.push(line);
            i++;
        }
        return result.join('\n');
    }

    function escapeHtml(text) {
        if (text == null) return '';
        if (typeof text !== 'string') text = String(text);
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    // For use inside HTML attribute values (id, etc.). Escapes quotes and entities so the value cannot break out.
    function escapeAttr(text) {
        if (text == null) return '';
        if (typeof text !== 'string') text = String(text);
        return text
            .replace(/&/g, '&amp;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    // Produces a DOM-safe id fragment from an arbitrary string.
    // Unlike escapeAttr (which HTML-entity-encodes), this replaces characters
    // that are unsafe for DOM ids so the same canonical id can be used
    // consistently in both innerHTML templates and getElementById lookups.
    function safeDomId(text) {
        if (text == null) return '';
        if (typeof text !== 'string') text = String(text);
        return text.replace(/[^a-zA-Z0-9_-]/g, function (ch) {
            return '_' + ch.charCodeAt(0).toString(16) + '_';
        });
    }

    // For embedding in a single-quoted JS string inside an HTML attribute (e.g. onclick="genie.foo('${id}')").
    // Ensures the passed value is the raw string so getElementById(id) matches the decoded DOM id.
    function escapeJsQuoted(text) {
        if (text == null) return '';
        if (typeof text !== 'string') text = String(text);
        return text
            .replace(/\\/g, '\\\\')
            .replace(/'/g, "\\'")
            .replace(/\r/g, '\\r')
            .replace(/\n/g, '\\n');
    }

    // ── Copy Message ──
    function addCopyButton(bubbleEl, rawContent) {
        const parent = bubbleEl.parentElement;
        if (!parent || !parent.classList.contains('bubble-wrapper')) return;
        // Avoid adding duplicate buttons
        if (parent.querySelector('.copy-msg-btn')) return;

        const btn = document.createElement('button');
        btn.className = 'copy-msg-btn';
        btn.title = 'Copy message';
        btn.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>`;
        btn.addEventListener('click', () => copyMessageContent(btn, rawContent));
        parent.appendChild(btn);
    }

    function copyMessageContent(btn, text) {
        navigator.clipboard.writeText(text).then(() => {
            btn.classList.add('copied');
            btn.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
            setTimeout(() => {
                btn.classList.remove('copied');
                btn.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>`;
            }, 1500);
        }).catch(() => {
            // Fallback for older browsers
            const ta = document.createElement('textarea');
            ta.value = text;
            ta.style.position = 'fixed';
            ta.style.opacity = '0';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
            btn.classList.add('copied');
            btn.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>`;
            setTimeout(() => {
                btn.classList.remove('copied');
                btn.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/></svg>`;
            }, 1500);
        });
    }

    function addSpeakButton(bubbleEl, rawContent) {
        const parent = bubbleEl.parentElement;
        if (!parent || !parent.classList.contains('bubble-wrapper')) return;
        if (parent.querySelector('.speak-msg-btn')) return;
        if (!window.speechSynthesis) return;
        if (!rawContent || !String(rawContent).trim()) return;
        const btn = document.createElement('button');
        btn.className = 'speak-msg-btn';
        btn.title = 'Read aloud';
        btn.setAttribute('aria-label', 'Read this message aloud');
        btn.innerHTML = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 010 7.07"/><path d="M19.07 4.93a10 10 0 010 14.14"/></svg>`;
        btn.addEventListener('click', () => toggleSpeakMessage(btn, rawContent));
        parent.appendChild(btn);
    }

    // Strip markdown to plain text for TTS. Inline implementation to avoid a build step;
    // CDN libs (remove-markdown, markdown-to-txt) are CommonJS/ESM and need bundling.
    function stripMarkdownForSpeech(text) {
        if (!text || !text.trim()) return '';
        var t = text.trim()
            .replace(/```[\s\S]*?```/g, ' ')
            .replace(/`[^`]+`/g, ' ')
            .replace(/\*\*([^*]+)\*\*/g, '$1')
            .replace(/\*([^*]+)\*/g, '$1')
            .replace(/__([^_]+)__/g, '$1')
            .replace(/_([^_]+)_/g, '$1')
            .replace(/#{1,6}\s*/g, '')
            .replace(/^[\s\t]*[\*\-+]|\d+\.\s+/gm, ' ')
            .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
            .replace(/!\[([^\]]*)\][^\s]*/g, '$1')
            .replace(/\n+/g, ' ')
            .replace(/\s+/g, ' ')
            .trim();
        return t;
    }

    function toggleSpeakMessage(btn, text) {
        if (!text || !text.trim()) return;
        if (btn.classList.contains('speaking')) {
            window.speechSynthesis.cancel();
            btn.classList.remove('speaking');
            btn.title = 'Read aloud';
            return;
        }
        document.querySelectorAll('.speak-msg-btn.speaking').forEach(b => { b.classList.remove('speaking'); b.title = 'Read aloud'; });
        window.speechSynthesis.cancel();
        var plainText = stripMarkdownForSpeech(text);
        if (!plainText) return;
        const utterance = new SpeechSynthesisUtterance(plainText);
        utterance.lang = document.documentElement.lang || 'en-US';
        utterance.onend = () => { btn.classList.remove('speaking'); btn.title = 'Read aloud'; };
        utterance.onerror = () => { btn.classList.remove('speaking'); btn.title = 'Read aloud'; };
        btn.classList.add('speaking');
        btn.title = 'Stop reading';
        window.speechSynthesis.speak(utterance);
    }

    // ── Input Handling ──
    function handleKeyDown(e) {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            sendMessage();
        }
    }

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') {
            if (document.fullscreenElement || document.webkitFullscreenElement) return;
            if (isStreaming) { e.preventDefault(); stopStreaming(); }
            if (window.speechSynthesis && window.speechSynthesis.speaking) {
                window.speechSynthesis.cancel();
                document.querySelectorAll('.speak-msg-btn.speaking').forEach(b => { b.classList.remove('speaking'); b.title = 'Read aloud'; });
            }
            return;
        }
        if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
            e.preventDefault();
            if (inputEl && !inputEl.disabled) inputEl.focus();
        }
    });

    function autoResize(el) {
        el.style.height = 'auto';
        el.style.height = Math.min(el.scrollHeight, 120) + 'px';
    }

    function usePrompt(text) {
        const input = document.getElementById('chat-input');
        // Fill the input with the selected prompt text. If the input is disabled
        // (e.g., when disconnected), we still populate it but only focus when enabled.
        input.value = text;
        autoResize(input);
        if (!input.disabled) {
            input.focus();
        }
    }

    // ── Sidebar & History ──
    function toggleSidebar() {
        document.getElementById('history-sidebar').classList.toggle('open');
        document.getElementById('sidebar-overlay').classList.toggle('open');
    }

    async function refreshSidebar() {
        try {
            const all = await genieDB.getAllConversations();
            const listEl = document.getElementById('sidebar-list');

            if (all.length === 0) {
                listEl.innerHTML = '<div class="sidebar-empty">No conversations yet</div>';
                return;
            }

            // Group by serverUrl
            const groups = {};
            for (const c of all) {
                const key = c.serverUrl || 'Unknown';
                if (!groups[key]) groups[key] = [];
                groups[key].push(c);
            }

            let html = '';
            for (const [url, convs] of Object.entries(groups)) {
                const label = url.replace(/^https?:\/\//, '');
                html += `<div class="sidebar-server-group">`;
                html += `<div class="sidebar-server-label">${escapeHtml(label)}</div>`;
                for (const c of convs) {
                    const active = c.threadId === threadId ? ' active' : '';
                    const timeStr = formatTimeAgo(c.updatedAt);
                    html += `<div class="sidebar-conv${active}" onclick="genie.loadConversation('${escapeJsQuoted(c.threadId)}')">`;
                    html += `<div class="sidebar-conv-info">`;
                    html += `<div class="sidebar-conv-title">${escapeHtml(c.title || 'Untitled')}</div>`;
                    html += `<div class="sidebar-conv-time">${timeStr}</div>`;
                    html += `</div>`;
                    html += `<button class="sidebar-conv-delete" onclick="event.stopPropagation(); genie.deleteConversation('${escapeJsQuoted(c.threadId)}')" title="Delete">🗑️</button>`;
                    html += `</div>`;
                }
                html += `</div>`;
            }
            listEl.innerHTML = html;
        } catch (err) {
            console.warn('Failed to refresh sidebar:', err);
        }
    }

    function formatTimeAgo(ts) {
        if (!ts) return '';
        const diff = Date.now() - ts;
        const mins = Math.floor(diff / 60000);
        if (mins < 1) return 'Just now';
        if (mins < 60) return mins + 'm ago';
        const hours = Math.floor(mins / 60);
        if (hours < 24) return hours + 'h ago';
        const days = Math.floor(hours / 24);
        if (days < 7) return days + 'd ago';
        return new Date(ts).toLocaleDateString();
    }

    async function loadConversation(convThreadId) {
        try {
            const conv = await genieDB.getConversation(convThreadId);
            if (!conv) return;

            // Switch to this conversation
            threadId = conv.threadId;
            currentConversation = conv;
            const prevServerUrl = serverUrl;
            serverUrl = conv.serverUrl || serverUrl;
            if (conv.serverUrl && conv.serverUrl !== prevServerUrl && aguiPasswordForSession) {
                aguiPasswordForSession = '';
                addSystemMessage('Switched to a different server. Re-enter the password in the connect area if this server requires one.');
            }

            // Clear messages area and pending approvals column
            messagesEl.innerHTML = '';
            if (approvalsColumnListEl) {
                approvalsColumnListEl.innerHTML = '';
                updateApprovalsColumnVisibility();
            }
            emptyState.style.display = 'none';
            currentAssistantBubble = null;
            currentAssistantContent = '';

            // Replay messages
            for (const msg of conv.messages) {
                if (msg.role === 'user') {
                    addUserBubble(msg.content);
                } else if (msg.role === 'assistant') {
                    const wrapper = document.createElement('div');
                    wrapper.className = 'flex justify-start mb-4';
                    const bubbleWrap = document.createElement('div');
                    bubbleWrap.className = 'bubble-wrapper';
                    const bubble = document.createElement('div');
                    bubble.className = 'chat-bubble assistant';
                    bubble.innerHTML = renderMarkdown(msg.content);
                    bubbleWrap.appendChild(bubble);
                    addCopyButton(bubble, msg.content);
                    addSpeakButton(bubble, msg.content);
                    wrapper.appendChild(bubbleWrap);
                    messagesEl.appendChild(wrapper);
                } else if (msg.role === 'tool') {
                    const div = document.createElement('div');
                    div.className = 'flex justify-start mb-3';
                    const friendly = friendlyToolName(msg.toolName || '');
                    div.innerHTML = `
                    <details class="tool-card">
                        <summary>
                            <span class="tool-label">${friendly}</span>
                            <span class="tool-status done">Done ✓</span>
                            <span class="tool-chevron">▶</span>
                        </summary>
                        <div class="tool-body">
                            <div class="tool-result">${escapeHtml(msg.content || '')}</div>
                        </div>
                    </details>`;
                    messagesEl.appendChild(div);
                } else if (msg.role === 'system') {
                    addSystemMessage(msg.content);
                }
            }

            scrollToBottom();
            toggleSidebar();
            await refreshSidebar();
            updateButtonState();
        } catch (err) {
            console.warn('Failed to load conversation:', err);
        }
    }

    async function deleteConversation(convThreadId) {
        try {
            await genieDB.deleteConversation(convThreadId);
            if (convThreadId === threadId) {
                startNewChat();
            }
            await refreshSidebar();
        } catch (err) {
            console.warn('Failed to delete conversation:', err);
        }
    }

    function exportChat() {
        if (!currentConversation || !currentConversation.messages.length) {
            alert('No conversation to export');
            return;
        }

        const history = currentConversation.messages.map(m => {
            const role = m.role.toUpperCase();
            const content = m.content || '';
            return `### ${role}\n\n${content}\n`;
        }).join('\n---\n\n');

        const blob = new Blob([history], { type: 'text/markdown' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `genie-chat-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.md`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    function summarizeChat() {
        if (!isConnected) {
            alert('Please connect to Genie first');
            return;
        }
        if (isStreaming) return;

        // Visual feedback
        addSystemMessage('📝 Summarizing conversation…');
        const btn = document.getElementById('btn-summarize');
        const origLabel = btn.textContent;
        btn.textContent = '⏳ Summarizing…';
        btn.disabled = true;

        const input = document.getElementById('chat-input');
        input.value = "Please summarize our conversation so far in a concise manner, highlighting key decisions and next steps.";
        sendMessage();

        // Restore button label after streaming ends (poll for completion)
        const restore = setInterval(() => {
            if (!isStreaming) {
                btn.textContent = origLabel;
                updateButtonState();
                clearInterval(restore);
            }
        }, 500);
    }

    function startNewChat() {
        // Cancel any active SSE stream from the previous run
        stopStreaming();
        finishStreaming();

        // Every new chat gets a completely fresh threadId so the
        // backend never carries over memory/context from a prior chat.
        threadId = crypto.randomUUID();
        runCounter = 0;
        currentAssistantBubble = null;
        currentAssistantContent = '';
        currentMessageId = null;

        // Revoke any object URLs used in the previous conversation to prevent memory leaks
        for (var i = 0; i < chatHistoryBlobURLs.length; i++) {
            URL.revokeObjectURL(chatHistoryBlobURLs[i]);
        }
        chatHistoryBlobURLs = [];

        // Clear messages and pending approvals column
        messagesEl.innerHTML = '';
        if (approvalsColumnListEl) {
            approvalsColumnListEl.innerHTML = '';
            updateApprovalsColumnVisibility();
        }

        // Start a new conversation record with segmentation metadata
        if (isConnected && serverUrl) {
            currentConversation = {
                threadId: threadId,
                serverUrl: serverUrl,
                userId: userId,
                platform: PLATFORM_ID,
                title: 'New conversation',
                messages: [],
                updatedAt: Date.now(),
            };
            emptyState.style.display = 'none';
        } else {
            currentConversation = null;
            messagesEl.appendChild(emptyState);
            emptyState.style.display = '';
        }

        // Close sidebar if open
        document.getElementById('history-sidebar').classList.remove('open');
        document.getElementById('sidebar-overlay').classList.remove('open');

        refreshSidebar().catch(console.warn);
        updateButtonState();
        inputEl.focus();
    }

    function updateButtonState() {
        const hasMessages = currentConversation && currentConversation.messages && currentConversation.messages.length > 0;
        const canSummarize = isConnected && !isStreaming && hasMessages;

        const btnSummarize = document.getElementById('btn-summarize');
        const btnExport = document.getElementById('btn-export');
        const btnNewChat = document.getElementById('btn-new-chat');

        if (btnSummarize) btnSummarize.disabled = !canSummarize;
        if (btnExport) btnExport.disabled = !hasMessages;
        if (btnNewChat) btnNewChat.disabled = !hasMessages && !currentConversation;
        if (attachBtn) attachBtn.disabled = !isConnected;
    }

    // Initialize vibrate toggle label from localStorage on load
    updateVibrateToggleLabel();

    // If page was opened with ?url=... or ?genie_url=..., connect to that Genie instance (one-click login).
    // Supports ?token=JWT for one-click JWT auth (e.g. ?url=https://genie.example.com&token=eyJhbG...).
    // Remove the query params from the address bar so the URL is not stored in history or sent in Referer.
    (function tryConnectFromQueryParam() {
        const params = new URLSearchParams(window.location.search);
        const genieUrl = params.get('url') || params.get('genie_url');
        if (!genieUrl) return;
        let decoded;
        try {
            decoded = decodeURIComponent(genieUrl.trim());
        } catch (e) {
            decoded = genieUrl.trim();
        }
        // Check for a JWT token in the query params.
        const queryToken = params.get('token') || params.get('jwt');
        if (queryToken) {
            try {
                aguiTokenForSession = decodeURIComponent(queryToken.trim());
            } catch (e) {
                aguiTokenForSession = queryToken.trim();
            }
        }
        const input = document.getElementById('endpoint-input');
        if (input && decoded) {
            input.value = decoded;
            if (typeof history.replaceState === 'function') {
                const cleanUrl = window.location.pathname || '/';
                history.replaceState(null, '', cleanUrl);
            }
            connectToServer();
        }
    })();

    // Expose handlers under window.genie for chat.html and dynamically generated onclick attributes.
    // State (serverUrl, aguiPasswordForSession, etc.) remains in closure and is not accessible from outside.
    if (connectionBadge && !connectionBadge._genieClickHandlerAttached) {
        connectionBadge.addEventListener('click', function () {
            if (!isConnected && !isReconnecting && serverUrl) {
                triggerReconnect();
            }
        });
        connectionBadge._genieClickHandlerAttached = true;
    }

    window.genie = {
        toggleSidebar: toggleSidebar,
        startNewChat: startNewChat,
        triggerReconnect: triggerReconnect,
        summarizeChat: summarizeChat,
        exportChat: exportChat,
        toggleFullscreen: toggleFullscreen,
        enableNotifications: enableNotifications,
        dismissNotificationPrompt: dismissNotificationPrompt,
        connectToServer: connectToServer,
        connectWithPassword: connectWithPassword,
        connectWithToken: connectWithToken,
        connectWithActiveAuth: connectWithActiveAuth,
        loginWithGoogle: loginWithGoogle,
        closeAuthModal: closeAuthModal,
        closePasswordModal: closeAuthModal,
        switchAuthTab: switchAuthTab,
        usePrompt: usePrompt,
        handleKeyDown: handleKeyDown,
        autoResize: autoResize,
        toggleMic: toggleMic,
        sendMessage: sendMessage,
        stopStreaming: stopStreaming,
        toggleVibratePreference: toggleVibratePreference,
        resolveApproval: resolveApproval,
        revisitApproval: revisitApproval,
        approveAllPending: approveAllPending,
        rejectAllPending: rejectAllPending,
        approveByToolName: approveByToolName,
        dismissBatchBanner: dismissBatchBanner,
        submitClarification: submitClarification,
        submitClarificationWithAnswer: submitClarificationWithAnswer,
        loadConversation: loadConversation,
        deleteConversation: deleteConversation,
        handleFileSelect: handleFileSelect,
        removeAttachment: removeAttachment,
    };
})();

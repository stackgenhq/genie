/**
 * Genie Config Builder
 * Schema-driven form → TOML/YAML output, zero dependencies.
 *
 * Structure:
 *   1. State & Constants
 *   2. DOM Helpers
 *   3. Field Builders     (fieldText, fieldNumber, fieldSelect, fieldToggle)
 *   4. Section Renderers  (one per config section)
 *   5. TOML Serializers   (one per config section)
 *   6. YAML Serializers   (one per config section)
 *   7. Actions            (toggle, copy, download, format switch)
 *   8. Init
 */
(function () {
    'use strict';

    /* ================================================================
     * 1. STATE & CONSTANTS
     * ================================================================ */

    var state = {
        format: 'toml',
        providers: [{ provider: 'openai', model_name: 'gpt-5.2', variant: 'default', token: 'OPENAI_API_KEY', good_for_task: 'efficiency' }],


        skills_roots: ['./skills'],
        mcp_servers: [],
        web_search: { provider: 'duckduckgo', google_api_key: 'GOOGLE_API_KEY', google_cx: 'GOOGLE_CSE_ID', bing_api_key: 'BING_API_KEY' },
        vector_memory: { persistence_dir: '', embedding_provider: 'dummy', api_key: 'OPENAI_API_KEY', ollama_url: '', ollama_model: '' },
        messenger: {
            platform: '', buffer_size: 100,
            slack: { app_token: 'SLACK_APP_TOKEN', bot_token: 'SLACK_BOT_TOKEN' },
            discord: { bot_token: 'DISCORD_BOT_TOKEN' },
            telegram: { token: 'TELEGRAM_BOT_TOKEN' },
            teams: { app_id: 'TEAMS_APP_ID', app_password: 'TEAMS_APP_PASSWORD', listen_addr: ':3978' },
            googlechat: { credentials_file: '', listen_addr: ':8080' },
            whatsapp: { access_token: 'WHATSAPP_ACCESS_TOKEN', phone_number_id: 'WHATSAPP_PHONE_NUMBER_ID', app_secret: 'WHATSAPP_APP_SECRET', verify_token: 'WHATSAPP_VERIFY_TOKEN', listen_addr: ':8443' }
        },
        scm: { provider: '', token: 'SCM_TOKEN', base_url: '' },
        pm: { provider: '', api_token: 'PM_API_TOKEN', base_url: '', email: '' },
        browser: { blocked_domains: [] },
        email: { provider: '', host: '', port: 587, username: '', password: '', imap_host: '', imap_port: 993 },
        hitl: { read_only_tools: [] },
        agui: { port: 8080, cors_origins: ['https://appcd-dev.github.io'], rate_limit: 0.5, rate_burst: 3, max_concurrent: 5, max_body_bytes: 1048576 }
    };

    var PROVIDERS = ['openai', 'gemini', 'anthropic'];
    var MODELS_BY_PROVIDER = {
        openai: ['gpt-5.3-codex', 'gpt-5.2', 'gpt-4.1', 'gpt-4.1-mini', 'gpt-4.1-nano', 'o3', 'o4-mini'],
        gemini: ['gemini-3-pro', 'gemini-3-flash-preview', 'gemini-2.5-pro', 'gemini-2.5-flash'],
        anthropic: ['claude-opus-4.6', 'claude-sonnet-4.5', 'claude-haiku-4.5', 'claude-sonnet-4', 'claude-opus-4']
    };
    var TASK_TYPES = ['tool_calling', 'planning', 'terminal_calling', 'scientific_reasoning',
        'novel_reasoning', 'general_task', 'mathematical', 'long_horizon_autonomy', 'efficiency'];
    var MCP_TRANSPORTS = ['stdio', 'streamable_http', 'sse'];
    var EMBED_PROVIDERS = ['dummy', 'openai', 'ollama'];
    var PLATFORMS = ['', 'slack', 'discord', 'telegram', 'teams', 'googlechat', 'whatsapp'];

    var SEARCH_PROVIDERS = ['duckduckgo', 'google', 'bing'];
    var SCM_PROVIDERS = ['', 'github', 'gitlab', 'bitbucket'];
    var PM_PROVIDERS = ['', 'jira', 'linear', 'asana'];

    /* ================================================================
     * 2. DOM HELPERS
     * ================================================================ */

    function $(id) { return document.getElementById(id); }

    /** Create a DOM element with optional attributes and children. */
    function el(tag, attrs, children) {
        var e = document.createElement(tag);
        if (attrs) Object.keys(attrs).forEach(function (k) {
            if (k === 'className') e.className = attrs[k];
            else if (k.indexOf('on') === 0) e.addEventListener(k.slice(2).toLowerCase(), attrs[k]);
            else e.setAttribute(k, attrs[k]);
        });
        if (children) {
            if (typeof children === 'string') e.innerHTML = children;
            else if (Array.isArray(children)) children.forEach(function (c) { if (c) e.appendChild(c); });
            else e.appendChild(children);
        }
        return e;
    }

    /** TOML-safe quote a string value. */
    function q(s) {
        return '"' + (s || '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
    }

    /** YAML-safe quote — only wraps when the value contains special chars. */
    function yq(s) {
        if (!s) return '""';
        if (/[:{}\[\],&*?|>!%#@`'"]/.test(s) || s.indexOf('${') !== -1)
            return '"' + s.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
        return s;
    }

    /** Check if a string array has at least one truthy value. */
    function hasItems(arr) {
        return arr && arr.length > 0 && arr.some(Boolean);
    }

    /* ================================================================
     * 3. FIELD BUILDERS
     * ================================================================ */

    function fieldText(label, value, onChange, placeholder, tooltip) {
        var wrapper = el('div', {});
        var labelHtml = label;
        if (tooltip) labelHtml += '<span class="form-tooltip">' + tooltip + '</span>';
        wrapper.appendChild(el('label', { className: 'form-label' }, labelHtml));
        var inp = el('input', { className: 'form-input', type: 'text', value: value || '', placeholder: placeholder || '' });
        inp.addEventListener('input', function () { onChange(this.value); });
        wrapper.appendChild(inp);
        return wrapper;
    }

    function fieldNumber(label, value, onChange, min, max, tooltip) {
        var wrapper = el('div', {});
        var labelHtml = label;
        if (tooltip) labelHtml += '<span class="form-tooltip">' + tooltip + '</span>';
        wrapper.appendChild(el('label', { className: 'form-label' }, labelHtml));
        var inp = el('input', { className: 'form-input', type: 'number', value: String(value != null ? value : ''), min: String(min), max: String(max) });
        inp.addEventListener('input', function () { onChange(parseInt(this.value, 10) || 0); });
        wrapper.appendChild(inp);
        return wrapper;
    }

    function fieldSelect(label, value, options, onChange, tooltip) {
        var wrapper = el('div', {});
        var labelHtml = label;
        if (tooltip) labelHtml += '<span class="form-tooltip">' + tooltip + '</span>';
        wrapper.appendChild(el('label', { className: 'form-label' }, labelHtml));
        var sel = el('select', { className: 'form-select' });
        options.forEach(function (opt) {
            var o = el('option', { value: opt }, opt || '(disabled)');
            if (opt === value) o.selected = true;
            sel.appendChild(o);
        });
        sel.addEventListener('change', function () { onChange(this.value); });
        wrapper.appendChild(sel);
        return wrapper;
    }

    function fieldToggle(label, value, onChange, tooltip) {
        var wrapper = el('div', {});
        var labelHtml = label;
        if (tooltip) labelHtml += '<span class="form-tooltip">' + tooltip + '</span>';
        wrapper.appendChild(el('label', { className: 'form-label' }, labelHtml));
        var toggle = el('div', { className: 'toggle' + (value ? ' active' : '') });
        toggle.addEventListener('click', function () {
            var newVal = !this.classList.contains('active');
            this.classList.toggle('active');
            onChange(newVal);
        });
        wrapper.appendChild(el('div', { className: 'toggle-wrapper mt-1' }, [toggle]));
        return wrapper;
    }

    /** Field for environment variable references (secrets). Stores just the var name, outputs ${NAME}. */
    function fieldEnvVar(label, value, onChange, placeholder, tooltip) {
        var wrapper = el('div', {});
        var labelHtml = label + ' <span class="text-gray-400 font-normal">(env var)</span>';
        if (tooltip) labelHtml += '<span class="form-tooltip">' + tooltip + '</span>';
        wrapper.appendChild(el('label', { className: 'form-label' }, labelHtml));
        var row = el('div', { className: 'flex items-center gap-1' });
        row.appendChild(el('span', { className: 'text-gray-400 font-mono text-sm' }, '${'));
        var inp = el('input', { className: 'form-input font-mono', type: 'text', value: value || '', placeholder: placeholder || 'ENV_VAR_NAME' });
        inp.addEventListener('input', function () { onChange(this.value); });
        row.appendChild(inp);
        row.appendChild(el('span', { className: 'text-gray-400 font-mono text-sm' }, '}'));
        wrapper.appendChild(row);
        return wrapper;
    }

    /* ================================================================
     * 4. SECTION RENDERERS
     * ================================================================ */

    function renderAll() {
        renderProviders();

        renderSkills();
        renderMCP();
        renderWebSearch();
        renderVectorMemory();
        renderMessenger();
        renderSCM();
        renderPM();
        renderBrowser();
        renderEmail();
        renderHITL();
        renderAGUI();
        renderOutput();
    }

    // ── Model Providers ──
    function renderProviders() {
        var c = $('providers-body');
        if (!c) return;
        c.innerHTML = '';
        state.providers.forEach(function (p, i) {
            c.appendChild(buildProviderCard(p, i));
        });
        c.appendChild(
            el('button', { className: 'btn-add mt-2', onClick: addProvider }, '+ Add Provider')
        );
    }

    function buildProviderCard(p, i) {
        var models = MODELS_BY_PROVIDER[p.provider] || MODELS_BY_PROVIDER.openai;
        return el('div', { className: 'repeatable-item' }, [
            el('div', { className: 'flex items-center justify-between mb-3' }, [
                el('span', { className: 'text-sm font-semibold text-gray-600' }, 'Provider #' + (i + 1)),
                el('button', { className: 'btn-remove', onClick: function () { state.providers.splice(i, 1); renderAll(); } }, '✕')
            ]),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
                fieldSelect('Provider', p.provider, PROVIDERS, function (v) {
                    p.provider = v;
                    p.model_name = (MODELS_BY_PROVIDER[v] || MODELS_BY_PROVIDER.openai)[0];
                    p.token = v.toUpperCase() + '_API_KEY';
                    renderAll();
                }, 'The AI company that runs this model — each has different strengths'),
                fieldSelect('Model Name', p.model_name, models, function (v) { p.model_name = v; renderOutput(); }, 'The specific AI model to use — bigger models are smarter but slower'),
                fieldText('Variant', p.variant, function (v) { p.variant = v; renderOutput(); }, 'e.g. default', 'Usually "default" — use "azure" if you host OpenAI on Microsoft Azure'),
                fieldEnvVar('Token', p.token, function (v) { p.token = v; renderOutput(); }, 'OPENAI_API_KEY', 'The environment variable name holding your API key — keeps secrets out of config files'),
                fieldSelect('Good For Task', p.good_for_task, TASK_TYPES, function (v) { p.good_for_task = v; renderOutput(); }, 'What this model is best at — Genie routes tasks to the best-fit model')
            ])
        ]);
    }

    function addProvider() {
        state.providers.push({ provider: 'gemini', model_name: 'gemini-3-pro-preview', variant: 'default', token: 'GEMINI_API_KEY', good_for_task: 'tool_calling' });
        renderAll();
    }





    // ── Skills ──
    function renderSkills() {
        var c = $('skills-body');
        if (!c) return;
        c.innerHTML = '';
        state.skills_roots.forEach(function (s, i) {
            c.appendChild(buildSkillRow(s, i));
        });
        c.appendChild(
            el('button', { className: 'btn-add mt-1', onClick: function () { state.skills_roots.push(''); renderAll(); } }, '+ Add Path')
        );
    }

    function buildSkillRow(value, i) {
        var inp = el('input', { className: 'form-input', type: 'text', value: value, placeholder: './skills or https://...' });
        inp.addEventListener('input', function () { state.skills_roots[i] = this.value; renderOutput(); });
        return el('div', { className: 'flex items-center gap-2 mb-2' }, [
            inp,
            el('button', { className: 'btn-remove', onClick: function () { state.skills_roots.splice(i, 1); renderAll(); } }, '✕')
        ]);
    }

    // ── MCP Servers ──
    function renderMCP() {
        var c = $('mcp-body');
        if (!c) return;
        c.innerHTML = '';
        state.mcp_servers.forEach(function (srv, i) {
            c.appendChild(buildMCPCard(srv, i));
        });
        c.appendChild(
            el('button', { className: 'btn-add mt-2', onClick: addMCPServer }, '+ Add MCP Server')
        );
    }

    function buildMCPCard(srv, i) {
        var connectionField = srv.transport === 'stdio'
            ? fieldText('Command', srv.command || '', function (v) { srv.command = v; renderOutput(); }, 'npx @org/mcp-server')
            : fieldText('Server URL', srv.server_url || '', function (v) { srv.server_url = v; renderOutput(); }, 'https://mcp.example.com');

        return el('div', { className: 'repeatable-item' }, [
            el('div', { className: 'flex items-center justify-between mb-3' }, [
                el('span', { className: 'text-sm font-semibold text-gray-600' }, 'Server #' + (i + 1) + (srv.name ? ' — ' + srv.name : '')),
                el('button', { className: 'btn-remove', onClick: function () { state.mcp_servers.splice(i, 1); renderAll(); } }, '✕')
            ]),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
                fieldText('Name', srv.name, function (v) { srv.name = v; renderOutput(); }, 'my-server', 'A friendly name to identify this tool server'),
                fieldSelect('Transport', srv.transport, MCP_TRANSPORTS, function (v) { srv.transport = v; renderAll(); }, 'How to connect — stdio runs a local program, HTTP/SSE connects to a remote server'),
                connectionField,
                fieldText('Args (comma-separated)', (srv.args || []).join(', '), function (v) { srv.args = splitCSV(v); renderOutput(); }, '--port 8080', 'Extra arguments passed to the command (stdio only)'),
                fieldText('Include Tools (comma-sep)', (srv.include_tools || []).join(', '), function (v) { srv.include_tools = splitCSV(v); renderOutput(); }, null, 'Only use these specific tools from the server (leave empty for all)'),
                fieldText('Exclude Tools (comma-sep)', (srv.exclude_tools || []).join(', '), function (v) { srv.exclude_tools = splitCSV(v); renderOutput(); }, null, 'Block these tools from being used — useful for restricting dangerous operations')
            ])
        ]);
    }

    function addMCPServer() {
        state.mcp_servers.push({ name: '', transport: 'stdio', command: '', server_url: '', args: [], include_tools: [], exclude_tools: [] });
        renderAll();
    }

    function splitCSV(v) {
        return v ? v.split(',').map(function (s) { return s.trim(); }) : [];
    }

    // ── Web Search ──
    function renderWebSearch() {
        var c = $('websearch-body');
        if (!c) return;
        c.innerHTML = '';
        var ws = state.web_search;
        var fields = [
            fieldSelect('Provider', ws.provider, SEARCH_PROVIDERS, function (v) { ws.provider = v; renderAll(); })
        ];
        if (ws.provider === 'google') {
            fields.push(fieldEnvVar('Google API Key', ws.google_api_key, function (v) { ws.google_api_key = v; renderOutput(); }, 'GOOGLE_API_KEY'));
            fields.push(fieldEnvVar('Google CX', ws.google_cx, function (v) { ws.google_cx = v; renderOutput(); }, 'GOOGLE_CSE_ID'));
        } else if (ws.provider === 'bing') {
            fields.push(fieldEnvVar('Bing API Key', ws.bing_api_key, function (v) { ws.bing_api_key = v; renderOutput(); }, 'BING_API_KEY'));
        }
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── Vector Memory ──
    function renderVectorMemory() {
        var c = $('vector-body');
        if (!c) return;
        c.innerHTML = '';
        var vm = state.vector_memory;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldSelect('Embedding Provider', vm.embedding_provider, EMBED_PROVIDERS, function (v) { vm.embedding_provider = v; renderAll(); }, 'How Genie creates memory embeddings — OpenAI is best quality, Ollama is free/local'),
            fieldText('Persistence Dir', vm.persistence_dir, function (v) { vm.persistence_dir = v; renderOutput(); }, './data/vectors', 'Where to save memory on disk — leave empty for in-memory only (lost on restart)'),
            vm.embedding_provider === 'openai' ? fieldEnvVar('API Key', vm.api_key, function (v) { vm.api_key = v; renderOutput(); }, 'OPENAI_API_KEY', 'OpenAI API key used to generate text embeddings') : null,
            vm.embedding_provider === 'ollama' ? fieldText('Ollama URL', vm.ollama_url, function (v) { vm.ollama_url = v; renderOutput(); }, 'http://localhost:11434', 'Address of your locally running Ollama server') : null,
            vm.embedding_provider === 'ollama' ? fieldText('Ollama Model', vm.ollama_model, function (v) { vm.ollama_model = v; renderOutput(); }, 'nomic-embed-text', 'Which Ollama model to use for embeddings') : null
        ]));
    }

    // ── Messenger ──
    function renderMessenger() {
        var c = $('messenger-body');
        if (!c) return;
        c.innerHTML = '';
        var m = state.messenger;
        var fields = [
            fieldSelect('Platform', m.platform, PLATFORMS, function (v) { m.platform = v; renderAll(); }, 'Which chat app to connect Genie to — leave empty to disable messaging'),
            fieldNumber('Buffer Size', m.buffer_size, function (v) { m.buffer_size = v; renderOutput(); }, 1, 10000, 'How many incoming messages to queue — increase for busy team channels')
        ];
        if (m.platform === 'slack') {
            fields.push(fieldEnvVar('App Token', m.slack.app_token, function (v) { m.slack.app_token = v; renderOutput(); }, 'SLACK_APP_TOKEN', 'Slack App-Level Token (starts with xapp-) — enables real-time Socket Mode'));
            fields.push(fieldEnvVar('Bot Token', m.slack.bot_token, function (v) { m.slack.bot_token = v; renderOutput(); }, 'SLACK_BOT_TOKEN', 'Slack Bot User Token (starts with xoxb-) — used to read and send messages'));
        } else if (m.platform === 'discord') {
            fields.push(fieldEnvVar('Bot Token', m.discord.bot_token, function (v) { m.discord.bot_token = v; renderOutput(); }, 'DISCORD_BOT_TOKEN', 'Discord Bot Token from the Developer Portal — add the bot to your server first'));
        } else if (m.platform === 'telegram') {
            fields.push(fieldEnvVar('Token', m.telegram.token, function (v) { m.telegram.token = v; renderOutput(); }, 'TELEGRAM_BOT_TOKEN', 'Bot token from @BotFather on Telegram — message /newbot to create one'));
        } else if (m.platform === 'teams') {
            fields.push(fieldEnvVar('App ID', m.teams.app_id, function (v) { m.teams.app_id = v; renderOutput(); }, 'TEAMS_APP_ID', 'Azure Bot registration App ID from the Azure Portal'));
            fields.push(fieldEnvVar('App Password', m.teams.app_password, function (v) { m.teams.app_password = v; renderOutput(); }, 'TEAMS_APP_PASSWORD', 'Azure Bot registration secret/password'));
            fields.push(fieldText('Listen Address', m.teams.listen_addr, function (v) { m.teams.listen_addr = v; renderOutput(); }, ':3978', 'Network address where Genie listens for Teams webhook events'));
        } else if (m.platform === 'googlechat') {
            fields.push(fieldText('Credentials File', m.googlechat.credentials_file, function (v) { m.googlechat.credentials_file = v; renderOutput(); }, '/path/to/service-account.json', 'Path to your Google Cloud service account key file'));
            fields.push(fieldText('Listen Address', m.googlechat.listen_addr, function (v) { m.googlechat.listen_addr = v; renderOutput(); }, ':8080', 'Network address where Genie listens for Google Chat webhook events'));
        } else if (m.platform === 'whatsapp') {
            fields.push(fieldEnvVar('Access Token', m.whatsapp.access_token, function (v) { m.whatsapp.access_token = v; renderOutput(); }, 'WHATSAPP_ACCESS_TOKEN', 'WhatsApp Business API access token from Meta Developer Console'));
            fields.push(fieldText('Phone Number ID', m.whatsapp.phone_number_id, function (v) { m.whatsapp.phone_number_id = v; renderOutput(); }, 'WHATSAPP_PHONE_NUMBER_ID', 'The Phone Number ID from your WhatsApp Business account settings'));
            fields.push(fieldEnvVar('App Secret', m.whatsapp.app_secret, function (v) { m.whatsapp.app_secret = v; renderOutput(); }, 'WHATSAPP_APP_SECRET', 'Facebook App Secret — verifies webhook signatures for security'));
            fields.push(fieldEnvVar('Verify Token', m.whatsapp.verify_token, function (v) { m.whatsapp.verify_token = v; renderOutput(); }, 'WHATSAPP_VERIFY_TOKEN', 'Your chosen webhook verification token — must match what you set in Meta Dashboard'));
            fields.push(fieldText('Listen Address', m.whatsapp.listen_addr, function (v) { m.whatsapp.listen_addr = v; renderOutput(); }, ':8443', 'Network address where Genie listens for WhatsApp webhook events'));
        }
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── SCM ──
    function renderSCM() {
        var c = $('scm-body');
        if (!c) return;
        c.innerHTML = '';
        var s = state.scm;
        var fields = [
            fieldSelect('Provider', s.provider, SCM_PROVIDERS, function (v) { s.provider = v; renderAll(); }, 'Which SCM platform to connect — leave empty to disable'),
        ];
        if (s.provider) {
            fields.push(fieldEnvVar('Token', s.token, function (v) { s.token = v; renderOutput(); }, 'SCM_TOKEN', 'Personal Access Token for API access'));
            fields.push(fieldText('Base URL', s.base_url, function (v) { s.base_url = v; renderOutput(); }, 'https://github.example.com', 'Enterprise instance URL — leave empty for cloud'));
        }
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── PM ──
    function renderPM() {
        var c = $('pm-body');
        if (!c) return;
        c.innerHTML = '';
        var p = state.pm;
        var fields = [
            fieldSelect('Provider', p.provider, PM_PROVIDERS, function (v) { p.provider = v; renderAll(); }, 'Which issue tracker to connect — leave empty to disable'),
        ];
        if (p.provider) {
            fields.push(fieldEnvVar('API Token', p.api_token, function (v) { p.api_token = v; renderOutput(); }, 'PM_API_TOKEN', 'API token for authentication'));
            fields.push(fieldText('Base URL', p.base_url, function (v) { p.base_url = v; renderOutput(); }, 'https://mycompany.atlassian.net', 'Jira: required instance URL; Linear/Asana: optional'));
            if (p.provider === 'jira') {
                fields.push(fieldText('Email', p.email, function (v) { p.email = v; renderOutput(); }, 'you@company.com', 'Jira only — email address for Basic auth'));
            }
        }
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── Browser ──
    function renderBrowser() {
        var c = $('browser-body');
        if (!c) return;
        c.innerHTML = '';
        var b = state.browser;
        c.appendChild(el('div', { className: 'space-y-4' }, [
            fieldText('Blocked Domains (comma-separated)', (b.blocked_domains || []).join(', '), function (v) { b.blocked_domains = splitCSV(v); renderOutput(); }, 'example.com, internal.net', 'Domains disallowed for automation (suffix match)')
        ]));
    }

    // ── Email ──
    function renderEmail() {
        var c = $('email-body');
        if (!c) return;
        c.innerHTML = '';
        var e = state.email;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldSelect('Provider', e.provider, ['', 'smtp'], function (v) { e.provider = v; renderAll(); }, 'Email provider (currently only SMTP supported)'),
        ]));

        if (e.provider === 'smtp') {
            c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4 mt-4' }, [
                fieldText('SMTP Host', e.host, function (v) { e.host = v; renderOutput(); }, 'smtp.example.com'),
                fieldNumber('SMTP Port', e.port, function (v) { e.port = v; renderOutput(); }, 1, 65535),
                fieldEnvVar('Username', e.username, function (v) { e.username = v; renderOutput(); }, 'SMTP_USERNAME'),
                fieldEnvVar('Password', e.password, function (v) { e.password = v; renderOutput(); }, 'SMTP_PASSWORD'),
                fieldText('IMAP Host', e.imap_host, function (v) { e.imap_host = v; renderOutput(); }, 'imap.example.com'),
                fieldNumber('IMAP Port', e.imap_port, function (v) { e.imap_port = v; renderOutput(); }, 1, 65535)
            ]));
        }
    }

    // ── HITL ──
    function renderHITL() {
        var c = $('hitl-body');
        if (!c) return;
        c.innerHTML = '';
        var h = state.hitl;
        c.appendChild(el('div', { className: 'space-y-4' }, [
            fieldText('Read-Only Tools (comma-separated)', (h.read_only_tools || []).join(', '), function (v) { h.read_only_tools = splitCSV(v); renderOutput(); }, 'read_file, list_file', 'Tools that require explicit human approval before execution')
        ]));
    }

    // ── AGUI ──
    function renderAGUI() {
        var c = $('agui-body');
        if (!c) return;
        c.innerHTML = '';
        var a = state.agui;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldNumber('Port', a.port, function (v) { a.port = v; renderOutput(); }, 1024, 65535, 'HTTP server port'),
            fieldText('CORS Origins (comma-separated)', (a.cors_origins || []).join(', '), function (v) { a.cors_origins = splitCSV(v); renderOutput(); }, 'https://myapp.com', 'Allowed origins for browser access'),
            fieldNumber('Rate Limit (req/sec)', a.rate_limit, function (v) { a.rate_limit = parseFloat(v); renderOutput(); }, 0, 1000, 'Requests per second per IP (0 to disable)'),
            fieldNumber('Rate Burst', a.rate_burst, function (v) { a.rate_burst = v; renderOutput(); }, 1, 100, 'Burst allowance'),
            fieldNumber('Max Concurrent', a.max_concurrent, function (v) { a.max_concurrent = v; renderOutput(); }, 0, 1000, 'Max in-flight requests'),
            fieldNumber('Max Body Bytes', a.max_body_bytes, function (v) { a.max_body_bytes = v; renderOutput(); }, 0, 104857600, 'Max request body size in bytes')
        ]));
    }

    /* ================================================================
     * 5. TOML SERIALIZERS  (one function per config section)
     * ================================================================ */

    function providersToToml(lines) {
        state.providers.forEach(function (p) {
            lines.push('[[model_config.providers]]');
            lines.push('provider = ' + q(p.provider));
            lines.push('model_name = ' + q(p.model_name));
            lines.push('variant = ' + q(p.variant));
            if (p.token) lines.push('token = ' + q('${' + p.token + '}'));
            if (p.good_for_task) lines.push('good_for_task = ' + q(p.good_for_task));
            lines.push('');
        });
    }





    function skillsToToml(lines) {
        if (!hasItems(state.skills_roots)) return;
        lines.push('skills_roots = [' + state.skills_roots.filter(Boolean).map(q).join(', ') + ']');
        lines.push('');
    }

    function mcpToToml(lines) {
        state.mcp_servers.forEach(function (srv) {
            lines.push('[[mcp.servers]]');
            if (srv.name) lines.push('name = ' + q(srv.name));
            lines.push('transport = ' + q(srv.transport));
            if (srv.transport === 'stdio' && srv.command) lines.push('command = ' + q(srv.command));
            if (srv.transport !== 'stdio' && srv.server_url) lines.push('server_url = ' + q(srv.server_url));
            if (hasItems(srv.args)) lines.push('args = [' + srv.args.filter(Boolean).map(q).join(', ') + ']');
            if (hasItems(srv.include_tools)) lines.push('include_tools = [' + srv.include_tools.filter(Boolean).map(q).join(', ') + ']');
            if (hasItems(srv.exclude_tools)) lines.push('exclude_tools = [' + srv.exclude_tools.filter(Boolean).map(q).join(', ') + ']');
            lines.push('');
        });
    }

    function webSearchToToml(lines) {
        var ws = state.web_search;
        lines.push('[web_search]');
        lines.push('provider = ' + q(ws.provider || 'duckduckgo'));
        if (ws.provider === 'google') {
            if (ws.google_api_key) lines.push('google_api_key = ' + q('${' + ws.google_api_key + '}'));
            if (ws.google_cx) lines.push('google_cx = ' + q('${' + ws.google_cx + '}'));
        } else if (ws.provider === 'bing') {
            if (ws.bing_api_key) lines.push('bing_api_key = ' + q('${' + ws.bing_api_key + '}'));
        }
        lines.push('');
    }

    function vectorToToml(lines) {
        var vm = state.vector_memory;
        if (vm.embedding_provider === 'dummy' && !vm.persistence_dir && !vm.api_key) return;
        lines.push('[vector_memory]');
        if (vm.persistence_dir) lines.push('persistence_dir = ' + q(vm.persistence_dir));
        lines.push('embedding_provider = ' + q(vm.embedding_provider));
        if (vm.api_key) lines.push('api_key = ' + q('${' + vm.api_key + '}'));
        if (vm.ollama_url) lines.push('ollama_url = ' + q(vm.ollama_url));
        if (vm.ollama_model) lines.push('ollama_model = ' + q(vm.ollama_model));
        lines.push('');
    }

    function messengerToToml(lines) {
        var m = state.messenger;
        if (!m.platform) return;
        lines.push('[messenger]');
        lines.push('platform = ' + q(m.platform));
        if (m.buffer_size !== 100) lines.push('buffer_size = ' + m.buffer_size);
        lines.push('');
        if (m.platform === 'slack') {
            lines.push('[messenger.slack]');
            if (m.slack.app_token) lines.push('app_token = ' + q('${' + m.slack.app_token + '}'));
            if (m.slack.bot_token) lines.push('bot_token = ' + q('${' + m.slack.bot_token + '}'));
            lines.push('');
        } else if (m.platform === 'discord') {
            lines.push('[messenger.discord]');
            if (m.discord.bot_token) lines.push('bot_token = ' + q('${' + m.discord.bot_token + '}'));
            lines.push('');
        } else if (m.platform === 'telegram') {
            lines.push('[messenger.telegram]');
            if (m.telegram.token) lines.push('token = ' + q('${' + m.telegram.token + '}'));
            lines.push('');
        } else if (m.platform === 'teams') {
            lines.push('[messenger.teams]');
            if (m.teams.app_id) lines.push('app_id = ' + q('${' + m.teams.app_id + '}'));
            if (m.teams.app_password) lines.push('app_password = ' + q('${' + m.teams.app_password + '}'));
            if (m.teams.listen_addr) lines.push('listen_addr = ' + q(m.teams.listen_addr));
            lines.push('');
        } else if (m.platform === 'googlechat') {
            lines.push('[messenger.googlechat]');
            if (m.googlechat.credentials_file) lines.push('credentials_file = ' + q(m.googlechat.credentials_file));
            if (m.googlechat.listen_addr) lines.push('listen_addr = ' + q(m.googlechat.listen_addr));
            lines.push('');
        } else if (m.platform === 'whatsapp') {
            lines.push('[messenger.whatsapp]');
            if (m.whatsapp.access_token) lines.push('access_token = ' + q('${' + m.whatsapp.access_token + '}'));
            if (m.whatsapp.phone_number_id) lines.push('phone_number_id = ' + q(m.whatsapp.phone_number_id));
            if (m.whatsapp.app_secret) lines.push('app_secret = ' + q('${' + m.whatsapp.app_secret + '}'));
            if (m.whatsapp.verify_token) lines.push('verify_token = ' + q('${' + m.whatsapp.verify_token + '}'));
            if (m.whatsapp.listen_addr) lines.push('listen_addr = ' + q(m.whatsapp.listen_addr));
            lines.push('');
        }
    }

    /** Assemble full TOML output. */
    function toToml() {
        var lines = [];
        if (state.providers.length > 0) providersToToml(lines);


        skillsToToml(lines);
        mcpToToml(lines);
        webSearchToToml(lines);
        vectorToToml(lines);
        messengerToToml(lines);
        scmToToml(lines);
        pmToToml(lines);
        browserToToml(lines);
        emailToToml(lines);
        hitlToToml(lines);
        aguiToToml(lines);
        return lines.join('\n');
    }

    function scmToToml(lines) {
        var s = state.scm;
        if (!s.provider) return;
        lines.push('[scm]');
        lines.push('provider = ' + q(s.provider));
        if (s.token) lines.push('token = ' + q('${' + s.token + '}'));
        if (s.base_url) lines.push('base_url = ' + q(s.base_url));
        lines.push('');
    }

    function pmToToml(lines) {
        var p = state.pm;
        if (!p.provider) return;
        lines.push('[pm]');
        lines.push('provider = ' + q(p.provider));
        if (p.api_token) lines.push('api_token = ' + q('${' + p.api_token + '}'));
        if (p.base_url) lines.push('base_url = ' + q(p.base_url));
        if (p.provider === 'jira' && p.email) lines.push('email = ' + q(p.email));
        lines.push('');
    }

    function browserToToml(lines) {
        var b = state.browser;
        if (!hasItems(b.blocked_domains)) return;
        lines.push('[browser]');
        lines.push('blocked_domains = [' + b.blocked_domains.filter(Boolean).map(q).join(', ') + ']');
        lines.push('');
    }

    function emailToToml(lines) {
        var e = state.email;
        if (!e.provider) return;
        lines.push('[email]');
        lines.push('provider = ' + q(e.provider));
        if (e.host) lines.push('host = ' + q(e.host));
        lines.push('port = ' + e.port);
        if (e.username) lines.push('username = ' + q('${' + e.username + '}'));
        if (e.password) lines.push('password = ' + q('${' + e.password + '}'));
        if (e.imap_host) lines.push('imap_host = ' + q(e.imap_host));
        lines.push('imap_port = ' + e.imap_port);
        lines.push('');
    }

    function hitlToToml(lines) {
        var h = state.hitl;
        if (!hasItems(h.read_only_tools)) return;
        lines.push('[hitl]');
        lines.push('read_only_tools = [' + h.read_only_tools.filter(Boolean).map(q).join(', ') + ']');
        lines.push('');
    }

    function aguiToToml(lines) {
        var a = state.agui;
        lines.push('[agui]');
        lines.push('port = ' + a.port);
        if (hasItems(a.cors_origins)) lines.push('cors_origins = [' + a.cors_origins.filter(Boolean).map(q).join(', ') + ']');
        lines.push('rate_limit = ' + a.rate_limit);
        lines.push('rate_burst = ' + a.rate_burst);
        lines.push('max_concurrent = ' + a.max_concurrent);
        lines.push('max_body_bytes = ' + a.max_body_bytes);
        lines.push('');
    }

    /* ================================================================
     * 6. YAML SERIALIZERS  (one function per config section)
     * ================================================================ */

    function providersToYaml(lines) {
        lines.push('model_config:');
        lines.push('  providers:');
        state.providers.forEach(function (p) {
            lines.push('    - provider: ' + p.provider);
            lines.push('      model_name: ' + p.model_name);
            lines.push('      variant: ' + p.variant);
            if (p.token) lines.push('      token: ' + yq('${' + p.token + '}'));
            if (p.good_for_task) lines.push('      good_for_task: ' + p.good_for_task);
        });
        lines.push('');
    }





    function skillsToYaml(lines) {
        if (!hasItems(state.skills_roots)) return;
        lines.push('skills_roots:');
        state.skills_roots.filter(Boolean).forEach(function (s) { lines.push('  - ' + yq(s)); });
        lines.push('');
    }

    function mcpToYaml(lines) {
        if (state.mcp_servers.length === 0) return;
        lines.push('mcp:');
        lines.push('  servers:');
        state.mcp_servers.forEach(function (srv) {
            lines.push('    - name: ' + yq(srv.name));
            lines.push('      transport: ' + srv.transport);
            if (srv.transport === 'stdio' && srv.command) lines.push('      command: ' + yq(srv.command));
            if (srv.transport !== 'stdio' && srv.server_url) lines.push('      server_url: ' + yq(srv.server_url));
            if (hasItems(srv.args)) {
                lines.push('      args:');
                srv.args.filter(Boolean).forEach(function (a) { lines.push('        - ' + yq(a)); });
            }
            if (hasItems(srv.include_tools)) {
                lines.push('      include_tools:');
                srv.include_tools.filter(Boolean).forEach(function (t) { lines.push('        - ' + t); });
            }
            if (hasItems(srv.exclude_tools)) {
                lines.push('      exclude_tools:');
                srv.exclude_tools.filter(Boolean).forEach(function (t) { lines.push('        - ' + t); });
            }
        });
        lines.push('');
    }

    function webSearchToYaml(lines) {
        var ws = state.web_search;
        lines.push('web_search:');
        lines.push('  provider: ' + (ws.provider || 'duckduckgo'));
        if (ws.provider === 'google') {
            if (ws.google_api_key) lines.push('  google_api_key: ' + yq('${' + ws.google_api_key + '}'));
            if (ws.google_cx) lines.push('  google_cx: ' + yq('${' + ws.google_cx + '}'));
        } else if (ws.provider === 'bing') {
            if (ws.bing_api_key) lines.push('  bing_api_key: ' + yq('${' + ws.bing_api_key + '}'));
        }
        lines.push('');
    }

    function vectorToYaml(lines) {
        var vm = state.vector_memory;
        if (vm.embedding_provider === 'dummy' && !vm.persistence_dir && !vm.api_key) return;
        lines.push('vector_memory:');
        if (vm.persistence_dir) lines.push('  persistence_dir: ' + yq(vm.persistence_dir));
        lines.push('  embedding_provider: ' + vm.embedding_provider);
        if (vm.api_key) lines.push('  api_key: ' + yq('${' + vm.api_key + '}'));
        if (vm.ollama_url) lines.push('  ollama_url: ' + yq(vm.ollama_url));
        if (vm.ollama_model) lines.push('  ollama_model: ' + vm.ollama_model);
        lines.push('');
    }

    function messengerToYaml(lines) {
        var m = state.messenger;
        if (!m.platform) return;
        lines.push('messenger:');
        lines.push('  platform: ' + m.platform);
        if (m.buffer_size !== 100) lines.push('  buffer_size: ' + m.buffer_size);
        if (m.platform === 'slack') {
            lines.push('  slack:');
            if (m.slack.app_token) lines.push('    app_token: ' + yq('${' + m.slack.app_token + '}'));
            if (m.slack.bot_token) lines.push('    bot_token: ' + yq('${' + m.slack.bot_token + '}'));
        } else if (m.platform === 'discord') {
            lines.push('  discord:');
            if (m.discord.bot_token) lines.push('    bot_token: ' + yq('${' + m.discord.bot_token + '}'));
        } else if (m.platform === 'telegram') {
            lines.push('  telegram:');
            if (m.telegram.token) lines.push('    token: ' + yq('${' + m.telegram.token + '}'));
        } else if (m.platform === 'teams') {
            lines.push('  teams:');
            if (m.teams.app_id) lines.push('    app_id: ' + yq('${' + m.teams.app_id + '}'));
            if (m.teams.app_password) lines.push('    app_password: ' + yq('${' + m.teams.app_password + '}'));
            if (m.teams.listen_addr) lines.push('    listen_addr: ' + yq(m.teams.listen_addr));
        } else if (m.platform === 'googlechat') {
            lines.push('  googlechat:');
            if (m.googlechat.credentials_file) lines.push('    credentials_file: ' + yq(m.googlechat.credentials_file));
            if (m.googlechat.listen_addr) lines.push('    listen_addr: ' + yq(m.googlechat.listen_addr));
        } else if (m.platform === 'whatsapp') {
            lines.push('  whatsapp:');
            if (m.whatsapp.access_token) lines.push('    access_token: ' + yq('${' + m.whatsapp.access_token + '}'));
            if (m.whatsapp.phone_number_id) lines.push('    phone_number_id: ' + yq(m.whatsapp.phone_number_id));
            if (m.whatsapp.app_secret) lines.push('    app_secret: ' + yq('${' + m.whatsapp.app_secret + '}'));
            if (m.whatsapp.verify_token) lines.push('    verify_token: ' + yq('${' + m.whatsapp.verify_token + '}'));
            if (m.whatsapp.listen_addr) lines.push('    listen_addr: ' + yq(m.whatsapp.listen_addr));
        }
        lines.push('');
    }

    /** Assemble full YAML output. */
    function toYaml() {
        var lines = [];
        if (state.providers.length > 0) providersToYaml(lines);


        skillsToYaml(lines);
        mcpToYaml(lines);
        webSearchToYaml(lines);
        vectorToYaml(lines);
        messengerToYaml(lines);
        scmToYaml(lines);
        pmToYaml(lines);
        browserToYaml(lines);
        emailToYaml(lines);
        hitlToYaml(lines);
        aguiToYaml(lines);
        return lines.join('\n');
    }

    function scmToYaml(lines) {
        var s = state.scm;
        if (!s.provider) return;
        lines.push('scm:');
        lines.push('  provider: ' + s.provider);
        if (s.token) lines.push('  token: ' + yq('${' + s.token + '}'));
        if (s.base_url) lines.push('  base_url: ' + yq(s.base_url));
        lines.push('');
    }

    function pmToYaml(lines) {
        var p = state.pm;
        if (!p.provider) return;
        lines.push('pm:');
        lines.push('  provider: ' + p.provider);
        if (p.api_token) lines.push('  api_token: ' + yq('${' + p.api_token + '}'));
        if (p.base_url) lines.push('  base_url: ' + yq(p.base_url));
        if (p.provider === 'jira' && p.email) lines.push('  email: ' + yq(p.email));
        lines.push('');
    }

    function browserToYaml(lines) {
        var b = state.browser;
        if (!hasItems(b.blocked_domains)) return;
        lines.push('browser:');
        lines.push('  blocked_domains:');
        b.blocked_domains.filter(Boolean).forEach(function (d) { lines.push('    - ' + yq(d)); });
        lines.push('');
    }

    function emailToYaml(lines) {
        var e = state.email;
        if (!e.provider) return;
        lines.push('email:');
        lines.push('  provider: ' + e.provider);
        if (e.host) lines.push('  host: ' + yq(e.host));
        lines.push('  port: ' + e.port);
        if (e.username) lines.push('  username: ' + yq('${' + e.username + '}'));
        if (e.password) lines.push('  password: ' + yq('${' + e.password + '}'));
        if (e.imap_host) lines.push('  imap_host: ' + yq(e.imap_host));
        lines.push('  imap_port: ' + e.imap_port);
        lines.push('');
    }

    function hitlToYaml(lines) {
        var h = state.hitl;
        if (!hasItems(h.read_only_tools)) return;
        lines.push('hitl:');
        lines.push('  read_only_tools:');
        h.read_only_tools.filter(Boolean).forEach(function (t) { lines.push('    - ' + t); });
        lines.push('');
    }

    function aguiToYaml(lines) {
        var a = state.agui;
        lines.push('agui:');
        lines.push('  port: ' + a.port);
        if (hasItems(a.cors_origins)) {
            lines.push('  cors_origins:');
            a.cors_origins.filter(Boolean).forEach(function (o) { lines.push('    - ' + yq(o)); });
        }
        lines.push('  rate_limit: ' + a.rate_limit);
        lines.push('  rate_burst: ' + a.rate_burst);
        lines.push('  max_concurrent: ' + a.max_concurrent);
        lines.push('  max_body_bytes: ' + a.max_body_bytes);
        lines.push('');
    }

    /* ================================================================
     * 7. OUTPUT & ACTIONS
     * ================================================================ */

    function renderOutput() {
        var code = $('output-code');
        if (!code) return;
        code.textContent = state.format === 'toml' ? toToml() : toYaml();
    }

    window.toggleSection = function (id) {
        var body = $(id);
        if (!body) return;
        body.classList.toggle('open');
        var chevron = body.previousElementSibling.querySelector('.chevron');
        if (chevron) chevron.classList.toggle('rotate-180');
    };

    window.setFormat = function (fmt) {
        state.format = fmt;
        document.querySelectorAll('.format-toggle button').forEach(function (b) {
            b.classList.toggle('active', b.dataset.fmt === fmt);
        });
        renderOutput();
    };

    window.copyOutput = function () {
        var code = $('output-code');
        navigator.clipboard.writeText(code.textContent).then(function () {
            var btn = $('copy-btn');
            btn.textContent = '✓ Copied!';
            btn.classList.add('copied');
            setTimeout(function () { btn.textContent = 'Copy'; btn.classList.remove('copied'); }, 1500);
        });
    };

    window.downloadConfig = function () {
        var content = $('output-code').textContent;
        var ext = state.format === 'toml' ? '.toml' : '.yaml';
        var blob = new Blob([content], { type: 'text/plain' });
        var a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = '.genie' + ext;
        a.click();
        URL.revokeObjectURL(a.href);
    };

    /* ================================================================
     * 8. INIT
     * ================================================================ */

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', renderAll);
    } else {
        renderAll();
    }
})();

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
        vector_memory: { persistence_dir: '', embedding_provider: 'dummy', api_key: 'OPENAI_API_KEY', ollama_url: '', ollama_model: '', huggingface_url: '', gemini_api_key: 'GOOGLE_API_KEY', gemini_model: '', vector_store_provider: 'inmemory', allowed_metadata_keys: [], milvus: { address: '', username: '', password: '', db_name: '', api_key: 'MILVUS_API_KEY', collection_name: '', dimension: 0 } },
        graph: { disabled: false, data_dir: '' },
        data_sources: { enabled: false, sync_interval: '15m', search_keywords: [], gmail: { enabled: false, label_ids: [] }, gdrive: { enabled: false, folder_ids: [] }, github: { enabled: false, repos: [] }, gitlab: { enabled: false, repos: [] } },
        messenger: {
            platform: '', buffer_size: 100, allowed_senders: [],
            slack: { app_token: 'SLACK_APP_TOKEN', bot_token: 'SLACK_BOT_TOKEN' },
            discord: { bot_token: 'DISCORD_BOT_TOKEN' },
            telegram: { token: 'TELEGRAM_BOT_TOKEN' },
            teams: { app_id: 'TEAMS_APP_ID', app_password: 'TEAMS_APP_PASSWORD', listen_addr: ':3978' },
            googlechat: {},
            whatsapp: {},
            agui: { port: 8080, cors_origins: ['https://stackgenhq.github.io'], rate_limit: 0.5, rate_burst: 3, max_concurrent: 5, max_body_bytes: 1048576 }
        },
        scm: { provider: '', token: 'SCM_TOKEN', base_url: '' },
        pm: { provider: '', api_token: 'PM_API_TOKEN', base_url: '', email: '' },
        browser: { blocked_domains: [] },
        email: { provider: '', host: '', port: 587, username: '', password: '', imap_host: '', imap_port: 993 },
        hitl: { always_allowed: [], denied_tools: [], cache_ttl: '' },
        toolwrap: {
            timeout: { enabled: false, default_timeout: '30s', per_tool: '' },
            rate_limit: { enabled: false, global_rate_per_minute: 60, per_tool_rate_per_minute: '' },
            circuit_breaker: { enabled: false, failure_threshold: 5, open_duration: '30s' },
            concurrency: { enabled: false, global_limit: 10, per_tool_limits: '' },
            retry: { enabled: false, max_attempts: 3, initial_backoff: '500ms', max_backoff: '10s' },
            metrics: { enabled: false, prefix: 'tools' },
            tracing: { enabled: false },
            sanitize: { enabled: false, replacement: '[REDACTED]', per_tool: '' },
            validation: { enabled: false }
        },
        db_config: { db_file: '' },

        langfuse: { public_key: 'LANGFUSE_PUBLIC_KEY', secret_key: 'LANGFUSE_SECRET_KEY', host: 'https://cloud.langfuse.com', enable_prompts: false },
        runbook: { runbook_paths: [] },
        cron: { enabled: false, tasks: [] },
        security: { secrets: [] },
        pii: { salt: '', entropy_threshold: 4.2, min_secret_length: 12, sensitive_keys: [] },
        disable_pensieve: false
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
    var EMBED_PROVIDERS = ['dummy', 'openai', 'ollama', 'huggingface', 'gemini'];
    var VECTOR_STORE_PROVIDERS = ['inmemory', 'milvus'];
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
        renderRunbook();
        renderMCP();
        renderWebSearch();
        renderVectorMemory();
        renderGraph();
        renderDataSources();
        renderMessenger();
        renderSCM();
        renderPM();
        renderBrowser();
        renderEmail();
        renderHITL();
        renderToolwrap();
        renderSecurity();
        renderPII();
        renderDBConfig();
        renderAGUI();
        renderLangfuse();
        renderCron();
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

    function envMapToLines(env) {
        if (!env || typeof env !== 'object') return '';
        return Object.keys(env).filter(Boolean).map(function (k) { return k + '=' + (env[k] || ''); }).join('\n');
    }
    function parseEnvLines(text) {
        var out = {};
        if (!text) return out;
        text.split('\n').forEach(function (line) {
            var idx = line.indexOf('=');
            if (idx > 0) {
                var k = line.slice(0, idx).trim();
                if (k) out[k] = line.slice(idx + 1).trim();
            }
        });
        return out;
    }
    function headersMapToLines(headers) {
        if (!headers || typeof headers !== 'object') return '';
        return Object.keys(headers).filter(Boolean).map(function (k) { return k + ': ' + (headers[k] || ''); }).join('\n');
    }
    function parseHeadersLines(text) {
        var out = {};
        if (!text) return out;
        text.split('\n').forEach(function (line) {
            var idx = line.indexOf(':');
            if (idx > 0) {
                var k = line.slice(0, idx).trim();
                if (k) out[k] = line.slice(idx + 1).trim();
            }
        });
        return out;
    }

    function buildMCPCard(srv, i) {
        srv.env = srv.env || {};
        srv.headers = srv.headers || {};
        var connectionField = srv.transport === 'stdio'
            ? fieldText('Command', srv.command || '', function (v) { srv.command = v; renderOutput(); }, 'npx @org/mcp-server')
            : fieldText('Server URL', srv.server_url || '', function (v) { srv.server_url = v; renderOutput(); }, 'https://mcp.example.com');

        var fields = [
            fieldText('Name', srv.name, function (v) { srv.name = v; renderOutput(); }, 'my-server', 'A friendly name to identify this tool server'),
            fieldSelect('Transport', srv.transport, MCP_TRANSPORTS, function (v) { srv.transport = v; renderAll(); }, 'How to connect — stdio runs a local program, HTTP/SSE connects to a remote server'),
            connectionField,
            fieldText('Args (comma-separated)', (srv.args || []).join(', '), function (v) { srv.args = splitCSV(v); renderOutput(); }, '--port 8080', 'Extra arguments passed to the command (stdio only)'),
            fieldText('Include Tools (comma-sep)', (srv.include_tools || []).join(', '), function (v) { srv.include_tools = splitCSV(v); renderOutput(); }, null, 'Only use these specific tools from the server (leave empty for all)'),
            fieldText('Exclude Tools (comma-sep)', (srv.exclude_tools || []).join(', '), function (v) { srv.exclude_tools = splitCSV(v); renderOutput(); }, null, 'Block these tools from being used — useful for restricting dangerous operations')
        ];

        if (srv.transport === 'stdio') {
            var envWrapper = el('div', {});
            var envTooltip = 'Environment variables for the stdio subprocess. Use ${VAR} so Genie resolves the value from your secret provider (e.g. GITHUB_PERSONAL_ACCESS_TOKEN=${GH_TOKEN}).';
            envWrapper.appendChild(el('label', { className: 'form-label' }, 'Env (one per line: KEY=value or KEY=${VAR})<span class="form-tooltip">' + envTooltip + '</span>'));
            var envTa = el('textarea', { className: 'form-input font-mono text-sm', rows: 3, placeholder: 'GITHUB_PERSONAL_ACCESS_TOKEN=${GH_TOKEN}' });
            envTa.value = envMapToLines(srv.env);
            envTa.addEventListener('input', function () { srv.env = parseEnvLines(this.value); renderOutput(); });
            envWrapper.appendChild(envTa);
            fields.push(envWrapper);
        } else {
            var headersWrapper = el('div', {});
            var headersTooltip = 'HTTP headers for this server (e.g. Authorization: Bearer ${MCP_TOKEN}).';
            headersWrapper.appendChild(el('label', { className: 'form-label' }, 'Headers (one per line: Name: value)<span class="form-tooltip">' + headersTooltip + '</span>'));
            var headersTa = el('textarea', { className: 'form-input font-mono text-sm', rows: 2, placeholder: 'Authorization: Bearer ${MCP_TOKEN}' });
            headersTa.value = headersMapToLines(srv.headers);
            headersTa.addEventListener('input', function () { srv.headers = parseHeadersLines(this.value); renderOutput(); });
            headersWrapper.appendChild(headersTa);
            fields.push(headersWrapper);
        }

        return el('div', { className: 'repeatable-item' }, [
            el('div', { className: 'flex items-center justify-between mb-3' }, [
                el('span', { className: 'text-sm font-semibold text-gray-600' }, 'Server #' + (i + 1) + (srv.name ? ' — ' + srv.name : '')),
                el('button', { className: 'btn-remove', onClick: function () { state.mcp_servers.splice(i, 1); renderAll(); } }, '✕')
            ]),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields)
        ]);
    }

    function addMCPServer() {
        state.mcp_servers.push({ name: '', transport: 'stdio', command: '', server_url: '', args: [], include_tools: [], exclude_tools: [], env: {}, headers: {} });
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
        var fields = [
            fieldSelect('Vector Store Provider', vm.vector_store_provider, VECTOR_STORE_PROVIDERS, function (v) { vm.vector_store_provider = v; renderAll(); }, 'Backend for storing vectors — inmemory is simple/local, Milvus is production-grade and scalable'),
            fieldSelect('Embedding Provider', vm.embedding_provider, EMBED_PROVIDERS, function (v) { vm.embedding_provider = v; renderAll(); }, 'How Genie creates memory embeddings — OpenAI is best quality, Gemini is great, HuggingFace TEI is self-hosted, Ollama is free/local'),
            fieldText('Allowed Metadata Keys (comma-separated)', (vm.allowed_metadata_keys || []).join(', '), function (v) { vm.allowed_metadata_keys = splitCSV(v); renderOutput(); }, 'product, category, source', 'Optional list of metadata keys allowed in memory_store and memory_search — use for product/category buckets; leave empty to allow any key')
        ];
        if (vm.vector_store_provider === 'inmemory') {
            fields.push(fieldText('Persistence Dir', vm.persistence_dir, function (v) { vm.persistence_dir = v; renderOutput(); }, './data/vectors', 'Where to save memory on disk — leave empty for in-memory only (lost on restart)'));
        }
        if (vm.embedding_provider === 'openai') {
            fields.push(fieldEnvVar('API Key', vm.api_key, function (v) { vm.api_key = v; renderOutput(); }, 'OPENAI_API_KEY', 'OpenAI API key used to generate text embeddings'));
        } else if (vm.embedding_provider === 'ollama') {
            fields.push(fieldText('Ollama URL', vm.ollama_url, function (v) { vm.ollama_url = v; renderOutput(); }, 'http://localhost:11434', 'Address of your locally running Ollama server'));
            fields.push(fieldText('Ollama Model', vm.ollama_model, function (v) { vm.ollama_model = v; renderOutput(); }, 'nomic-embed-text', 'Which Ollama model to use for embeddings'));
        } else if (vm.embedding_provider === 'huggingface') {
            fields.push(fieldText('HuggingFace TEI URL', vm.huggingface_url, function (v) { vm.huggingface_url = v; renderOutput(); }, 'http://localhost:8080', 'Address of your HuggingFace Text-Embeddings-Inference server'));
        } else if (vm.embedding_provider === 'gemini') {
            fields.push(fieldEnvVar('Google API Key', vm.gemini_api_key, function (v) { vm.gemini_api_key = v; renderOutput(); }, 'GOOGLE_API_KEY', 'Google API key for Gemini embedding models'));
            fields.push(fieldText('Gemini Model', vm.gemini_model, function (v) { vm.gemini_model = v; renderOutput(); }, 'gemini-embedding-exp-03-07', 'Which Gemini model to use for embeddings (leave empty for default)'));
        }
        if (vm.vector_store_provider === 'milvus') {
            fields.push(fieldText('Milvus Address', vm.milvus.address, function (v) { vm.milvus.address = v; renderOutput(); }, 'localhost:19530', 'Milvus server address and port — required for Milvus backend'));
            fields.push(fieldText('Milvus Username', vm.milvus.username, function (v) { vm.milvus.username = v; renderOutput(); }, '', 'Username for Milvus authentication — leave empty if not required'));
            fields.push(fieldEnvVar('Milvus Password', vm.milvus.password, function (v) { vm.milvus.password = v; renderOutput(); }, 'MILVUS_PASSWORD', 'Password for Milvus authentication — leave empty if not required'));
            fields.push(fieldText('Milvus DB Name', vm.milvus.db_name, function (v) { vm.milvus.db_name = v; renderOutput(); }, '', 'Database name in Milvus — leave empty for default'));
            fields.push(fieldEnvVar('Milvus API Key', vm.milvus.api_key, function (v) { vm.milvus.api_key = v; renderOutput(); }, 'MILVUS_API_KEY', 'API key for Milvus authentication — leave empty if not required'));
            fields.push(fieldText('Milvus Collection Name', vm.milvus.collection_name, function (v) { vm.milvus.collection_name = v; renderOutput(); }, 'trpc_agent_documents', 'Collection name in Milvus — defaults to trpc_agent_documents if empty'));
            fields.push(fieldNumber('Milvus Dimension', vm.milvus.dimension, function (v) { vm.milvus.dimension = v; renderOutput(); }, 0, 10000, 'Vector dimension — must match embedder dimension, defaults to embedder dimension if 0'));
        }
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── Graph ──
    function renderGraph() {
        var c = $('graph-body');
        if (!c) return;
        c.innerHTML = '';
        var g = state.graph;
        var fields = [
            fieldToggle('Disabled', g.disabled, function (v) { g.disabled = v; renderAll(); }, 'Turn off the knowledge graph and all graph_* tools (store entity, store relation, query neighbors, get entity, shortest path)'),
            fieldText('Data Dir', g.data_dir, function (v) { g.data_dir = v; renderOutput(); }, '~/.genie/my-agent', 'Where to save the graph snapshot (memory.bin.zst). If left empty, the graph will not be persisted to disk.')
        ];
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── Data Sources ──
    function renderDataSources() {
        var c = $('data-sources-body');
        if (!c) return;
        c.innerHTML = '';
        var ds = state.data_sources;
        var fields = [
            fieldToggle('Enabled', ds.enabled, function (v) { ds.enabled = v; renderAll(); }, 'Turn on background sync of Gmail, Drive, etc. into vector memory'),
            fieldText('Sync Interval', ds.sync_interval, function (v) { ds.sync_interval = v; renderOutput(); }, '15m', 'How often to run sync (e.g. 15m, 1h). Default 15m when enabled'),
            fieldText('Search Keywords (comma-separated, max 10)', (ds.search_keywords || []).join(', '), function (v) { ds.search_keywords = splitCSV(v).slice(0, 10); renderOutput(); }, 'Acme, Q4 roadmap', 'Only index items containing at least one keyword; leave empty to index all')
        ];
        fields.push(el('div', { className: 'col-span-2 text-sm font-medium text-gray-600 mt-2' }, 'Gmail'));
        fields.push(fieldToggle('Gmail enabled', ds.gmail.enabled, function (v) { ds.gmail.enabled = v; renderAll(); }, 'Sync Gmail messages from the given labels'));
        fields.push(fieldText('Gmail Label IDs (comma-separated)', (ds.gmail.label_ids || []).join(', '), function (v) { ds.gmail.label_ids = splitCSV(v); renderOutput(); }, 'INBOX', 'Gmail label IDs to sync (e.g. INBOX)'));
        fields.push(el('div', { className: 'col-span-2 text-sm font-medium text-gray-600 mt-2' }, 'Google Drive'));
        fields.push(fieldToggle('Drive enabled', ds.gdrive.enabled, function (v) { ds.gdrive.enabled = v; renderAll(); }, 'Sync Drive files from the given folders'));
        fields.push(fieldText('Drive Folder IDs (comma-separated)', (ds.gdrive.folder_ids || []).join(', '), function (v) { ds.gdrive.folder_ids = splitCSV(v); renderOutput(); }, 'root', 'Drive folder IDs to sync (e.g. root)'));
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, fields));
    }

    // ── Messenger ──
    function renderMessenger() {
        var c = $('messenger-body');
        if (!c) return;
        c.innerHTML = '';
        var m = state.messenger;
        var fields = [
            fieldSelect('Platform', m.platform, PLATFORMS, function (v) { m.platform = v; renderAll(); }, 'Which chat app to connect Genie to — leave empty to disable messaging'),
            fieldNumber('Buffer Size', m.buffer_size, function (v) { m.buffer_size = v; renderOutput(); }, 1, 10000, 'How many incoming messages to queue — increase for busy team channels'),
            fieldText('Allowed Senders (comma-separated)', (m.allowed_senders || []).join(', '), function (v) { m.allowed_senders = splitCSV(v); renderOutput(); }, '15551234567, 15559876543', 'Only respond to these sender IDs (phone numbers for WhatsApp). Leave empty to allow all.')
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
            // Google Chat uses the logged-in user OAuth token (SecretProvider); no config fields.
        } else if (m.platform === 'whatsapp') {
            // WhatsApp uses default store path (~/.genie/whatsapp); no config fields.
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
            fieldText('Read-Only Tools (comma-separated)', (h.always_allowed || []).join(', '), function (v) { h.always_allowed = splitCSV(v); renderOutput(); }, 'read_file, list_file', 'Tools that skip human approval — safe read-only operations'),
            fieldText('Denied Tools (comma-separated)', (h.denied_tools || []).join(', '), function (v) { h.denied_tools = splitCSV(v); renderOutput(); }, 'execute_code, run_shell', 'Tools that are completely blocked — the agent cannot use these at all. Supports wildcards (e.g. browser_*)'),
            fieldText('Approval Cache TTL', h.cache_ttl || '', function (v) { h.cache_ttl = v; renderOutput(); }, '10m', 'How long an approved tool+args combination stays auto-approved before requiring fresh human approval (e.g. 5m, 15m, 1h). Default: 10m')
        ]));
    }

    // ── Toolwrap Middleware ──
    function renderToolwrap() {
        var c = $('toolwrap-body');
        if (!c) return;
        c.innerHTML = '';
        var tw = state.toolwrap;

        // Timeout
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Timeout'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4' }, [
                fieldToggle('Enabled', tw.timeout.enabled, function (v) { tw.timeout.enabled = v; renderAll(); }, 'Enforce per-tool execution deadlines'),
                tw.timeout.enabled ? fieldText('Default', tw.timeout.default_timeout, function (v) { tw.timeout.default_timeout = v; renderOutput(); }, '30s', 'Default timeout for all tools') : null,
                tw.timeout.enabled ? fieldText('Per-Tool Overrides', tw.timeout.per_tool, function (v) { tw.timeout.per_tool = v; renderOutput(); }, 'execute_code:120s, web_search:15s', 'Tool-specific timeouts (name:duration, comma-separated)') : null
            ].filter(Boolean))
        ]));

        // Rate Limit
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Rate Limit'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4' }, [
                fieldToggle('Enabled', tw.rate_limit.enabled, function (v) { tw.rate_limit.enabled = v; renderAll(); }, 'Token-bucket rate limiting'),
                tw.rate_limit.enabled ? fieldNumber('Global Rate/min', tw.rate_limit.global_rate_per_minute, function (v) { tw.rate_limit.global_rate_per_minute = v; renderOutput(); }, 1, 10000, 'Calls per minute across all tools') : null,
                tw.rate_limit.enabled ? fieldText('Per-Tool Rates', tw.rate_limit.per_tool_rate_per_minute, function (v) { tw.rate_limit.per_tool_rate_per_minute = v; renderOutput(); }, 'web_search:10, api_call:30', 'Per-tool limits (name:rate, comma-separated)') : null
            ].filter(Boolean))
        ]));

        // Circuit Breaker
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Circuit Breaker'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4' }, [
                fieldToggle('Enabled', tw.circuit_breaker.enabled, function (v) { tw.circuit_breaker.enabled = v; renderAll(); }, 'Per-tool circuit breakers'),
                tw.circuit_breaker.enabled ? fieldNumber('Failure Threshold', tw.circuit_breaker.failure_threshold, function (v) { tw.circuit_breaker.failure_threshold = v; renderOutput(); }, 1, 100, 'Failures before circuit opens') : null,
                tw.circuit_breaker.enabled ? fieldText('Open Duration', tw.circuit_breaker.open_duration, function (v) { tw.circuit_breaker.open_duration = v; renderOutput(); }, '30s', 'Cooldown before half-open probe') : null
            ].filter(Boolean))
        ]));

        // Concurrency
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Concurrency'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4' }, [
                fieldToggle('Enabled', tw.concurrency.enabled, function (v) { tw.concurrency.enabled = v; renderAll(); }, 'Weighted concurrency semaphore'),
                tw.concurrency.enabled ? fieldNumber('Global Limit', tw.concurrency.global_limit, function (v) { tw.concurrency.global_limit = v; renderOutput(); }, 1, 1000, 'Max simultaneous tool executions') : null,
                tw.concurrency.enabled ? fieldText('Per-Tool Limits', tw.concurrency.per_tool_limits, function (v) { tw.concurrency.per_tool_limits = v; renderOutput(); }, 'web_search:3, browser:2', 'Per-tool caps (name:limit, comma-separated)') : null
            ].filter(Boolean))
        ]));

        // Retry
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Retry'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
                fieldToggle('Enabled', tw.retry.enabled, function (v) { tw.retry.enabled = v; renderAll(); }, 'Automatic retry with exponential backoff'),
                tw.retry.enabled ? fieldNumber('Max Attempts', tw.retry.max_attempts, function (v) { tw.retry.max_attempts = v; renderOutput(); }, 1, 20, 'Total attempts including first call') : null,
                tw.retry.enabled ? fieldText('Initial Backoff', tw.retry.initial_backoff, function (v) { tw.retry.initial_backoff = v; renderOutput(); }, '500ms', 'Wait before first retry') : null,
                tw.retry.enabled ? fieldText('Max Backoff', tw.retry.max_backoff, function (v) { tw.retry.max_backoff = v; renderOutput(); }, '10s', 'Maximum backoff cap') : null
            ].filter(Boolean))
        ]));

        // Observability row
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Observability'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4' }, [
                fieldToggle('Metrics', tw.metrics.enabled, function (v) { tw.metrics.enabled = v; renderAll(); }, 'Emit OTel metrics per tool call'),
                tw.metrics.enabled ? fieldText('Prefix', tw.metrics.prefix, function (v) { tw.metrics.prefix = v; renderOutput(); }, 'tools', 'Metric name prefix') : null,
                fieldToggle('Tracing', tw.tracing.enabled, function (v) { tw.tracing.enabled = v; renderOutput(); }, 'Create OTel spans per tool call')
            ].filter(Boolean))
        ]));

        // Security row
        c.appendChild(el('div', { className: 'space-y-3 mb-4' }, [
            el('h4', { className: 'text-xs font-semibold text-gray-500 uppercase tracking-wider' }, 'Security'),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4' }, [
                fieldToggle('Sanitize', tw.sanitize.enabled, function (v) { tw.sanitize.enabled = v; renderAll(); }, 'Redact secrets from tool output'),
                tw.sanitize.enabled ? fieldText('Replacement', tw.sanitize.replacement, function (v) { tw.sanitize.replacement = v; renderOutput(); }, '[REDACTED]', 'Text to replace redacted values') : null,
                tw.sanitize.enabled ? fieldText('Per-Tool Patterns', tw.sanitize.per_tool, function (v) { tw.sanitize.per_tool = v; renderOutput(); }, 'read_file:API_KEY|password, execute_code:token', 'Patterns per tool (tool:pat1|pat2, comma-separated)') : null
            ].filter(Boolean)),
            el('div', { className: 'grid grid-cols-1 sm:grid-cols-3 gap-4 mt-2' }, [
                fieldToggle('Validation', tw.validation.enabled, function (v) { tw.validation.enabled = v; renderOutput(); }, 'Validate tool args against JSON schema')
            ])
        ]));
    }

    // ── Security ──
    function renderSecurity() {
        var c = $('security-body');
        if (!c) return;
        c.innerHTML = '';
        var sec = state.security;
        sec.secrets.forEach(function (s, i) {
            var row = el('div', { className: 'repeatable-item' }, [
                el('div', { className: 'flex items-center justify-between mb-3' }, [
                    el('span', { className: 'text-sm font-semibold text-gray-600' }, 'Secret #' + (i + 1) + (s.name ? ' — ' + s.name : '')),
                    el('button', { className: 'btn-remove', onClick: function () { sec.secrets.splice(i, 1); renderAll(); } }, '✕')
                ]),
                el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
                    fieldText('Secret Name', s.name, function (v) { s.name = v; renderOutput(); }, 'OPENAI_API_KEY', 'The env var name this maps to'),
                    fieldText('Runtimevar URL', s.url, function (v) { s.url = v; renderOutput(); }, 'gcpsecretmanager://projects/p/secrets/s?decoder=string', 'Go CDK runtimevar URL for this secret')
                ])
            ]);
            c.appendChild(row);
        });
        c.appendChild(
            el('button', { className: 'btn-add mt-2', onClick: function () { sec.secrets.push({ name: '', url: '' }); renderAll(); } }, '+ Add Secret Mapping')
        );
        c.appendChild(el('p', { className: 'text-xs text-gray-400 mt-2' }, 'Tip: secrets not listed here automatically fall back to <code>os.Getenv</code>'));
    }

    // ── PII Redaction ──
    function renderPII() {
        var c = $('pii-body');
        if (!c) return;
        c.innerHTML = '';
        var p = state.pii;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldText('HMAC Salt', p.salt, function (v) { p.salt = v; renderOutput(); }, 'my-stable-salt-for-correlation',
                'Deterministic hashing key — same input + same salt = same [HIDDEN:hash]. Leave empty for random (hashes change on restart).'),
            fieldNumber('Entropy Threshold', p.entropy_threshold, function (v) { p.entropy_threshold = parseFloat(v) || 4.2; renderOutput(); }, 2, 5,
                'Shannon entropy score above which tokens are treated as secrets. Lower = more aggressive (2.0), higher = more permissive (5.0). Default: 3.6'),
            fieldNumber('Min Secret Length', p.min_secret_length, function (v) { p.min_secret_length = v; renderOutput(); }, 1, 64,
                'Tokens shorter than this are never redacted (unless they are values of sensitive keys). Default: 6'),
            fieldText('Sensitive Keys (comma-separated)', (p.sensitive_keys || []).join(', '), function (v) { p.sensitive_keys = splitCSV(v); renderOutput(); },
                'pass, secret, token, key, api_key, password',
                'Key names whose values are always redacted regardless of entropy. Case-insensitive.')
        ]));
        c.appendChild(el('p', { className: 'text-xs text-gray-400 mt-2' },
            'Powered by <a href="https://github.com/aragossa/pii-shield" class="text-purple-500 hover:underline" target="_blank">pii-shield</a> — entropy-based detection with Luhn CC validation, bigram analysis, and deterministic HMAC hashing.'));

        // Pensieve toggle lives in PII section for proximity to security settings.
        c.appendChild(el('div', { className: 'mt-6 pt-4', style: 'border-top: 1px solid rgba(0,0,0,0.06)' }, [
            fieldToggle('Disable Pensieve Tools', state.disable_pensieve, function (v) { state.disable_pensieve = v; renderOutput(); },
                'Disable context self-management tools (delete_context, check_budget, note, read_notes). ' +
                'delete_context and note require HITL approval. Based on the StateLM paper (arXiv:2602.12108).')
        ]));
    }

    // ── DB Config ──
    function renderDBConfig() {
        var c = $('db-config-body');
        if (!c) return;
        c.innerHTML = '';
        var d = state.db_config;
        c.appendChild(el('div', { className: 'space-y-4' }, [
            fieldText('Database File Path', d.db_file, function (v) { d.db_file = v; renderOutput(); }, '~/.genie/genie.db', 'Path to the SQLite database file')
        ]));
    }

    // ── Runbooks ──
    function renderRunbook() {
        var c = $('runbook-body');
        if (!c) return;
        c.innerHTML = '';
        var rb = state.runbook;
        var paths = rb.runbook_paths || [];
        paths.forEach(function (p, i) {
            var inp = el('input', { className: 'form-input', type: 'text', value: p, placeholder: './runbooks/deploy.md or /abs/path' });
            inp.addEventListener('input', function () { rb.runbook_paths[i] = this.value; renderOutput(); });
            c.appendChild(el('div', { className: 'flex items-center gap-2 mb-2' }, [
                inp,
                el('button', { className: 'btn-remove', onClick: function () { rb.runbook_paths.splice(i, 1); renderAll(); } }, '✕')
            ]));
        });
        c.appendChild(
            el('button', { className: 'btn-add mt-1', onClick: function () { rb.runbook_paths.push(''); renderAll(); } }, '+ Add Path')
        );
        c.appendChild(el('p', { className: 'text-xs text-gray-400 mt-2' }, 'Tip: files in <code>.genie/runbooks/</code> are auto-discovered without config'));
    }

    // ── AGUI ──
    function renderAGUI() {
        var c = $('agui-body');
        if (!c) return;
        c.innerHTML = '';
        var a = state.messenger.agui;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldNumber('Port', a.port, function (v) { a.port = v; renderOutput(); }, 1024, 65535, 'HTTP server port'),
            fieldText('CORS Origins (comma-separated)', (a.cors_origins || []).join(', '), function (v) { a.cors_origins = splitCSV(v); renderOutput(); }, 'https://myapp.com', 'Allowed origins for browser access'),
            fieldNumber('Rate Limit (req/sec)', a.rate_limit, function (v) { a.rate_limit = parseFloat(v); renderOutput(); }, 0, 1000, 'Requests per second per IP (0 to disable)'),
            fieldNumber('Rate Burst', a.rate_burst, function (v) { a.rate_burst = v; renderOutput(); }, 1, 100, 'Burst allowance'),
            fieldNumber('Max Concurrent', a.max_concurrent, function (v) { a.max_concurrent = v; renderOutput(); }, 0, 1000, 'Max in-flight requests'),
            fieldNumber('Max Body Bytes', a.max_body_bytes, function (v) { a.max_body_bytes = v; renderOutput(); }, 0, 104857600, 'Max request body size in bytes')
        ]));
    }

    // ── Langfuse ──
    function renderLangfuse() {
        var c = $('langfuse-body');
        if (!c) return;
        c.innerHTML = '';
        var l = state.langfuse;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldEnvVar('Public Key', l.public_key, function (v) { l.public_key = v; renderOutput(); }, 'LANGFUSE_PUBLIC_KEY', 'Your Langfuse project public key'),
            fieldEnvVar('Secret Key', l.secret_key, function (v) { l.secret_key = v; renderOutput(); }, 'LANGFUSE_SECRET_KEY', 'Your Langfuse project secret key'),
            fieldText('Host', l.host, function (v) { l.host = v; renderOutput(); }, 'https://cloud.langfuse.com', 'Langfuse API host (default: cloud)'),
            fieldToggle('Enable Prompt Management', l.enable_prompts, function (v) { l.enable_prompts = v; renderOutput(); }, 'Enable prompt management integration')
        ]));
    }

    // ── Cron ──
    function renderCron() {
        var c = $('cron-body');
        if (!c) return;
        c.innerHTML = '';
        var cr = state.cron;
        c.appendChild(el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
            fieldToggle('Enabled', cr.enabled, function (v) { cr.enabled = v; renderAll(); }, 'Master switch — enable/disable the cron scheduler')
        ]));
        if (cr.enabled) {
            cr.tasks.forEach(function (t, i) {
                c.appendChild(el('div', { className: 'repeatable-item' }, [
                    el('div', { className: 'flex items-center justify-between mb-3' }, [
                        el('span', { className: 'text-sm font-semibold text-gray-600' }, 'Task #' + (i + 1) + (t.name ? ' — ' + t.name : '')),
                        el('button', { className: 'btn-remove', onClick: function () { cr.tasks.splice(i, 1); renderAll(); } }, '✕')
                    ]),
                    el('div', { className: 'grid grid-cols-1 sm:grid-cols-2 gap-4' }, [
                        fieldText('Name', t.name, function (v) { t.name = v; renderOutput(); }, 'daily-report', 'Unique task identifier'),
                        fieldText('Expression', t.expression, function (v) { t.expression = v; renderOutput(); }, '0 9 * * 1-5', 'Standard 5-field cron expression'),
                        fieldText('Action', t.action, function (v) { t.action = v; renderOutput(); }, 'Summarize open PRs', 'Prompt sent to the agent on each execution')
                    ])
                ]));
            });
            c.appendChild(
                el('button', { className: 'btn-add mt-2', onClick: function () { cr.tasks.push({ name: '', expression: '', action: '' }); renderAll(); } }, '+ Add Task')
            );
        }
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

    function runbookToToml(lines) {
        var rb = state.runbook;
        if (!hasItems(rb.runbook_paths)) return;
        lines.push('[runbook]');
        if (hasItems(rb.runbook_paths)) lines.push('runbook_paths = [' + rb.runbook_paths.filter(Boolean).map(q).join(', ') + ']');
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
            if (srv.env && Object.keys(srv.env).length > 0) {
                lines.push('[mcp.servers.env]');
                Object.keys(srv.env).filter(Boolean).forEach(function (k) { lines.push(k + ' = ' + q(srv.env[k] || '')); });
            }
            if (srv.headers && Object.keys(srv.headers).length > 0) {
                lines.push('[mcp.servers.headers]');
                Object.keys(srv.headers).filter(Boolean).forEach(function (k) { lines.push(k + ' = ' + q(srv.headers[k] || '')); });
            }
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
        if (vm.embedding_provider === 'dummy' && !vm.persistence_dir && !vm.api_key && vm.vector_store_provider === 'inmemory') return;
        lines.push('[vector_memory]');
        if (vm.vector_store_provider && vm.vector_store_provider !== 'inmemory') {
            lines.push('vector_store_provider = ' + q(vm.vector_store_provider));
        }
        if (vm.persistence_dir) lines.push('persistence_dir = ' + q(vm.persistence_dir));
        lines.push('embedding_provider = ' + q(vm.embedding_provider));
        if (vm.api_key) lines.push('api_key = ' + q('${' + vm.api_key + '}'));
        if (vm.ollama_url) lines.push('ollama_url = ' + q(vm.ollama_url));
        if (vm.ollama_model) lines.push('ollama_model = ' + q(vm.ollama_model));
        if (vm.huggingface_url) lines.push('huggingface_url = ' + q(vm.huggingface_url));
        if (vm.gemini_api_key) lines.push('gemini_api_key = ' + q('${' + vm.gemini_api_key + '}'));
        if (vm.gemini_model) lines.push('gemini_model = ' + q(vm.gemini_model));
        if (vm.allowed_metadata_keys && vm.allowed_metadata_keys.length) {
            lines.push('allowed_metadata_keys = [' + vm.allowed_metadata_keys.filter(Boolean).map(q).join(', ') + ']');
        }
        if (vm.vector_store_provider === 'milvus') {
            lines.push('');
            lines.push('[vector_memory.milvus]');
            if (vm.milvus.address) lines.push('milvus_address = ' + q(vm.milvus.address));
            if (vm.milvus.username) lines.push('milvus_username = ' + q(vm.milvus.username));
            if (vm.milvus.password) lines.push('milvus_password = ' + q('${' + vm.milvus.password + '}'));
            if (vm.milvus.db_name) lines.push('milvus_db_name = ' + q(vm.milvus.db_name));
            if (vm.milvus.api_key) lines.push('milvus_api_key = ' + q('${' + vm.milvus.api_key + '}'));
            if (vm.milvus.collection_name) lines.push('milvus_collection_name = ' + q(vm.milvus.collection_name));
            if (vm.milvus.dimension > 0) lines.push('milvus_dimension = ' + vm.milvus.dimension);
        }
        lines.push('');
    }

    function graphToToml(lines) {
        var g = state.graph;
        if (g.disabled && !g.data_dir) return;
        lines.push('[graph]');
        lines.push('disabled = ' + (g.disabled ? 'true' : 'false'));
        if (g.data_dir) lines.push('data_dir = ' + q(g.data_dir));
        lines.push('');
    }

    function dataSourcesToToml(lines) {
        var ds = state.data_sources;
        if (!ds.enabled && !ds.gmail.enabled && !ds.gdrive.enabled && !ds.github.enabled && !ds.gitlab.enabled) return;
        lines.push('[data_sources]');
        lines.push('enabled = ' + (ds.enabled ? 'true' : 'false'));
        if (ds.sync_interval) lines.push('sync_interval = ' + q(ds.sync_interval));
        if (ds.search_keywords && ds.search_keywords.length) {
            lines.push('search_keywords = [' + ds.search_keywords.filter(Boolean).map(q).join(', ') + ']');
        }
        if (ds.gmail.enabled && hasItems(ds.gmail.label_ids)) {
            lines.push('');
            lines.push('[data_sources.gmail]');
            lines.push('enabled = true');
            lines.push('label_ids = [' + ds.gmail.label_ids.filter(Boolean).map(q).join(', ') + ']');
        }
        if (ds.gdrive.enabled && hasItems(ds.gdrive.folder_ids)) {
            lines.push('');
            lines.push('[data_sources.gdrive]');
            lines.push('enabled = true');
            lines.push('folder_ids = [' + ds.gdrive.folder_ids.filter(Boolean).map(q).join(', ') + ']');
        }
        if (ds.github.enabled && hasItems(ds.github.repos)) {
            lines.push('');
            lines.push('[data_sources.github]');
            lines.push('enabled = true');
            lines.push('repos = [' + ds.github.repos.filter(Boolean).map(q).join(', ') + ']');
        }
        if (ds.gitlab.enabled && hasItems(ds.gitlab.repos)) {
            lines.push('');
            lines.push('[data_sources.gitlab]');
            lines.push('enabled = true');
            lines.push('repos = [' + ds.gitlab.repos.filter(Boolean).map(q).join(', ') + ']');
        }
        lines.push('');
    }

    function messengerToToml(lines) {
        var m = state.messenger;
        if (m.platform) {
            lines.push('[messenger]');
            lines.push('platform = ' + q(m.platform));
            if (m.buffer_size !== 100) lines.push('buffer_size = ' + m.buffer_size);
            if (hasItems(m.allowed_senders)) lines.push('allowed_senders = [' + m.allowed_senders.filter(Boolean).map(q).join(', ') + ']');
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
                lines.push('');
            } else if (m.platform === 'whatsapp') {
                lines.push('[messenger.whatsapp]');
                lines.push('');
            }
        }
        // AGUI config is always emitted — it's the default/fallback messenger
        aguiToToml(lines);
    }

    /** Assemble full TOML output. */
    function toToml() {
        var lines = [];
        // Root-level keys must come before any [section] headers in TOML.
        pensieveToToml(lines);
        if (state.providers.length > 0) providersToToml(lines);


        skillsToToml(lines);
        mcpToToml(lines);
        webSearchToToml(lines);
        vectorToToml(lines);
        graphToToml(lines);
        dataSourcesToToml(lines);
        messengerToToml(lines);
        scmToToml(lines);
        pmToToml(lines);
        browserToToml(lines);
        emailToToml(lines);
        hitlToToml(lines);
        toolwrapToToml(lines);
        dbConfigToToml(lines);

        langfuseToToml(lines);
        securityToToml(lines);
        piiToToml(lines);
        runbookToToml(lines);
        cronToToml(lines);
        return lines.join('\n');
    }

    function langfuseToToml(lines) {
        var l = state.langfuse;
        if (!l.public_key && !l.secret_key && !l.host) return;
        lines.push('[langfuse]');
        if (l.public_key) lines.push('public_key = ' + q('${' + l.public_key + '}'));
        if (l.secret_key) lines.push('secret_key = ' + q('${' + l.secret_key + '}'));
        if (l.host) lines.push('host = ' + q(l.host));
        if (l.enable_prompts) lines.push('enable_prompts = true');
        lines.push('');
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
        lines.push('[project_management]');
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
        if (!hasItems(h.always_allowed) && !hasItems(h.denied_tools) && !h.cache_ttl) return;
        lines.push('[hitl]');
        if (hasItems(h.always_allowed)) lines.push('always_allowed = [' + h.always_allowed.filter(Boolean).map(q).join(', ') + ']');
        if (hasItems(h.denied_tools)) lines.push('denied_tools = [' + h.denied_tools.filter(Boolean).map(q).join(', ') + ']');
        if (h.cache_ttl) lines.push('cache_ttl = ' + q(h.cache_ttl));
        lines.push('');
    }

    function parseKVPairs(str) {
        if (!str) return {};
        var result = {};
        str.split(',').forEach(function (pair) {
            var parts = pair.trim().split(':');
            if (parts.length === 2 && parts[0].trim() && parts[1].trim()) {
                result[parts[0].trim()] = parts[1].trim();
            }
        });
        return result;
    }

    function parseSanitizePerTool(str) {
        if (!str) return {};
        var result = {};
        str.split(',').forEach(function (entry) {
            var parts = entry.trim().split(':');
            if (parts.length === 2 && parts[0].trim() && parts[1].trim()) {
                result[parts[0].trim()] = parts[1].trim().split('|').map(function (s) { return s.trim(); }).filter(Boolean);
            }
        });
        return result;
    }

    function toolwrapToToml(lines) {
        var tw = state.toolwrap;
        var any = tw.timeout.enabled || tw.rate_limit.enabled || tw.circuit_breaker.enabled ||
            tw.concurrency.enabled || tw.retry.enabled || tw.metrics.enabled ||
            tw.tracing.enabled || tw.sanitize.enabled || tw.validation.enabled;
        if (!any) return;

        if (tw.timeout.enabled) {
            lines.push('[toolwrap.timeout]');
            lines.push('enabled = true');
            if (tw.timeout.default_timeout) lines.push('default = ' + q(tw.timeout.default_timeout));
            var perTool = parseKVPairs(tw.timeout.per_tool);
            if (Object.keys(perTool).length > 0) {
                lines.push('[toolwrap.timeout.per_tool]');
                Object.keys(perTool).forEach(function (k) { lines.push(k + ' = ' + q(perTool[k])); });
            }
            lines.push('');
        }
        if (tw.rate_limit.enabled) {
            lines.push('[toolwrap.rate_limit]');
            lines.push('enabled = true');
            lines.push('global_rate_per_minute = ' + tw.rate_limit.global_rate_per_minute);
            var perToolRL = parseKVPairs(tw.rate_limit.per_tool_rate_per_minute);
            if (Object.keys(perToolRL).length > 0) {
                lines.push('[toolwrap.rate_limit.per_tool_rate_per_minute]');
                Object.keys(perToolRL).forEach(function (k) { lines.push(k + ' = ' + perToolRL[k]); });
            }
            lines.push('');
        }
        if (tw.circuit_breaker.enabled) {
            lines.push('[toolwrap.circuit_breaker]');
            lines.push('enabled = true');
            lines.push('failure_threshold = ' + tw.circuit_breaker.failure_threshold);
            if (tw.circuit_breaker.open_duration) lines.push('open_duration = ' + q(tw.circuit_breaker.open_duration));
            lines.push('');
        }
        if (tw.concurrency.enabled) {
            lines.push('[toolwrap.concurrency]');
            lines.push('enabled = true');
            lines.push('global_limit = ' + tw.concurrency.global_limit);
            var perToolConc = parseKVPairs(tw.concurrency.per_tool_limits);
            if (Object.keys(perToolConc).length > 0) {
                lines.push('[toolwrap.concurrency.per_tool_limits]');
                Object.keys(perToolConc).forEach(function (k) { lines.push(k + ' = ' + perToolConc[k]); });
            }
            lines.push('');
        }
        if (tw.retry.enabled) {
            lines.push('[toolwrap.retry]');
            lines.push('enabled = true');
            lines.push('max_attempts = ' + tw.retry.max_attempts);
            if (tw.retry.initial_backoff) lines.push('initial_backoff = ' + q(tw.retry.initial_backoff));
            if (tw.retry.max_backoff) lines.push('max_backoff = ' + q(tw.retry.max_backoff));
            lines.push('');
        }
        if (tw.metrics.enabled) {
            lines.push('[toolwrap.metrics]');
            lines.push('enabled = true');
            if (tw.metrics.prefix) lines.push('prefix = ' + q(tw.metrics.prefix));
            lines.push('');
        }
        if (tw.tracing.enabled) {
            lines.push('[toolwrap.tracing]');
            lines.push('enabled = true');
            lines.push('');
        }
        if (tw.sanitize.enabled) {
            lines.push('[toolwrap.sanitize]');
            lines.push('enabled = true');
            if (tw.sanitize.replacement) lines.push('replacement = ' + q(tw.sanitize.replacement));
            var perToolSan = parseSanitizePerTool(tw.sanitize.per_tool);
            if (Object.keys(perToolSan).length > 0) {
                lines.push('[toolwrap.sanitize.per_tool]');
                Object.keys(perToolSan).forEach(function (k) {
                    lines.push(k + ' = [' + perToolSan[k].map(q).join(', ') + ']');
                });
            }
            lines.push('');
        }
        if (tw.validation.enabled) {
            lines.push('[toolwrap.validation]');
            lines.push('enabled = true');
            lines.push('');
        }
    }

    function dbConfigToToml(lines) {
        var d = state.db_config;
        if (!d.db_file) return;
        lines.push('[db_config]');
        lines.push('db_file = ' + q(d.db_file));
        lines.push('');
    }

    function aguiToToml(lines) {
        var a = state.messenger.agui;
        lines.push('[messenger.agui]');
        lines.push('port = ' + a.port);
        if (hasItems(a.cors_origins)) lines.push('cors_origins = [' + a.cors_origins.filter(Boolean).map(q).join(', ') + ']');
        lines.push('rate_limit = ' + a.rate_limit);
        lines.push('rate_burst = ' + a.rate_burst);
        lines.push('max_concurrent = ' + a.max_concurrent);
        lines.push('max_body_bytes = ' + a.max_body_bytes);
        lines.push('');
    }

    function pensieveToToml(lines) {
        if (!state.disable_pensieve) return;
        lines.push('disable_pensieve = true');
        lines.push('');
    }

    function securityToToml(lines) {
        var sec = state.security;
        var mapped = sec.secrets.filter(function (s) { return s.name && s.url; });
        if (mapped.length > 0) {
            lines.push('[security.secrets]');
            mapped.forEach(function (s) {
                lines.push(q(s.name) + ' = ' + q(s.url));
            });
            lines.push('');
        }
    }

    function piiToToml(lines) {
        var p = state.pii;
        var hasContent = p.salt || p.entropy_threshold !== 4.2 || p.min_secret_length !== 12 || hasItems(p.sensitive_keys);
        if (!hasContent) return;
        lines.push('[pii]');
        if (p.salt) lines.push('salt = ' + q(p.salt));
        if (p.entropy_threshold !== 4.2) lines.push('entropy_threshold = ' + p.entropy_threshold);
        if (p.min_secret_length !== 12) lines.push('min_secret_length = ' + p.min_secret_length);
        if (hasItems(p.sensitive_keys)) lines.push('sensitive_keys = [' + p.sensitive_keys.filter(Boolean).map(q).join(', ') + ']');
        lines.push('');
    }

    function cronToToml(lines) {
        var cr = state.cron;
        if (!cr.enabled) return;
        lines.push('[cron]');
        lines.push('enabled = true');

        lines.push('');
        cr.tasks.forEach(function (t) {
            if (!t.name && !t.expression && !t.action) return;
            lines.push('[[cron.tasks]]');
            if (t.name) lines.push('name = ' + q(t.name));
            if (t.expression) lines.push('expression = ' + q(t.expression));
            if (t.action) lines.push('action = ' + q(t.action));
            lines.push('');
        });
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

    function runbookToYaml(lines) {
        var rb = state.runbook;
        if (!hasItems(rb.runbook_paths)) return;
        lines.push('runbook:');
        if (hasItems(rb.runbook_paths)) {
            lines.push('  runbook_paths:');
            rb.runbook_paths.filter(Boolean).forEach(function (p) { lines.push('    - ' + yq(p)); });
        }
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
            if (srv.env && Object.keys(srv.env).length > 0) {
                lines.push('      env:');
                Object.keys(srv.env).filter(Boolean).forEach(function (k) { lines.push('        ' + k + ': ' + yq(srv.env[k] || '')); });
            }
            if (srv.headers && Object.keys(srv.headers).length > 0) {
                lines.push('      headers:');
                Object.keys(srv.headers).filter(Boolean).forEach(function (k) { lines.push('        ' + k + ': ' + yq(srv.headers[k] || '')); });
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
        if (vm.embedding_provider === 'dummy' && !vm.persistence_dir && !vm.api_key && vm.vector_store_provider === 'inmemory') return;
        lines.push('vector_memory:');
        if (vm.vector_store_provider && vm.vector_store_provider !== 'inmemory') {
            lines.push('  vector_store_provider: ' + vm.vector_store_provider);
        }
        if (vm.persistence_dir) lines.push('  persistence_dir: ' + yq(vm.persistence_dir));
        lines.push('  embedding_provider: ' + vm.embedding_provider);
        if (vm.api_key) lines.push('  api_key: ' + yq('${' + vm.api_key + '}'));
        if (vm.ollama_url) lines.push('  ollama_url: ' + yq(vm.ollama_url));
        if (vm.ollama_model) lines.push('  ollama_model: ' + vm.ollama_model);
        if (vm.huggingface_url) lines.push('  huggingface_url: ' + yq(vm.huggingface_url));
        if (vm.gemini_api_key) lines.push('  gemini_api_key: ' + yq('${' + vm.gemini_api_key + '}'));
        if (vm.gemini_model) lines.push('  gemini_model: ' + vm.gemini_model);
        if (vm.allowed_metadata_keys && vm.allowed_metadata_keys.length) {
            lines.push('  allowed_metadata_keys:');
            vm.allowed_metadata_keys.filter(Boolean).forEach(function (k) { lines.push('    - ' + yq(k)); });
        }
        if (vm.vector_store_provider === 'milvus') {
            lines.push('  milvus:');
            if (vm.milvus.address) lines.push('    milvus_address: ' + yq(vm.milvus.address));
            if (vm.milvus.username) lines.push('    milvus_username: ' + yq(vm.milvus.username));
            if (vm.milvus.password) lines.push('    milvus_password: ' + yq('${' + vm.milvus.password + '}'));
            if (vm.milvus.db_name) lines.push('    milvus_db_name: ' + yq(vm.milvus.db_name));
            if (vm.milvus.api_key) lines.push('    milvus_api_key: ' + yq('${' + vm.milvus.api_key + '}'));
            if (vm.milvus.collection_name) lines.push('    milvus_collection_name: ' + yq(vm.milvus.collection_name));
            if (vm.milvus.dimension > 0) lines.push('    milvus_dimension: ' + vm.milvus.dimension);
        }
        lines.push('');
    }

    function graphToYaml(lines) {
        var g = state.graph;
        if (g.disabled && !g.data_dir) return;
        lines.push('graph:');
        lines.push('  disabled: ' + (g.disabled ? 'true' : 'false'));
        if (g.data_dir) lines.push('  data_dir: ' + yq(g.data_dir));
        lines.push('');
    }

    function dataSourcesToYaml(lines) {
        var ds = state.data_sources;
        if (!ds.enabled && !ds.gmail.enabled && !ds.gdrive.enabled && !ds.github.enabled && !ds.gitlab.enabled) return;
        lines.push('data_sources:');
        lines.push('  enabled: ' + (ds.enabled ? 'true' : 'false'));
        if (ds.sync_interval) lines.push('  sync_interval: ' + yq(ds.sync_interval));
        if (ds.search_keywords && ds.search_keywords.length) {
            lines.push('  search_keywords:');
            ds.search_keywords.filter(Boolean).forEach(function (k) { lines.push('    - ' + yq(k)); });
        }
        if (ds.gmail.enabled && hasItems(ds.gmail.label_ids)) {
            lines.push('  gmail:');
            lines.push('    enabled: true');
            lines.push('    label_ids:');
            ds.gmail.label_ids.filter(Boolean).forEach(function (id) { lines.push('      - ' + yq(id)); });
        }
        if (ds.gdrive.enabled && hasItems(ds.gdrive.folder_ids)) {
            lines.push('  gdrive:');
            lines.push('    enabled: true');
            lines.push('    folder_ids:');
            ds.gdrive.folder_ids.filter(Boolean).forEach(function (id) { lines.push('      - ' + yq(id)); });
        }
        if (ds.github.enabled && hasItems(ds.github.repos)) {
            lines.push('  github:');
            lines.push('    enabled: true');
            lines.push('    repos:');
            ds.github.repos.filter(Boolean).forEach(function (r) { lines.push('      - ' + yq(r)); });
        }
        if (ds.gitlab.enabled && hasItems(ds.gitlab.repos)) {
            lines.push('  gitlab:');
            lines.push('    enabled: true');
            lines.push('    repos:');
            ds.gitlab.repos.filter(Boolean).forEach(function (r) { lines.push('      - ' + yq(r)); });
        }
        lines.push('');
    }

    function messengerToYaml(lines) {
        var m = state.messenger;
        lines.push('messenger:');
        if (m.platform) {
            lines.push('  platform: ' + m.platform);
            if (m.buffer_size !== 100) lines.push('  buffer_size: ' + m.buffer_size);
            if (hasItems(m.allowed_senders)) {
                lines.push('  allowed_senders:');
                m.allowed_senders.filter(Boolean).forEach(function (s) { lines.push('    - ' + yq(s)); });
            }
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
                lines.push('  googlechat: {}');
            } else if (m.platform === 'whatsapp') {
                lines.push('  whatsapp: {}');
            }
        }
        // AGUI config is always emitted — it's the default/fallback messenger
        aguiToYaml(lines, '  ');
        lines.push('');
    }

    /** Assemble full YAML output. */
    function toYaml() {
        var lines = [];
        // Root-level keys first for consistency with TOML output.
        pensieveToYaml(lines);
        if (state.providers.length > 0) providersToYaml(lines);
        langfuseToYaml(lines);


        skillsToYaml(lines);
        mcpToYaml(lines);
        webSearchToYaml(lines);
        vectorToYaml(lines);
        graphToYaml(lines);
        dataSourcesToYaml(lines);
        messengerToYaml(lines);
        scmToYaml(lines);
        pmToYaml(lines);
        browserToYaml(lines);
        emailToYaml(lines);
        hitlToYaml(lines);
        toolwrapToYaml(lines);
        dbConfigToYaml(lines);

        securityToYaml(lines);
        piiToYaml(lines);
        runbookToYaml(lines);
        cronToYaml(lines);
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

    function dbConfigToYaml(lines) {
        var d = state.db_config;
        if (!d.db_file) return;
        lines.push('db_config:');
        lines.push('  db_file: ' + yq(d.db_file));
        lines.push('');
    }

    function pmToYaml(lines) {
        var p = state.pm;
        if (!p.provider) return;
        lines.push('project_management:');
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
        if (!hasItems(h.always_allowed) && !hasItems(h.denied_tools) && !h.cache_ttl) return;
        lines.push('hitl:');
        if (hasItems(h.always_allowed)) {
            lines.push('  always_allowed:');
            h.always_allowed.filter(Boolean).forEach(function (t) { lines.push('    - ' + t); });
        }
        if (hasItems(h.denied_tools)) {
            lines.push('  denied_tools:');
            h.denied_tools.filter(Boolean).forEach(function (t) { lines.push('    - ' + t); });
        }
        if (h.cache_ttl) lines.push('  cache_ttl: ' + h.cache_ttl);
        lines.push('');
    }

    function toolwrapToYaml(lines) {
        var tw = state.toolwrap;
        var any = tw.timeout.enabled || tw.rate_limit.enabled || tw.circuit_breaker.enabled ||
            tw.concurrency.enabled || tw.retry.enabled || tw.metrics.enabled ||
            tw.tracing.enabled || tw.sanitize.enabled || tw.validation.enabled;
        if (!any) return;
        lines.push('toolwrap:');

        if (tw.timeout.enabled) {
            lines.push('  timeout:');
            lines.push('    enabled: true');
            if (tw.timeout.default_timeout) lines.push('    default: ' + yq(tw.timeout.default_timeout));
            var perTool = parseKVPairs(tw.timeout.per_tool);
            if (Object.keys(perTool).length > 0) {
                lines.push('    per_tool:');
                Object.keys(perTool).forEach(function (k) { lines.push('      ' + k + ': ' + yq(perTool[k])); });
            }
        }
        if (tw.rate_limit.enabled) {
            lines.push('  rate_limit:');
            lines.push('    enabled: true');
            lines.push('    global_rate_per_minute: ' + tw.rate_limit.global_rate_per_minute);
            var perToolRL = parseKVPairs(tw.rate_limit.per_tool_rate_per_minute);
            if (Object.keys(perToolRL).length > 0) {
                lines.push('    per_tool_rate_per_minute:');
                Object.keys(perToolRL).forEach(function (k) { lines.push('      ' + k + ': ' + perToolRL[k]); });
            }
        }
        if (tw.circuit_breaker.enabled) {
            lines.push('  circuit_breaker:');
            lines.push('    enabled: true');
            lines.push('    failure_threshold: ' + tw.circuit_breaker.failure_threshold);
            if (tw.circuit_breaker.open_duration) lines.push('    open_duration: ' + yq(tw.circuit_breaker.open_duration));
        }
        if (tw.concurrency.enabled) {
            lines.push('  concurrency:');
            lines.push('    enabled: true');
            lines.push('    global_limit: ' + tw.concurrency.global_limit);
            var perToolConc = parseKVPairs(tw.concurrency.per_tool_limits);
            if (Object.keys(perToolConc).length > 0) {
                lines.push('    per_tool_limits:');
                Object.keys(perToolConc).forEach(function (k) { lines.push('      ' + k + ': ' + perToolConc[k]); });
            }
        }
        if (tw.retry.enabled) {
            lines.push('  retry:');
            lines.push('    enabled: true');
            lines.push('    max_attempts: ' + tw.retry.max_attempts);
            if (tw.retry.initial_backoff) lines.push('    initial_backoff: ' + yq(tw.retry.initial_backoff));
            if (tw.retry.max_backoff) lines.push('    max_backoff: ' + yq(tw.retry.max_backoff));
        }
        if (tw.metrics.enabled) {
            lines.push('  metrics:');
            lines.push('    enabled: true');
            if (tw.metrics.prefix) lines.push('    prefix: ' + yq(tw.metrics.prefix));
        }
        if (tw.tracing.enabled) {
            lines.push('  tracing:');
            lines.push('    enabled: true');
        }
        if (tw.sanitize.enabled) {
            lines.push('  sanitize:');
            lines.push('    enabled: true');
            if (tw.sanitize.replacement) lines.push('    replacement: ' + yq(tw.sanitize.replacement));
            var perToolSan = parseSanitizePerTool(tw.sanitize.per_tool);
            if (Object.keys(perToolSan).length > 0) {
                lines.push('    per_tool:');
                Object.keys(perToolSan).forEach(function (k) {
                    lines.push('      ' + k + ':');
                    perToolSan[k].forEach(function (p) { lines.push('        - ' + yq(p)); });
                });
            }
        }
        if (tw.validation.enabled) {
            lines.push('  validation:');
            lines.push('    enabled: true');
        }
        lines.push('');
    }

    function aguiToYaml(lines, indent) {
        var a = state.messenger.agui;
        var pfx = indent || '';
        var inner = pfx + '  ';
        lines.push(pfx + 'agui:');
        lines.push(inner + 'port: ' + a.port);
        if (hasItems(a.cors_origins)) {
            lines.push(inner + 'cors_origins:');
            a.cors_origins.filter(Boolean).forEach(function (o) { lines.push(inner + '  - ' + yq(o)); });
        }
        lines.push(inner + 'rate_limit: ' + a.rate_limit);
        lines.push(inner + 'rate_burst: ' + a.rate_burst);
        lines.push(inner + 'max_concurrent: ' + a.max_concurrent);
        lines.push(inner + 'max_body_bytes: ' + a.max_body_bytes);
    }

    function langfuseToYaml(lines) {
        var l = state.langfuse;
        if (!l.public_key && !l.secret_key && !l.host) return;
        lines.push('langfuse:');
        if (l.public_key) lines.push('  public_key: ' + yq('${' + l.public_key + '}'));
        if (l.secret_key) lines.push('  secret_key: ' + yq('${' + l.secret_key + '}'));
        if (l.host) lines.push('  host: ' + yq(l.host));
        if (l.enable_prompts) lines.push('  enable_prompts: true');
        lines.push('');
    }

    function cronToYaml(lines) {
        var cr = state.cron;
        if (!cr.enabled) return;
        lines.push('cron:');
        lines.push('  enabled: true');

        if (cr.tasks.length > 0) {
            lines.push('  tasks:');
            cr.tasks.forEach(function (t) {
                if (!t.name && !t.expression && !t.action) return;
                lines.push('    - name: ' + yq(t.name));
                if (t.expression) lines.push('      expression: ' + yq(t.expression));
                if (t.action) lines.push('      action: ' + yq(t.action));
            });
        }
        lines.push('');
    }

    function securityToYaml(lines) {
        var sec = state.security;
        var mapped = sec.secrets.filter(function (s) { return s.name && s.url; });
        if (mapped.length === 0) return;
        lines.push('security:');
        lines.push('  secrets:');
        mapped.forEach(function (s) {
            lines.push('    ' + s.name + ': ' + yq(s.url));
        });
        lines.push('');
    }

    function piiToYaml(lines) {
        var p = state.pii;
        var hasContent = p.salt || p.entropy_threshold !== 4.2 || p.min_secret_length !== 12 || hasItems(p.sensitive_keys);
        if (!hasContent) return;
        lines.push('pii:');
        if (p.salt) lines.push('  salt: ' + yq(p.salt));
        if (p.entropy_threshold !== 4.2) lines.push('  entropy_threshold: ' + p.entropy_threshold);
        if (p.min_secret_length !== 6) lines.push('  min_secret_length: ' + p.min_secret_length);
        if (hasItems(p.sensitive_keys)) {
            lines.push('  sensitive_keys:');
            p.sensitive_keys.filter(Boolean).forEach(function (k) {
                lines.push('    - ' + yq(k));
            });
        }
        lines.push('');
    }

    function pensieveToYaml(lines) {
        if (!state.disable_pensieve) return;
        lines.push('disable_pensieve: true');
        lines.push('');
    }

    /* ================================================================
     * 7. OUTPUT & ACTIONS
     * ================================================================ */

    /** Detect OS from browser (best-effort; not 100% reliable). */
    function detectOS() {
        var ua = navigator.userAgent;
        var platform = (navigator.userAgentData && navigator.userAgentData.platform)
            ? navigator.userAgentData.platform
            : navigator.platform || '';
        if (/Win(dows|32|64|CE|NT)|WOW64/i.test(ua) || /Windows/i.test(String(platform))) return 'windows';
        if (/Mac|iPhone|iPad|iPod/i.test(ua) || /Mac/i.test(String(platform))) return 'mac';
        if (/Linux|Android/i.test(ua) || /Linux/i.test(String(platform))) return 'linux';
        return 'other';
    }

    var INSTALL_STEPS = {
        mac: {
            label: 'macOS',
            steps: [
                { title: 'Install Genie (Homebrew):', code: 'brew install stackgenhq/homebrew-stackgen/genie' },
                { title: 'Or install with Go:', code: 'CGO_ENABLED=1 go install -mod=mod github.com/stackgenhq/genie@latest' },
                { title: 'Save the copied config to your home directory:', code: '# Paste the copied content into ~/.genie.toml (or ~/.genie.yaml)' },
                { title: 'Run Genie:', code: 'genie' }
            ]
        },
        linux: {
            label: 'Linux',
            steps: [
                { title: 'Install Genie (Homebrew):', code: 'brew install stackgenhq/homebrew-stackgen/genie' },
                { title: 'Or install with Go:', code: 'CGO_ENABLED=1 go install -mod=mod github.com/stackgenhq/genie@latest' },
                { title: 'Or run with Docker:', code: 'docker run --rm -it -v ~/.genie.toml:/root/.genie.toml -v $(pwd):/workspace ghcr.io/stackgenhq/genie:latest grant' },
                { title: 'Save the copied config to your home directory:', code: '# Paste the copied content into ~/.genie.toml (or ~/.genie.yaml)' },
                { title: 'Run Genie:', code: 'genie' }
            ]
        },
        windows: {
            label: 'Windows',
            steps: [
                { title: 'Install Genie (Scoop):', code: 'scoop bucket add stackgen https://github.com/stackgenhq/homebrew-stackgen\nscoop install genie' },
                { title: 'Or install with Go:', code: 'go install -mod=mod github.com/stackgenhq/genie@latest' },
                { title: 'Save the copied content as:', code: '%USERPROFILE%\\.genie\\.genie.toml' },
                { title: 'Run Genie in Command Prompt or PowerShell:', code: 'genie' }
            ]
        },
        other: {
            label: 'Other',
            steps: [
                { title: 'Install Genie (Go):', code: 'CGO_ENABLED=1 go install -mod=mod github.com/stackgenhq/genie@latest' },
                { title: 'Or run with Docker:', code: 'docker run --rm -it -v ~/.genie.toml:/root/.genie.toml -v $(pwd):/workspace ghcr.io/stackgenhq/genie:latest grant' },
                { title: 'Save the copied config as .genie.toml (or .genie.yaml) in your home directory or project root.', code: '' },
                { title: 'Run Genie:', code: 'genie' }
            ]
        }
    };

    /** Show a small modal with install instructions for the detected OS; allow switching OS. */
    function showInstallModal() {
        var overlay = document.getElementById('install-modal-overlay');
        if (overlay) return;

        var currentOS = detectOS();
        var ext = state.format === 'toml' ? '.toml' : '.yaml';
        var configFile = '.genie' + ext;

        function stepsHtml(osKey) {
            var data = INSTALL_STEPS[osKey] || INSTALL_STEPS.other;
            var html = '<div class="install-modal-steps">';
            data.steps.forEach(function (s) {
                html += '<p class="install-modal-step-title">' + s.title + '</p>';
                if (s.code) {
                    html += '<pre class="install-modal-code">' + s.code.replace(/</g, '&lt;').replace(/>/g, '&gt;') + '</pre>';
                }
            });
            html += '</div>';
            return html;
        }

        function renderBody(selectedOS) {
            var body = document.getElementById('install-modal-body');
            if (body) body.innerHTML = stepsHtml(selectedOS);
        }

        var osOrder = ['mac', 'linux', 'windows', 'other'];
        var tabButtons = osOrder.map(function (osKey) {
            var data = INSTALL_STEPS[osKey];
            var btn = el('button', {
                className: 'install-modal-tab' + (osKey === currentOS ? ' active' : ''),
                type: 'button'
            }, data.label);
            btn.dataset.os = osKey;
            btn.addEventListener('click', function () {
                document.querySelectorAll('.install-modal-tab').forEach(function (b) { b.classList.remove('active'); });
                btn.classList.add('active');
                renderBody(osKey);
            });
            return btn;
        });

        var overlayEl = el('div', { id: 'install-modal-overlay', className: 'install-modal-overlay' });
        var modal = el('div', { className: 'install-modal' }, [
            el('div', { className: 'install-modal-header' }, [
                el('h3', { className: 'install-modal-title' }, 'How to install on your machine'),
                el('button', { type: 'button', className: 'install-modal-close', 'aria-label': 'Close' }, '×')
            ]),
            el('p', { className: 'install-modal-copied' }, 'Your config has been copied to the clipboard.'),
            el('p', { className: 'install-modal-congrats' }, 'Congratulations on taking the first step to having a secure assistant.'),
            el('p', { className: 'install-modal-hint' }, 'Config file: ' + configFile + ' in your home directory or project root. Prefer terminal? Run genie setup for guided config creation.'),
            el('div', { className: 'install-modal-tabs' }, tabButtons),
            el('div', { id: 'install-modal-body', className: 'install-modal-body' }, stepsHtml(currentOS))
        ]);

        overlayEl.appendChild(modal);

        overlayEl.addEventListener('click', function (e) {
            if (e.target === overlayEl) closeModal();
        });
        modal.querySelector('.install-modal-close').addEventListener('click', closeModal);

        function closeModal() {
            overlayEl.remove();
        }

        document.body.appendChild(overlayEl);
    }

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
            showInstallModal();
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

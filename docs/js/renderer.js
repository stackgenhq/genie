/**
 * renderer.js — YAML-driven docs renderer.
 * Fetches docs.yml via js-yaml (loaded via CDN) and renders the docs page.
 */
(function () {
    'use strict';

    /* ── Helpers ── */

    /** Render inline backtick-delimited code in a text string to <code> tags. */
    function inlineCode(text) {
        if (!text) return '';
        return text.replace(/`([^`]+)`/g, '<code>$1</code>');
    }

    /** Build an HTML <table> from columns[] and rows[][]. */
    function renderTable(columns, rows) {
        var html = '<table class="config-table"><thead><tr>';
        columns.forEach(function (col) { html += '<th>' + col + '</th>'; });
        html += '</tr></thead><tbody>';
        rows.forEach(function (row) {
            html += '<tr>';
            row.forEach(function (cell) { html += '<td>' + inlineCode(String(cell)) + '</td>'; });
            html += '</tr>';
        });
        html += '</tbody></table>';
        return html;
    }

    /** Escape HTML for use inside <pre> blocks. */
    function escapeHtml(str) {
        return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }

    /** SVG chevron for FAQ. */
    var chevronSvg = '<svg class="w-5 h-5 text-gray-400 chevron-faq" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" /></svg>';

    /** Arrow SVG for CTA button. */
    var arrowSvg = '<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7l5 5m0 0l-5 5m5-5H6" /></svg>';

    /* ── Section Renderers ── */

    function renderConfigTab(sections) {
        // Quick nav pills with better accessibility and mobile scroll
        var pills = '<section class="px-4 md:px-6 py-4 md:py-6"><div class="max-w-5xl mx-auto"><div class="text-center mb-4"><p class="text-xs md:text-sm font-semibold text-gray-600 uppercase tracking-wide">Quick Navigation</p></div><div class="flex flex-wrap justify-center gap-2 fade-in overflow-x-auto">';
        sections.forEach(function (s) {
            pills += '<a href="#config-' + s.id + '" onclick="showConfigSection(\'' + s.id + '\'); return false;" class="doc-pill" aria-label="Jump to ' + s.name + '">' + s.icon + ' ' + s.name + '</a>';
        });
        pills += '</div></div></section>';

        // Sidebar
        var sidebar = '';
        sections.forEach(function (s, i) {
            sidebar += '<div class="cfg-item' + (i === 0 ? ' active' : '') + '" onclick="showConfigSection(\'' + s.id + '\')">';
            sidebar += '<span class="cfg-item-icon">' + s.icon + '</span>';
            sidebar += '<div><div class="cfg-item-name">' + s.name + '</div>';
            sidebar += '<div class="cfg-item-desc">' + s.sidebar_desc + '</div></div></div>';
        });

        // Detail panels with section anchors
        var panels = '';
        sections.forEach(function (s, i) {
            panels += '<div id="detail-' + s.id + '" class="cfg-detail-panel section-anchor' + (i === 0 ? ' active' : '') + '">';
            panels += '<div id="config-' + s.id + '" class="detail-header"><h3>' + s.detail_title + '</h3>';
            panels += '<p>' + inlineCode(s.detail_desc) + '</p></div>';

            // Regular tables
            if (s.tables) {
                s.tables.forEach(function (t) {
                    panels += renderTable(t.columns, t.rows);
                });
            }

            // Subsections (e.g. MCP, Messenger with sub-labels)
            if (s.subsections) {
                s.subsections.forEach(function (sub) {
                    panels += '<p class="detail-sub">' + sub.label + '</p>';
                    panels += renderTable(sub.columns, sub.rows);
                });
            }

            // Code example
            if (s.code_example) {
                panels += '<p class="detail-sub">' + s.code_example.label + '</p>';
                panels += '<pre class="bg-gray-50 rounded-lg p-4 text-xs font-mono overflow-x-auto"><code>' + escapeHtml(s.code_example.code.trim()) + '</code></pre>';
            }

            panels += '</div>';
        });

        return pills +
            '<section class="py-4 md:py-6 px-4 md:px-6 doc-section-wrapper"><div class="max-w-6xl mx-auto"><div class="grid grid-cols-1 lg:grid-cols-12 gap-4 md:gap-6">' +
            '<div class="lg:col-span-4 xl:col-span-3"><div class="cfg-sidebar space-y-1"><div class="mb-2 text-xs font-semibold text-gray-500 uppercase tracking-wide px-2 md:px-4">Configuration Sections</div>' + sidebar + '</div></div>' +
            '<div class="lg:col-span-8 xl:col-span-9"><div class="bg-white border border-gray-100 rounded-xl shadow-sm p-4 md:p-8" style="min-height: 400px;">' +
            panels +
            '</div></div></div></div></section>';
    }

    function renderHowToTab(guides) {
        var html = '<section class="py-6 md:py-8 px-4 md:px-6 doc-section-wrapper"><div class="max-w-4xl mx-auto">';
        html += '<div class="text-center mb-6 md:mb-8"><h2 class="text-xl md:text-2xl font-bold mb-2">How-To Guides</h2><p class="text-xs md:text-sm text-gray-500">Step-by-step guides to get up and running quickly.</p></div>';
        html += '<div class="grid grid-cols-1 md:grid-cols-2 gap-4 md:gap-5">';
        guides.forEach(function (g) {
            html += '<div class="howto-card">';
            html += '<div class="flex items-center gap-3 mb-3"><span class="step-number">' + g.step + '</span><h4>' + g.title + '</h4></div>';
            html += '<p>' + g.desc + '</p>';
            // Render code block
            var lines = g.code.trim().split('\n');
            html += '<div class="code-block">';
            lines.forEach(function (line, idx) {
                if (line.trim().startsWith('#')) {
                    html += '<span class="comment">' + escapeHtml(line) + '</span>';
                } else {
                    html += escapeHtml(line);
                }
                if (idx < lines.length - 1) html += '<br>';
            });
            html += '</div></div>';
        });
        html += '</div></div></section>';
        return html;
    }

    function renderArchitectureTab(arch) {
        var html = '<section class="py-8 md:py-12 px-4 md:px-6 doc-section-wrapper"><div class="max-w-5xl mx-auto fade-in">';
        html += '<h2 class="text-2xl md:text-3xl font-bold mb-4 text-center">' + arch.title + '</h2>';
        html += '<p class="text-sm md:text-base text-gray-500 text-center mb-6 md:mb-8">Understanding how Genie works under the hood</p>';

        // Overview
        if (arch.overview) {
            html += '<h3 class="text-lg md:text-xl font-semibold mb-4">' + arch.overview.title + '</h3>';
            html += '<p class="mb-4 text-xs md:text-sm text-gray-600">' + arch.overview.desc + '</p>';
            html += '<pre class="bg-gray-50 p-3 md:p-4 rounded-lg overflow-x-auto text-xs font-mono mb-6 md:mb-8">' + escapeHtml(arch.overview.diagram.trim()) + '</pre>';
        }

        // Subsystems
        if (arch.subsystems) {
            arch.subsystems.forEach(function (sub) {
                html += '<div class="mb-8 p-5 border border-gray-100 rounded-xl shadow-sm">';
                html += '<h3 class="text-lg font-semibold mb-2">' + sub.title + '</h3>';
                html += '<p class="text-sm text-gray-600 mb-4">' + sub.desc + '</p>';
                if (sub.columns && sub.rows) {
                    html += renderTable(sub.columns, sub.rows);
                }
                html += '</div>';
            });
        }

        // Event types table
        if (arch.event_types) {
            html += '<h3 class="text-xl font-semibold mb-4">' + arch.event_types.title + '</h3>';
            html += '<div class="mb-8">' + renderTable(arch.event_types.columns, arch.event_types.rows) + '</div>';
        }

        // Endpoints table
        if (arch.endpoints) {
            html += '<h3 class="text-xl font-semibold mb-4">' + arch.endpoints.title + '</h3>';
            html += '<div class="mb-8">' + renderTable(arch.endpoints.columns, arch.endpoints.rows) + '</div>';
        }

        // Debugging scenarios
        if (arch.debugging) {
            html += '<h3 class="text-xl font-semibold mb-4">' + arch.debugging.title + '</h3>';
            html += '<div class="space-y-4 mb-8">';
            arch.debugging.items.forEach(function (item) {
                html += '<div class="p-4 border border-gray-200 rounded-lg">';
                html += '<h4 class="font-semibold mb-2">' + item.title + '</h4>';
                html += '<ol class="list-decimal list-inside space-y-1 text-sm text-gray-600">';
                item.steps.forEach(function (s) { html += '<li>' + inlineCode(s) + '</li>'; });
                html += '</ol></div>';
            });
            html += '</div>';
        }

        // Footer note
        if (arch.footer_note) {
            html += '<p class="text-sm text-gray-500">' + inlineCode(arch.footer_note) + '</p>';
        }

        html += '</div></section>';
        return html;
    }

    function renderFaqTab(items) {
        var html = '<section class="py-6 md:py-8 px-4 md:px-6 doc-section-wrapper"><div class="max-w-3xl mx-auto">';
        html += '<div class="text-center mb-6 md:mb-8"><h2 class="text-xl md:text-2xl font-bold mb-2">Frequently Asked Questions</h2><p class="text-xs md:text-sm text-gray-500">Common questions and answers about Genie</p></div>';
        html += '<div class="space-y-3">';
        items.forEach(function (item) {
            html += '<div class="faq-item">';
            html += '<div class="faq-question" onclick="this.parentElement.classList.toggle(\'open\')">';
            html += '<span>' + item.q + '</span>' + chevronSvg;
            html += '</div>';
            html += '<div class="faq-answer">' + item.a + '</div>';
            html += '</div>';
        });
        html += '</div></div></section>';
        return html;
    }

    function renderCta(cta) {
        var html = '<section class="py-12 px-6"><div class="max-w-3xl mx-auto text-center fade-in">';
        html += '<h2 class="text-2xl font-bold tracking-tight mb-3">' + cta.title + '</h2>';
        html += '<p class="text-gray-500 mb-6 max-w-lg mx-auto text-sm">' + cta.desc + '</p>';
        html += '<a href="' + cta.button_href + '" class="btn-primary text-base no-underline">' + cta.button_text + ' ' + arrowSvg + '</a>';
        html += '</div></section>';
        return html;
    }

    /* ── Main Render ── */

    function renderDocsPage(data) {
        var container = document.getElementById('docs-content');
        if (!container) return;

        // Header with title
        var html = '<section class="relative pt-20 md:pt-32 pb-6 md:pb-8 px-4 md:px-6"><div class="max-w-5xl mx-auto text-center fade-in">';
        html += '<h1 class="text-3xl md:text-4xl lg:text-5xl font-extrabold tracking-tight mb-3"><span class="gradient-text">' + data.header.title + '</span></h1>';
        html += '<p class="text-sm md:text-base text-gray-500 max-w-xl mx-auto mb-4">' + data.header.subtitle + '</p>';
        html += '</div></section>';

        // Sticky tabs navigation
        html += '<div class="docs-header-sticky"><div class="max-w-5xl mx-auto px-4 md:px-6">';
        html += '<div class="docs-tabs">';
        data.header.tabs.forEach(function (tab) {
            html += '<button class="docs-tab' + (tab.id === 'configuration' ? ' active' : '') + '" onclick="showTab(\'' + tab.id + '\')" aria-label="' + tab.label + ' section">' + tab.icon + ' ' + tab.label + '</button>';
        });
        html += '</div></div></div>';

        // Configuration section
        html += '<div id="section-configuration" class="docs-section active">' + renderConfigTab(data.config_sections) + '</div>';

        // How-To section
        html += '<div id="section-howto" class="docs-section">' + renderHowToTab(data.howto_guides) + '</div>';

        // Architecture section
        html += '<div id="section-architecture" class="docs-section">' + renderArchitectureTab(data.architecture) + '</div>';

        // FAQ section
        html += '<div id="section-faq" class="docs-section">' + renderFaqTab(data.faq) + '</div>';

        // CTA
        html += renderCta(data.cta);

        container.innerHTML = html;
    }

    /* ── Tab/Sidebar switching (exposed globally) ── */

    window.showTab = function (tabId) {
        document.querySelectorAll('.docs-tab').forEach(function (btn) { btn.classList.remove('active'); });
        var tabs = document.querySelectorAll('.docs-tab');
        for (var i = 0; i < tabs.length; i++) {
            if (tabs[i].getAttribute('onclick').indexOf(tabId) !== -1) {
                tabs[i].classList.add('active');
                break;
            }
        }
        document.querySelectorAll('.docs-section').forEach(function (s) { s.classList.remove('active'); });
        var section = document.getElementById('section-' + tabId);
        if (section) section.classList.add('active');
    };

    window.showConfigSection = function (id) {
        document.querySelectorAll('.cfg-item').forEach(function (el) { el.classList.remove('active'); });
        var items = document.querySelectorAll('.cfg-item');
        for (var i = 0; i < items.length; i++) {
            if (items[i].getAttribute('onclick') && items[i].getAttribute('onclick').indexOf(id) !== -1) {
                items[i].classList.add('active');
                break;
            }
        }
        document.querySelectorAll('.cfg-detail-panel').forEach(function (el) { el.classList.remove('active'); });
        var panel = document.getElementById('detail-' + id);
        if (panel) panel.classList.add('active');
    };

    /* ── Boot ── */

    function boot() {
        fetch('data/docs.yml')
            .then(function (res) {
                if (!res.ok) throw new Error('Failed to load docs.yml: ' + res.status);
                return res.text();
            })
            .then(function (text) {
                var data = jsyaml.load(text);
                renderDocsPage(data);
            })
            .catch(function (err) {
                console.error('Docs renderer error:', err);
                var c = document.getElementById('docs-content');
                if (c) c.innerHTML = '<div class="text-center py-20 text-red-500">Failed to load documentation. ' + err.message + '</div>';
            });
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', boot);
    } else {
        boot();
    }
})();
